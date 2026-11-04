package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"octopustorrent/internal/testutil/volume"
)

// threeSteps is a test schema whose migrations each change what the one before made.
var threeSteps = schema{name: "three steps", applicationID: 0x54535433, migrations: []string{
	"CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL); INSERT INTO a(v) VALUES ('from 1')",
	"CREATE TABLE b(id INTEGER PRIMARY KEY, a INTEGER NOT NULL REFERENCES a(id) ON DELETE CASCADE); CREATE INDEX b_by_a ON b(a)",
	"ALTER TABLE a ADD COLUMN w INTEGER NOT NULL DEFAULT 7",
}}

// upTo is sch as it was at version v.
func upTo(sch schema, v int) schema {
	sch.migrations = sch.migrations[:v]
	return sch
}

// with is sch with one more migration.
func with(sch schema, migration string) schema {
	sch.migrations = append(slices.Clone(sch.migrations), migration)
	return sch
}

// at creates the database at path at version v of sch, runs the statements, and closes it.
func at(t *testing.T, path string, sch schema, v int, statements ...string) {
	t.Helper()
	s, err := open(deadline(t, 10*time.Second), path, defaults, upTo(sch, v))
	if err != nil {
		t.Fatalf("open %s at version %d: %v", sch.name, v, err)
	}
	for _, statement := range statements {
		must(t, s, statement)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// rawDB opens path with the driver alone: no WAL switch, no schema, as another application would.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db := sql.OpenDB(connector{dsn(path, defaults.busyTimeout, foreignKeysOn)})
	db.SetMaxOpenConns(1)
	return db
}

// foreignDB makes the file another application would leave: the statements run in the default
// rollback journal, through the driver alone.
func foreignDB(t *testing.T, path string, statements ...string) {
	t.Helper()
	db := rawDB(t, path)
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

// stateOf reads what inspect sees in the file at path, through the driver alone.
func stateOf(t *testing.T, path string) fileState {
	t.Helper()
	db := rawDB(t, path)
	defer db.Close()
	var st fileState
	err := run(deadline(t, 10*time.Second), db, &sql.TxOptions{ReadOnly: true}, func(tx Tx) (err error) {
		st, err = inspect(tx)
		return err
	})
	if err != nil {
		t.Fatalf("inspect %s: %v", path, err)
	}
	return st
}

// scalar reads one value from the file at path, through the driver alone.
func scalar[T any](t *testing.T, path, query string) T {
	t.Helper()
	db := rawDB(t, path)
	defer db.Close()
	var v T
	if err := db.QueryRow(query).Scan(&v); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return v
}

func referenceOf(t *testing.T, sch schema, v int) string {
	t.Helper()
	structure, err := reference(deadline(t, 10*time.Second), sch, v)
	if err != nil {
		t.Fatal(err)
	}
	return structure
}

func TestANewDatabaseGetsItsIdAndEveryMigrationAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.sqlite")
	s, err := open(deadline(t, 10*time.Second), path, defaults, threeSteps)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	st := stateOf(t, path)
	if st.applicationID != threeSteps.applicationID || st.version != 3 {
		t.Fatalf("application_id %#x, user_version %d; want %#x, 3", st.applicationID, st.version, threeSteps.applicationID)
	}
	if st.structure != referenceOf(t, threeSteps, 3) {
		t.Fatalf("the structure differs from the migrations':\n%s\nwant\n%s", st.structure, referenceOf(t, threeSteps, 3))
	}
	if got := scalar[string](t, path, "SELECT v || ' ' || w FROM a"); got != "from 1 7" {
		t.Fatalf("the first migration's row is %q, want it migrated by the third", got)
	}
}

func TestEveryOlderVersionMigratesWithItsRows(t *testing.T) {
	for v := 1; v < threeSteps.current(); v++ {
		path := filepath.Join(t.TempDir(), "old.sqlite")
		at(t, path, threeSteps, v, "INSERT INTO a(v) VALUES ('kept')")
		inode := identify(t, path).inode
		s, err := open(deadline(t, 10*time.Second), path, defaults, threeSteps)
		if err != nil {
			t.Fatalf("version %d: %v", v, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if st := stateOf(t, path); st.version != 3 || st.structure != referenceOf(t, threeSteps, 3) {
			t.Fatalf("version %d migrated to %d with another structure:\n%s", v, st.version, st.structure)
		}
		if got := scalar[int](t, path, "SELECT COUNT(*) FROM a WHERE v = 'kept' AND w = 7"); got != 1 {
			t.Fatalf("version %d: the row is not kept (%d)", v, got)
		}
		if identify(t, path).inode != inode {
			t.Fatalf("version %d: the file was recreated", v)
		}
	}
}

// refused requires open to fail with want, leaving the file's bytes, inode and journal mode as they
// were and nothing beside it.
func refused(t *testing.T, path string, sch schema, want error) {
	t.Helper()
	before, mode := identify(t, path), scalar[string](t, path, "PRAGMA journal_mode")
	s, err := open(deadline(t, 10*time.Second), path, defaults, sch)
	if s != nil {
		_ = s.Close()
	}
	if !errors.Is(err, want) {
		t.Fatalf("open = %v, want %v", err, want)
	}
	for _, other := range []error{ErrCorrupt, ErrUnknownSchema, ErrUnsupportedVersion, ErrMigrationFailed} {
		if other != want && errors.Is(err, other) {
			t.Fatalf("open = %v, which is also %v", err, other)
		}
	}
	if identify(t, path) != before {
		t.Fatal("the refused file changed")
	}
	if got := scalar[string](t, path, "PRAGMA journal_mode"); got != mode {
		t.Fatalf("the refused file's journal mode went from %s to %s", mode, got)
	}
	if got := names(t, filepath.Dir(path)); len(got) != 1 {
		t.Fatalf("the folder holds %v after the refusal", got)
	}
}

func TestANewerVersionIsRefusedUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.sqlite")
	foreignDB(t, path, "PRAGMA application_id = 1414747187", "PRAGMA user_version = 4", "CREATE TABLE a(id INTEGER PRIMARY KEY)")
	refused(t, path, threeSteps, ErrUnsupportedVersion)
}

func TestAFileThatIsNotOursIsRefusedBeforeItIsConverted(t *testing.T) {
	ours := "PRAGMA application_id = 1414747187"
	for name, statements := range map[string][]string{
		"another application": {"PRAGMA application_id = 42", "PRAGMA user_version = 1", "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)"},
		"no id but tables":    {"PRAGMA user_version = 1", "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)"},
		"no id, no version":   {"CREATE TABLE something(x)"},
		"our id, version 0":   {ours, "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)"},
		"our id, no objects":  {ours},
		"an extra table":      {ours, "PRAGMA user_version = 1", "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)", "CREATE TABLE extra(x)"},
		"a changed column":    {ours, "PRAGMA user_version = 1", "CREATE TABLE a(id INTEGER PRIMARY KEY, v INTEGER NOT NULL)"},
		"a missing index": {ours, "PRAGMA user_version = 2", "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)",
			"CREATE TABLE b(id INTEGER PRIMARY KEY, a INTEGER NOT NULL REFERENCES a(id) ON DELETE CASCADE)"},
		"a trigger": {ours, "PRAGMA user_version = 1", "CREATE TABLE a(id INTEGER PRIMARY KEY, v TEXT NOT NULL)",
			"CREATE TRIGGER t AFTER INSERT ON a BEGIN SELECT 1; END"},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "foreign.sqlite")
			foreignDB(t, path, statements...)
			refused(t, path, threeSteps, ErrUnknownSchema)
		})
	}
}

