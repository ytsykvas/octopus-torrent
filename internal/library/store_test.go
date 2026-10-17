package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func TestEveryConnectionHasTheDurabilitySettings(t *testing.T) {
	s, _ := withTable(t)
	ctx := deadline(t, 10*time.Second)
	check := func(db *sql.DB, name string, queryOnly int) {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Kept open until the test ends, so every connection of the pool is a different one.
		t.Cleanup(func() { _ = c.Close() })
		for pragma, want := range map[string]string{
			"journal_mode": "wal",
			"synchronous":  "2", // FULL
			"fullfsync":    "1",
			"busy_timeout": "5000",
			"foreign_keys": "1",
			"query_only":   strconv.Itoa(queryOnly),
		} {
			var got string
			if err := c.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&got); err != nil {
				t.Fatalf("%s: PRAGMA %s: %v", name, pragma, err)
			}
			if got != want {
				t.Errorf("%s: PRAGMA %s = %s, want %s", name, pragma, got, want)
			}
		}
	}
	check(s.write, "writer", 0)
	for i := range readers {
		check(s.read, fmt.Sprintf("reader %d", i), 1)
	}
	if stats := s.read.Stats(); stats.OpenConnections != readers {
		t.Errorf("the read pool opened %d connections, want %d", stats.OpenConnections, readers)
	}
}

func TestAFailedCommitIsAnErrorAndKeepsNothing(t *testing.T) {
	s, path := withTable(t)
	must(t, s, "CREATE TABLE parent(id INTEGER PRIMARY KEY)")
	must(t, s, "CREATE TABLE child(id INTEGER PRIMARY KEY, parent INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED)")
	// The deferred key is only checked by COMMIT: every statement succeeds, the commit fails.
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		if _, err := tx.Exec("INSERT INTO rows(n) VALUES (1)"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO child(parent) VALUES (99)")
		return err
	})
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("Write = %v, want ErrConstraint", err)
	}
	var se *sqlite.Error
	if !errors.As(err, &se) {
		t.Errorf("Write = %v: the driver's error is not kept", err)
	}
	// The writer connection is clean again, and nothing of the transaction was stored.
	must(t, s, "INSERT INTO parent(id) VALUES (1)")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, openStore(t, path)); n != 0 {
		t.Fatalf("after a failed commit and a reopen, %d rows exist, want 0", n)
	}
}

