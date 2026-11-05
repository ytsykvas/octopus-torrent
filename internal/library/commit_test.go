package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"octopustorrent/internal/testutil/volume"
)

// commitHook makes SQLite call hook when the writer commits, until the test ends: a non-zero result
// refuses the commit, which SQLite then rolls back. It runs inside COMMIT, after database/sql
// decided to commit, so it is the moment no context check can reach.
func commitHook(t *testing.T, s *store, hook sqlite.CommitHookFn) {
	t.Helper()
	register := func(fn sqlite.CommitHookFn) {
		t.Helper()
		conn, err := s.write.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if err := conn.Raw(func(dc any) error {
			dc.(sqlite.HookRegisterer).RegisterCommitHook(fn)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	register(hook)
	t.Cleanup(func() { register(nil) })
}

// A COMMIT that ran is reported by its own result even when the context ended while it ran: the
// commit, not the cancel, decides what was stored.
func TestACommitThatRanIsReportedByItsOwnResultWhenTheContextEnds(t *testing.T) {
	for name, refuse := range map[string]bool{"the commit refused": true, "the commit allowed": false} {
		t.Run(name, func(t *testing.T) {
			s, _ := withTable(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			commitHook(t, s, func() int32 {
				cancel()
				if refuse {
					return 1
				}
				return 0
			})
			err := s.Write(ctx, func(tx txn) error {
				_, err := tx.Exec("INSERT INTO rows(n) VALUES (1)")
				return err
			})
			want := map[bool]int{true: 0, false: 1}[refuse]
			switch {
			case refuse && (!errors.Is(err, ErrConstraint) || errors.Is(err, ErrCanceled)):
				t.Fatalf("Write whose COMMIT was refused while the context ended = %v, want ErrConstraint and not ErrCanceled", err)
			case !refuse && err != nil:
				t.Fatalf("Write whose COMMIT succeeded while the context ended = %v, want nil", err)
			}
			if n := countWith(t, s, deadline(t, 5*time.Second)); n != want {
				t.Fatalf("%d rows, want %d", n, want)
			}
		})
	}
}

// countWith reads the number of rows under ctx.
func countWith(t *testing.T, s *store, ctx context.Context) int {
	t.Helper()
	var n int
	if err := s.Read(ctx, func(tx txn) error { return tx.QueryRow("SELECT COUNT(*) FROM rows").Scan(&n) }); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// capFileSize lets no file of this process grow until restore is called (or the test ends), the way
// a disk refuses a write past a file's end: the write fails with EFBIG, which SQLite reports as
// SQLITE_IOERR_WRITE. The Go runtime ignores the SIGXFSZ that comes with it
// (runtime/signal_darwin.go: notify only), so the process keeps running.
func capFileSize(t *testing.T) (restore func()) {
	t.Helper()
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Fatal(err)
	}
	capped := old
	capped.Cur = 0
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &capped); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	restore = func() {
		once.Do(func() {
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(restore)
	return restore
}

// truncateWAL checkpoints the whole WAL into the database file and truncates it, so the next commit
// must grow the WAL from nothing.
func truncateWAL(t *testing.T, s *store) {
	t.Helper()
	var busy, logged, done int
	err := s.write.QueryRowContext(deadline(t, 5*time.Second), "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &done)
	if err != nil || busy != 0 {
		t.Fatalf("wal_checkpoint(TRUNCATE) = busy %d, %v", busy, err)
	}
}

// armed runs fn once, at the store's next commit point after on was set.
type armed struct {
	on bool
	fn func()
}

func (a *armed) beforeCommit() {
	if a.on {
		a.on = false
		a.fn()
	}
}

// A COMMIT that could not write has an unknown outcome: it may have reached the disk before the
// error. The caller learns it is unknown, never that the change was cancelled or refused.
func TestACommitThatCannotWriteHasAnUnknownOutcome(t *testing.T) {
	restore := func() {}
	trigger := &armed{fn: func() { restore = capFileSize(t) }}
	set := defaults
	set.beforeCommit = trigger.beforeCommit
	s, path := withTableUsing(t, set)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	truncateWAL(t, s)
	trigger.on = true
	err := s.Write(deadline(t, 5*time.Second), func(tx txn) error {
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (2)")
		return err
	})
	restore()
	var se *sqlite.Error
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, ErrIO) || errors.Is(err, ErrCanceled) ||
		!errors.As(err, &se) || se.Code() != sqlite3.SQLITE_IOERR_WRITE {
		t.Fatalf("Write whose COMMIT could not write = %v, want ErrOutcomeUnknown and ErrIO (SQLITE_IOERR_WRITE)", err)
	}
	t.Logf("Write whose COMMIT could not write: %v", err)
	must(t, s, "INSERT INTO rows(n) VALUES (3)")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, path)
	if n := count(t, again); n != 2 && n != 3 {
		t.Fatalf("%d rows after the reopen, want 2 (rows 1 and 3) or 3 (the unknown one stored)", n)
	}
	integrity(t, again)
}

// A full disk at COMMIT is a known outcome: SQLite rolled the transaction back.
func TestAFullDiskAtCommitKeepsNothingAndIsAKnownOutcome(t *testing.T) {
	mount := smallVolume(t)
	path := filepath.Join(mount, "library.sqlite")
	var filler string
	trigger := &armed{fn: func() { filler = volume.Fill(t, mount) }}
	set := defaults
	set.beforeCommit = trigger.beforeCommit
	s := tableAt(t, path, set)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	truncateWAL(t, s)
	trigger.on = true
	err := s.Write(deadline(t, 10*time.Second), func(tx txn) error {
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (2)")
		return err
	})
	if !errors.Is(err, ErrFull) || errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("Write whose COMMIT found the disk full = %v, want ErrFull and a known outcome", err)
	}
	t.Logf("Write whose COMMIT found the disk full: %v", err)
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, path)
	if n := count(t, again); n != 1 {
		t.Fatalf("%d rows after the reopen, want 1", n)
	}
	integrity(t, again)
}

// A migration keeps its own answer: a COMMIT that could not write fails the migration (the next open
// examines the version again, whatever reached the disk), and is never only "unknown".
func TestAMigrationWhoseCommitCannotWriteFailsAndTheNextOpenMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	first := openStore(t, path)
	must(t, first, "INSERT INTO rows(n) VALUES (1)")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// The read-only check of the next open creates the -shm file before afterCheck, so the cap
	// reaches the migration's COMMIT, which must write the WAL.
	second := with(rowsSchema, "CREATE TABLE more(id INTEGER PRIMARY KEY)")
	restore := func() {}
	set := defaults
	set.afterCheck = func() { restore = capFileSize(t) }
	_, err := open(deadline(t, 10*time.Second), path, set, second)
	restore()
	var se *sqlite.Error
	if !errors.Is(err, ErrMigrationFailed) || !errors.Is(err, ErrIO) || errors.Is(err, ErrOutcomeUnknown) ||
		!errors.As(err, &se) || se.Code() != sqlite3.SQLITE_IOERR_WRITE {
		t.Fatalf("open whose migration COMMIT could not write = %v, want ErrMigrationFailed and ErrIO (SQLITE_IOERR_WRITE) only", err)
	}
	t.Logf("open whose migration COMMIT could not write: %v", err)
	again := openWith(t, path, defaults, second)
	must(t, again, "INSERT INTO more(id) VALUES (1)")
	if n := count(t, again); n != 1 {
		t.Fatalf("%d rows after the next open, want 1", n)
	}
}