func TestAReadOnlyFileOfAnotherApplicationIsUnknownNotReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.sqlite")
	foreignDB(t, path, "PRAGMA application_id = 42", "CREATE TABLE a(x)")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	// Reading whose file it is takes no write lock, so a file we may not write is still recognised.
	refused(t, path, threeSteps, ErrUnknownSchema)
}

func TestSQLiteStatisticsDoNotMakeADatabaseUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analyzed.sqlite")
	at(t, path, threeSteps, 3, "INSERT INTO b(a) VALUES (1)", "ANALYZE", "PRAGMA optimize")
	if got := scalar[int](t, path, "SELECT COUNT(*) FROM sqlite_schema WHERE name LIKE 'sqlite_stat%'"); got == 0 {
		t.Fatal("ANALYZE left no statistics table; the test proves nothing")
	}
	at(t, path, threeSteps, 3)
}

func TestAFailedMigrationKeepsTheOldVersionAndEveryRow(t *testing.T) {
	broken := with(upTo(threeSteps, 1), "CREATE TABLE c(x); INSERT INTO missing VALUES (1)")
	for name, setup := range map[string]func(path string){
		"an older database": func(path string) { at(t, path, threeSteps, 1, "INSERT INTO a(v) VALUES ('kept')") },
		"a new database":    func(string) {},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "library.sqlite")
			setup(path)
			before := stateOf(t, path)
			s, err := open(deadline(t, 10*time.Second), path, defaults, broken)
			if s != nil {
				_ = s.Close()
			}
			if !errors.Is(err, ErrMigrationFailed) || errors.Is(err, ErrUnknownSchema) || errors.Is(err, ErrCorrupt) {
				t.Fatalf("open = %v, want ErrMigrationFailed only", err)
			}
			if want := "from version " + map[bool]string{true: "1", false: "0"}[before.version == 1] + " to 2"; !strings.Contains(err.Error(), want) {
				t.Fatalf("open = %v, want it to name %q", err, want)
			}
			// The bundled SQLite rolled back both pragmas with the migration.
			if after := stateOf(t, path); after != before {
				t.Fatalf("after the failed migration the file is %+v, want %+v", after, before)
			}
		})
	}
}