func TestACancelledReadEndsPromptlyAndTheConnectionStaysUsable(t *testing.T) {
	s, _ := withTable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := s.Read(ctx, func(tx Tx) error {
		var n int
		return tx.QueryRow("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT COUNT(*) FROM c").Scan(&n)
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("Read = %v, want ErrCanceled", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the cancelled read returned after %v, want under 1s", took)
	}
	if n := count(t, s); n != 0 {
		t.Fatalf("count after the cancelled read = %d", n)
	}
}

func TestACancelledWriteCommitsNothing(t *testing.T) {
	s, _ := withTable(t)
	ctx, cancel := context.WithCancel(context.Background())
	err := s.Write(ctx, func(tx Tx) error {
		if _, err := tx.Exec("INSERT INTO rows(n) VALUES (1)"); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("Write = %v, want ErrCanceled", err)
	}
	if n := count(t, s); n != 0 {
		t.Fatalf("a cancelled write stored %d rows", n)
	}
	must(t, s, "INSERT INTO rows(n) VALUES (2)")
	if n := count(t, s); n != 1 {
		t.Fatalf("the next write stored %d rows, want 1", n)
	}
}

func TestACancelledContextIsRefusedBeforeTheTransaction(t *testing.T) {
	s, _ := withTable(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := s.Write(ctx, func(Tx) error { called = true; return nil })
	if !errors.Is(err, ErrCanceled) || called {
		t.Fatalf("Write(cancelled) = %v, fn called %v; want ErrCanceled without a call", err, called)
	}
}

func TestALockedDatabaseIsBusyAfterTheTimeout(t *testing.T) {
	const busy = 200 * time.Millisecond
	s, path := withTableUsing(t, shortBusy(busy))
	other := openStore(t, path)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- other.Write(context.Background(), func(tx Tx) error {
			close(held)
			<-release
			_, err := tx.Exec("INSERT INTO rows(n) VALUES (1)")
			return err
		})
	}()
	<-held
	start := time.Now()
	err := s.Write(deadline(t, 5*time.Second), func(Tx) error { return nil })
	took := time.Since(start)
	close(release)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Write while another connection holds the lock = %v, want ErrBusy", err)
	}
	if took < busy || took > 2*time.Second {
		t.Errorf("ErrBusy after %v, want between %v and 2s", took, busy)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	must(t, s, "INSERT INTO rows(n) VALUES (2)")
	if n := count(t, s); n != 2 {
		t.Fatalf("%d rows after the lock was released, want 2", n)
	}
}

func TestReadersNeverWaitForTheWriterNorSeeAHalfTransaction(t *testing.T) {
	s, _ := withTable(t)
	const commits = 300
	ctx := deadline(t, 60*time.Second)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	reads := make([]int, readers)
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			last := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				var n int
				err := s.Read(ctx, func(tx Tx) error {
					return tx.QueryRow("SELECT COUNT(*) FROM rows").Scan(&n)
				})
				switch {
				case err != nil:
					errs <- fmt.Errorf("reader %d: %w", i, err)
					return
				case n%2 != 0:
					errs <- fmt.Errorf("reader %d saw %d rows: half of a transaction", i, n)
					return
				case n < last:
					errs <- fmt.Errorf("reader %d saw %d rows after %d", i, n, last)
					return
				}
				last = n
				reads[i]++
			}
		}()
	}
	start := time.Now()
	for i := range commits {
		err := s.Write(ctx, func(tx Tx) error {
			_, err := tx.Exec("INSERT INTO rows(n) VALUES (?), (?)", i, i)
			return err
		})
		if err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	took := time.Since(start)
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := count(t, s); n != 2*commits {
		t.Fatalf("%d rows, want %d", n, 2*commits)
	}
	t.Logf("%d commits in %v (%.2f ms each); reads per reader: %v", commits, took, float64(took.Microseconds())/commits/1000, reads)
}

func TestWritesDuringCheckpointsKeepEveryCommit(t *testing.T) {
	s, path := withTable(t)
	const commits = 300
	ctx := deadline(t, 60*time.Second)
	// A second connection checkpoints all the time: the access pattern of the WAL-reset race.
	other := sql.OpenDB(connector{dsn(path, defaults.busyTimeout)})
	defer other.Close()
	stop, stopped := make(chan struct{}), make(chan int)
	go func() {
		done := 0
		for {
			select {
			case <-stop:
				stopped <- done
				return
			default:
			}
			if _, err := other.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err == nil {
				done++
			}
		}
	}()
	for i := range commits {
		err := s.Write(ctx, func(tx Tx) error {
			_, err := tx.Exec("INSERT INTO rows(n) VALUES (?)", i)
			return err
		})
		if err != nil {
			t.Fatalf("commit %d: %v", i, err)
		}
	}
	close(stop)
	t.Logf("checkpoints run during the commits: %d", <-stopped)
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() != 0 {
		t.Errorf("the WAL holds %d bytes after Close, want it truncated", info.Size())
	}
	again := openStore(t, path)
	if n := count(t, again); n != commits {
		t.Fatalf("%d rows after a reopen, want %d", n, commits)
	}
	integrity(t, again)
}

func TestANewDatabaseInAReadOnlyFolderIsRefusedAndCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	readOnly(t, dir)
	_, err := Open(deadline(t, 5*time.Second), filepath.Join(dir, "library.sqlite"))
	if !errors.Is(err, ErrCannotOpen) {
		t.Fatalf("Open in a read-only folder = %v, want ErrCannotOpen", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("the read-only folder now holds %v", got)
	}
}

func TestAnExistingDatabaseInAReadOnlyFolderIsNotReset(t *testing.T) {
	s, path := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := identify(t, path)
	readOnly(t, filepath.Dir(path))
	// WAL needs the -wal and -shm files next to the database; SQLite refuses the folder up front
	// (SQLITE_READONLY_DIRECTORY) instead of failing a later write.
	_, err := Open(deadline(t, 5*time.Second), path)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Open = %v, want ErrReadOnly", err)
	}
	if identify(t, path) != before {
		t.Fatal("the database file changed")
	}
}

func TestAReadOnlyWALFileOpensAndRefusesTheWrite(t *testing.T) {
	s, path := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before := identify(t, path)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// The file is in WAL mode already, so opening it writes nothing and succeeds; it can be read,
	// and the first write is refused.
	again := openStore(t, path)
	if n := count(t, again); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
	err := again.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (2)")
		return err
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Write to a read-only file = %v, want ErrReadOnly", err)
	}
	// Nothing was written, so the closing checkpoint has nothing to copy.
	if err := again.Close(); err != nil {
		t.Fatalf("Close of a read-only file = %v", err)
	}
	if identify(t, path) != before {
		t.Fatal("the database file changed")
	}
}

func TestAReadOnlyFileThatIsNotWALIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	db := sql.OpenDB(connector{dsn(path, defaults.busyTimeout)})
	if _, err := db.Exec("CREATE TABLE rows(n)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before := identify(t, path)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// Switching a read-only file to WAL cannot be written; Open must say so instead of running in
	// another journal mode with other guarantees.
	_, err := Open(deadline(t, 5*time.Second), path)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("Open = %v, want ErrReadOnly", err)
	}
	if identify(t, path) != before {
		t.Fatal("the database file changed")
	}
}