// parentChild is a schema whose second migration rebuilds the parent table the way SQLite's
// documented procedure changes a table: a new table, the rows copied, the old one dropped, the new one
// renamed.
var parentChild = schema{name: "parent and child", applicationID: 0x54504331, migrations: []string{
	`CREATE TABLE parent(id INTEGER PRIMARY KEY);
	CREATE TABLE child(id INTEGER PRIMARY KEY, parent INTEGER NOT NULL REFERENCES parent(id) ON DELETE CASCADE);
	INSERT INTO parent(id) VALUES (1); INSERT INTO child(parent) VALUES (1), (1)`,
	`CREATE TABLE parent_new(id INTEGER PRIMARY KEY, name TEXT);
	INSERT INTO parent_new(id) SELECT id FROM parent;
	DROP TABLE parent;
	ALTER TABLE parent_new RENAME TO parent`,
}}

func TestATableRebuildKeepsTheRowsThatReferToIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, parentChild, 1)
	// With foreign keys on, dropping the parent would delete both children through the cascade.
	at(t, path, parentChild, 2)
	if got := scalar[int](t, path, "SELECT COUNT(*) FROM child"); got != 2 {
		t.Fatalf("%d children after the rebuild, want 2", got)
	}
	// Every connection after the migration has foreign keys on again.
	s := openWith(t, path, defaults, parentChild)
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		_, err := tx.Exec("INSERT INTO child(parent) VALUES (99)")
		return err
	})
	if !errors.Is(err, ErrConstraint) {
		t.Fatalf("a child of a missing parent after the migration = %v, want ErrConstraint", err)
	}
}

func TestAMigrationThatBreaksAForeignKeyKeepsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, parentChild, 1)
	before := stateOf(t, path)
	s, err := open(deadline(t, 10*time.Second), path, defaults, with(upTo(parentChild, 1), "INSERT INTO child(parent) VALUES (99)"))
	if s != nil {
		_ = s.Close()
	}
	if !errors.Is(err, ErrMigrationFailed) || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("open = %v, want ErrMigrationFailed naming the foreign key", err)
	}
	if after := stateOf(t, path); after != before {
		t.Fatalf("after the refused migration the file is %+v, want %+v", after, before)
	}
	if got := scalar[int](t, path, "SELECT COUNT(*) FROM child"); got != 2 {
		t.Fatalf("%d children, want 2", got)
	}
}

func TestAMigrationListThatDoesNotBuildIsAnErrorNotARefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 1)
	broken := threeSteps
	broken.migrations = []string{"CREATE TABLE a(", threeSteps.migrations[1]}
	s, err := open(deadline(t, 10*time.Second), path, defaults, broken)
	if s != nil {
		_ = s.Close()
	}
	if err == nil || errors.Is(err, ErrUnknownSchema) || errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("open with a migration that is not SQL = %v, want its error", err)
	}
}

func TestExaminingOnAnEndedTransactionIsAnError(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "library.sqlite"))
	ctx := deadline(t, 5*time.Second)
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := examine(Tx{ctx, tx}, rowsSchema); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("examine on an ended transaction = %v, want sql.ErrTxDone", err)
	}
}

// The real boundaries: a full disk, a read-only file, another process, a second store, a cancel and
// a kill during the migration.

func TestAFullDiskDuringAMigrationKeepsTheOldVersion(t *testing.T) {
	for name, c := range map[string]struct {
		migration string
		free      int64 // bytes left free on the volume; 0 = none
	}{
		// More than the volume holds: a statement spills pages and fails.
		"in a statement": {"CREATE TABLE big(data BLOB); WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 16) INSERT INTO big SELECT randomblob(1048576) FROM n", 0},
		// Small enough to stay in the page cache: COMMIT writes it and fails.
		"at commit": {"CREATE TABLE big(data BLOB); INSERT INTO big VALUES (randomblob(262144))", 96 << 10},
	} {
		t.Run(name, func(t *testing.T) {
			mount := smallVolume(t)
			path := filepath.Join(mount, "library.sqlite")
			at(t, path, threeSteps, 1, "INSERT INTO a(v) VALUES ('kept')")
			before := stateOf(t, path)
			// A reader keeps the WAL files, so opening the file needs no space and the migration is
			// what fails.
			reader := rawDB(t, path)
			defer reader.Close()
			if _, err := reader.Exec("SELECT COUNT(*) FROM a"); err != nil {
				t.Fatal(err)
			}
			filler := volume.Fill(t, mount)
			if c.free > 0 {
				info, err := os.Stat(filler)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Truncate(filler, info.Size()-c.free); err != nil {
					t.Fatal(err)
				}
			}
			s, err := open(deadline(t, 20*time.Second), path, defaults, with(upTo(threeSteps, 1), c.migration))
			if s != nil {
				_ = s.Close()
			}
			if !errors.Is(err, ErrFull) || !errors.Is(err, ErrMigrationFailed) {
				t.Fatalf("open = %v, want ErrFull and ErrMigrationFailed", err)
			}
			if err := os.Remove(filler); err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if after := stateOf(t, path); after != before {
				t.Fatalf("after the full disk the file is %+v, want %+v", after, before)
			}
			again := openWith(t, path, defaults, upTo(threeSteps, 1))
			integrity(t, again)
			if err := again.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAReadOnlyFileIsNotMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 1)
	before := identify(t, path)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	s, err := open(deadline(t, 10*time.Second), path, defaults, threeSteps)
	if s != nil {
		_ = s.Close()
	}
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("open = %v, want ErrReadOnly", err)
	}
	if identify(t, path) != before {
		t.Fatal("the read-only file changed")
	}
}

func TestTwoStoresMigratingOneFileAtOnceRunTheMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 2)
	// Applied twice, the migration fails: the column exists. Each store checks the version again
	// under the write lock, so the second one finds nothing left to do.
	// The stores close after both opened: two closing checkpoints at once may be ErrBusy, which is
	// Close's to report and not this test's subject.
	stores, errs := make(chan *Store, 2), make(chan error, 2)
	for range 2 {
		go func() {
			s, err := open(deadline(t, 20*time.Second), path, defaults, threeSteps)
			stores <- s
			errs <- err
		}()
	}
	for range 2 {
		if s := <-stores; s != nil {
			t.Cleanup(func() { _ = s.Close() })
		}
		if err := <-errs; err != nil {
			t.Fatalf("a concurrent open = %v", err)
		}
	}
	if st := stateOf(t, path); st.version != 3 {
		t.Fatalf("version %d, want 3", st.version)
	}
}