func TestAFileThatIsNotADatabaseIsRefusedAndKept(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "library.sqlite")
	if err := os.WriteFile(path, []byte(strings.Repeat("not a database ", 512)), 0o600); err != nil {
		t.Fatal(err)
	}
	before := identify(t, path)
	_, err := Open(deadline(t, 5*time.Second), path)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Open = %v, want ErrCorrupt", err)
	}
	if identify(t, path) != before {
		t.Fatal("the file changed")
	}
	if got := names(t, dir); len(got) != 1 {
		t.Fatalf("the folder now holds %v", got)
	}
}

func TestADamagedPageIsReportedAsCorrupt(t *testing.T) {
	s, path := withTable(t)
	for range 50 {
		must(t, s, "INSERT INTO rows(n, data) VALUES (1, zeroblob(2000))")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Page 3 onwards: the header and the schema stay readable, the table's pages do not.
	if _, err := f.WriteAt([]byte(strings.Repeat("\xff", 3*4096)), 2*4096); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, path)
	err = again.Read(deadline(t, 5*time.Second), func(tx Tx) error {
		var sum int
		return tx.QueryRow("SELECT SUM(LENGTH(data)) FROM rows").Scan(&sum)
	})
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Read of a damaged table = %v, want ErrCorrupt", err)
	}
}

func TestAPathWithSpecialCharactersIsTheFileOpened(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Application Support", "a?b#c%25d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "library.sqlite")
	s := openStore(t, path)
	must(t, s, "CREATE TABLE rows(id INTEGER PRIMARY KEY)")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if got := names(t, dir); len(got) != 1 || got[0] != "library.sqlite" {
		t.Fatalf("the folder holds %v, want only library.sqlite", got)
	}
}

func TestARelativePathIsRefused(t *testing.T) {
	if _, err := Open(context.Background(), "library.sqlite"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("Open(relative) = %v, want an error naming an absolute path", err)
	}
}

func TestAFunctionsOwnErrorRollsBackAndIsReturnedUnchanged(t *testing.T) {
	s, _ := withTable(t)
	own := errors.New("the caller's own reason")
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		if _, err := tx.Exec("INSERT INTO rows(n) VALUES (1)"); err != nil {
			return err
		}
		return own
	})
	if err != own {
		t.Fatalf("Write = %v, want the function's error itself", err)
	}
	if n := count(t, s); n != 0 {
		t.Fatalf("%d rows stored, want 0", n)
	}
}

func TestClosingTwiceAndUsingAClosedStore(t *testing.T) {
	s, _ := withTable(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("a second Close = %v", err)
	}
	if err := s.Write(context.Background(), func(Tx) error { return nil }); err == nil {
		t.Error("Write after Close succeeded")
	}
	if err := s.Read(context.Background(), func(Tx) error { return nil }); err == nil {
		t.Error("Read after Close succeeded")
	}
}

func TestCloseReportsACheckpointBlockedByAnotherReader(t *testing.T) {
	s, path := withTableUsing(t, shortBusy(100*time.Millisecond))
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	other := openStore(t, path)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- other.Read(context.Background(), func(tx Tx) error {
			var n int
			if err := tx.QueryRow("SELECT COUNT(*) FROM rows").Scan(&n); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	must(t, s, "INSERT INTO rows(n) VALUES (2)")
	err := s.Close()
	close(release)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Close while another reader holds the WAL = %v, want ErrBusy", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Nothing was lost: the WAL is still there and the next open sees both rows.
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if n := count(t, openStore(t, path)); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
}

func TestAJournalModeOtherThanWALIsRefused(t *testing.T) {
	// Only reads the mode: a new database answers "delete", as SQLite does when it keeps a mode.
	set := defaults
	set.walPragma = "PRAGMA journal_mode"
	_, err := open(deadline(t, 5*time.Second), filepath.Join(t.TempDir(), "library.sqlite"), set)
	if err == nil || !strings.Contains(err.Error(), `kept journal_mode "delete"`) {
		t.Fatalf("Open = %v, want the kept mode named", err)
	}
}

func TestQueryReadsEveryRowUnderTheOperationsContext(t *testing.T) {
	s, _ := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1), (2), (3)")
	var got []int
	err := s.Read(deadline(t, 5*time.Second), func(tx Tx) error {
		rows, err := tx.Query("SELECT n FROM rows ORDER BY n")
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				return err
			}
			got = append(got, n)
		}
		return rows.Err()
	})
	if err != nil || !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("Query = %v, %v; want [1 2 3]", got, err)
	}
}