func TestAFileThatChangedBeforeTheWriteLockIsCheckedAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 2)
	set := defaults
	// Between the check and the write lock, a store of a newer build migrates the file further.
	set.afterCheck = func() { at(t, path, with(threeSteps, "CREATE TABLE d(x)"), 4) }
	s, err := open(deadline(t, 10*time.Second), path, set, threeSteps)
	if s != nil {
		_ = s.Close()
	}
	if !errors.Is(err, ErrUnsupportedVersion) || !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("open = %v, want ErrUnsupportedVersion and ErrMigrationFailed", err)
	}
	if st := stateOf(t, path); st.version != 4 {
		t.Fatalf("version %d, want the newer build's 4", st.version)
	}
}

// slowMigration writes more than SQLite's page cache holds, so its pages reach the WAL, then runs
// forever: a migration interrupted in the middle.
var slowMigration = "CREATE TABLE big(data BLOB); " +
	"WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 64) INSERT INTO big SELECT randomblob(65536) FROM n; " +
	"WITH RECURSIVE c(x) AS (VALUES (1) UNION ALL SELECT x + 1 FROM c) SELECT COUNT(*) FROM c"

// migrating waits until the WAL beside path holds uncommitted pages of a running migration.
func migrating(t *testing.T, path string) {
	t.Helper()
	ctx := deadline(t, 30*time.Second)
	for size(path+"-wal") < 1<<20 {
		select {
		case <-ctx.Done():
			t.Fatalf("the migration wrote %d bytes to the WAL, want 1 MiB", size(path+"-wal"))
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// interrupted requires the file at path to be at version 1 with its row, intact, and then to migrate
// on the next open.
func interrupted(t *testing.T, path string) {
	t.Helper()
	if st := stateOf(t, path); st.version != 1 {
		t.Fatalf("version %d after the interrupted migration, want 1", st.version)
	}
	if got := scalar[int](t, path, "SELECT COUNT(*) FROM a WHERE v = 'kept'"); got != 1 {
		t.Fatalf("the row is gone (%d)", got)
	}
	s := openWith(t, path, defaults, upTo(threeSteps, 1))
	integrity(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	at(t, path, threeSteps, 3)
}

func TestACancelledMigrationKeepsTheOldVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 1, "INSERT INTO a(v) VALUES ('kept')")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		s, err := open(ctx, path, defaults, with(upTo(threeSteps, 1), slowMigration))
		if s != nil {
			_ = s.Close()
		}
		done <- err
	}()
	migrating(t, path)
	cancel()
	if err := <-done; !errors.Is(err, ErrCanceled) || !errors.Is(err, ErrMigrationFailed) {
		t.Fatalf("open = %v, want ErrCanceled and ErrMigrationFailed", err)
	}
	interrupted(t, path)
}

func TestAKilledMigrationKeepsTheOldVersion(t *testing.T) {
	if path := childPath(t, migrateChildEnv); path != "" {
		_, err := open(context.Background(), path, defaults, with(upTo(threeSteps, 1), slowMigration))
		t.Fatalf("the endless migration ended: %v", err)
	}
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, threeSteps, 1, "INSERT INTO a(v) VALUES ('kept')")
	cmd, _, _ := child(t, "TestAKilledMigrationKeepsTheOldVersion", migrateChildEnv, path)
	migrating(t, path)
	// SIGKILL: no rollback, no Close; the next open finds uncommitted pages in the WAL.
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("the child exited normally, want killed")
	}
	interrupted(t, path)
}

func TestAnotherProcessHoldingTheLockMakesTheMigrationBusy(t *testing.T) {
	if path := childPath(t, lockChildEnv); path != "" {
		holdLock(t, path)
		return
	}
	path := filepath.Join(t.TempDir(), "library.sqlite")
	at(t, path, rowsSchema, 1)
	cmd, stdin, lines := child(t, "TestAnotherProcessHoldingTheLockMakesTheMigrationBusy", lockChildEnv, path)
	await(t, lines, "locked")
	s, err := open(deadline(t, 10*time.Second), path, shortBusy(200*time.Millisecond), with(rowsSchema, "CREATE TABLE extra(x)"))
	if s != nil {
		_ = s.Close()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("open while another process holds the lock = %v, want ErrBusy", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the child: %v", err)
	}
	if st := stateOf(t, path); st.version != 1 {
		t.Fatalf("version %d, want 1", st.version)
	}
}
