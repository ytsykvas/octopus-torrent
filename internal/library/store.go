package library

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// readers is the size of the read-only pool.
const readers = 4

// settings are what open fixes for every database; the tests open with other values.
type settings struct {
	// busyTimeout is how long a connection waits for another one's lock before ErrBusy.
	busyTimeout time.Duration
	// walPragma switches the database to WAL.
	walPragma string
	// afterCheck runs between the read-only check and the migration's write lock, where another
	// store may change the file; a test changes it there.
	afterCheck func()
	// beforeCommit runs in Write after fn succeeded, right before COMMIT: a test makes a real failure
	// (a cancel, a full disk, a file that cannot grow) happen exactly there.
	beforeCommit func()
}

var defaults = settings{
	busyTimeout:  5 * time.Second,
	walPragma:    "PRAGMA journal_mode=WAL",
	afterCheck:   func() {},
	beforeCommit: func() {},
}

// store is one SQLite database opened the way task 008 decided:
//
//   - WAL, so readers never wait for the writer and the writer never waits for readers;
//   - synchronous=FULL with fullfsync on every connection: a commit returns only after the WAL is
//     on stable storage (F_FULLFSYNC; a plain fsync on macOS does not reach it);
//   - one writer connection whose transactions take the write lock at BEGIN (BEGIN IMMEDIATE), so
//     writes are serialized (one at a time, not in submission order) and a read lock is never
//     upgraded into a deadlock;
//   - a read-only pool of readers connections (query_only);
//   - only a connection that writes checkpoints: the writer, automatically and fully when the store
//     closes, and the connection that opened the file (prepare) when it closes as the file's last.
//
// A store never creates a database in place of an existing file and never deletes one.
type store struct {
	path  string
	file  os.FileInfo // the file open opened; a write or read checks the path still names it
	write *sql.DB
	read  *sql.DB
	// beforeCommit is settings.beforeCommit.
	beforeCommit func()

	closeOnce sync.Once
	closeErr  error
}

// connector opens connections of the modernc driver with a fixed DSN; unlike sql.Open it cannot fail.
// Opening a connection takes no context: it runs the DSN's PRAGMAs, which wait at most busyTimeout.
type connector struct{ dsn string }

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.Driver().Open(c.dsn) }
func (c connector) Driver() driver.Driver                        { return &sqlite.Driver{} }

// Whether a connection enforces foreign keys: every connection of a store does; only the connection
// that opens and migrates a file does not (see migrate).
const (
	foreignKeysOn  = "foreign_keys(1)"
	foreignKeysOff = "foreign_keys(0)"
)

// dsn is the URI of path with the settings every connection applies when it opens. The path is
// escaped, so a space, '?', '#' or '%' in a folder name is part of the file name. The driver sorts
// the _pragma list, so a setting is given once, never overridden by a later one.
func dsn(path string, busyTimeout time.Duration, foreignKeys string, params ...string) string {
	q := url.Values{}
	for _, pragma := range []string{
		fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()),
		"synchronous(FULL)",
		"fullfsync(1)",
		foreignKeys,
	} {
		q.Add("_pragma", pragma)
	}
	for i := 0; i+1 < len(params); i += 2 {
		q.Add(params[i], params[i+1])
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// open opens the database of sch at the absolute, clean path, creating it (readable by its owner
// only) when no file exists there. It refuses a path that is not a regular file (a symlink, a
// folder), and an empty or missing main file beside a WAL that holds commits: SQLite would discard
// that WAL. prepare then brings the file to the current version of sch
// before the store's own connections open. The exported openers (OpenLibrary, OpenCompletion) each
// pass their own schema; there is no way to open a file without one.
func open(ctx context.Context, path string, set settings, sch schema) (*store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("the library database path %q is not absolute and clean", path)
	}
	if main, wal := size(path), size(path+"-wal"); main == 0 && wal > 0 {
		return nil, &kindError{ErrCorrupt, fmt.Errorf("%s is empty or missing, but its WAL holds %d bytes of commits", path, wal)}
	}
	// Created here, not by SQLite, so the file is private; SQLite gives the -wal and -shm files the
	// same mode. O_EXCL leaves an existing file, or anything a symlink points to, untouched.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = f.Close()
	}
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, &kindError{ErrCannotOpen, err}
	}
	// Only a regular file at the path itself: through a symlink SQLite would open, and convert, an
	// empty file wherever the link points.
	file, err := os.Lstat(path)
	if err == nil && !file.Mode().IsRegular() {
		err = fmt.Errorf("%s is a %s, not a regular file", path, file.Mode().Type())
	}
	if err != nil {
		return nil, &kindError{ErrCannotOpen, err}
	}
	if err := prepare(ctx, path, set, sch); err != nil {
		return nil, err
	}
	write := sql.OpenDB(connector{dsn(path, set.busyTimeout, foreignKeysOn, "_txlock", "immediate")})
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	// mode=ro: a reader can neither write nor create a file, even if the path stops naming one.
	read := sql.OpenDB(connector{dsn(path, set.busyTimeout, foreignKeysOn, "mode", "ro", "_pragma", "query_only(1)")})
	read.SetMaxOpenConns(readers)
	read.SetMaxIdleConns(readers)
	return &store{path: path, file: file, write: write, read: read, beforeCommit: set.beforeCommit}, nil
}

// prepare runs on a connection of its own, closed before the store's connections open:
//
//  1. before anything is written, it reads whose file this is in a read-only transaction (a plain
//     BEGIN, no write lock, so a read-only file is recognised too) and refuses one that is not a
//     database of sch, or is of a newer version, unchanged and in its own journal mode;
//  2. it switches the file to WAL;
//  3. it runs the pending migrations in one transaction (migrate); a failure is ErrMigrationFailed,
//     joined with the cause's kind, and keeps the old version and every row.
//
// The connection does not enforce foreign keys (migrate says why); when it closes as the file's last
// connection, SQLite checkpoints the WAL into the file and removes it.
func prepare(ctx context.Context, path string, set settings, sch schema) (err error) {
	db := sql.OpenDB(connector{dsn(path, set.busyTimeout, foreignKeysOff, "_txlock", "immediate")})
	db.SetMaxOpenConns(1)
	defer func() { err = errors.Join(err, db.Close()) }()
	var from int
	if err := run(ctx, db, &sql.TxOptions{ReadOnly: true}, func(tx txn) (err error) {
		from, err = examine(tx, sch)
		return err
	}); err != nil {
		return err
	}
	var mode string
	if err := db.QueryRowContext(ctx, set.walPragma).Scan(&mode); err != nil {
		return classify(err)
	}
	if mode != "wal" {
		// SQLite answers with the mode it kept when it cannot switch, and a read-only file is already
		// an error above; any other mode would silently bring other guarantees.
		return fmt.Errorf("SQLite kept journal_mode %q instead of wal", mode)
	}
	if from < sch.current() {
		set.afterCheck()
		if err := run(ctx, db, nil, func(tx txn) error { return migrate(tx, sch) }); err != nil {
			return &kindError{ErrMigrationFailed, fmt.Errorf("the %s database from version %d to %d: %w", sch.name, from, sch.current(), err)}
		}
	}
	return nil
}

// size is the size of the file at path, 0 when there is none.
func size(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// check refuses an operation once the path no longer names the file open opened: the writer would
// commit into a deleted inode, and a new reader would find another file.
func (s *store) check() error {
	info, err := os.Lstat(s.path)
	if err != nil {
		return &kindError{ErrReplaced, err}
	}
	if !os.SameFile(info, s.file) {
		return &kindError{ErrReplaced, fmt.Errorf("%s is another file now", s.path)}
	}
	return nil
}

// txn is a transaction whose every statement runs under the context of its operation, so cancelling
// the operation interrupts the statement that is running (database/sql's methods without a context
// would run it to the end). A txn is valid only inside the function it was given to; rows from Query
// must be closed there too.
type txn struct {
	ctx context.Context
	tx  *sql.Tx
}

// Exec runs a statement that returns no rows.
func (t txn) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, query, args...)
}

// Query runs a statement that returns rows.
func (t txn) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(t.ctx, query, args...)
}

// QueryRow runs a statement that returns at most one row.
func (t txn) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(t.ctx, query, args...)
}

// Write runs fn in a write transaction and commits it. Writes are serialized: one at a time, but
// concurrent callers get the writer in no particular order (database/sql hands a freed connection to
// a random waiter), so an order between operations is the caller's to keep. Write returns nil only
// when the commit succeeded and the path still names the file it went into. An error before COMMIT
// keeps nothing of fn's changes; so does a COMMIT that the context's end prevented (ErrCanceled), or
// one that failed busy, full or on a constraint. Any other error from COMMIT (an I/O error, ...)
// also matches ErrOutcomeUnknown: the change may be durable or not, which only the next open of the
// file shows. So does a commit into a file that was deleted, moved or replaced meanwhile
// (ErrReplaced): SQLite commits into it without an error, but the library at the path lacks the
// change. An error of fn's own comes back unchanged; one that SQLite or the context caused comes back
// with its kind, wrapping the original.
func (s *store) Write(ctx context.Context, fn func(txn) error) error {
	if err := s.check(); err != nil {
		return err
	}
	tx, err := begin(ctx, s.write, nil, fn)
	if err != nil {
		return err
	}
	s.beforeCommit()
	err = tx.Commit()
	switch {
	case err == nil:
		// The file may have been lost between the check before BEGIN and the commit.
		if lost := s.check(); lost != nil {
			return &kindError{ErrOutcomeUnknown, lost}
		}
		return nil
	case errors.Is(err, sql.ErrTxDone) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		// database/sql did not run COMMIT: the context ended first, and the transaction was rolled
		// back (database/sql/sql.go, Tx.Commit checks the context before it commits).
		return classify(errors.Join(ctx.Err(), err))
	}
	// COMMIT ran (modernc runs it under context.Background and rolls back a transaction SQLite left
	// open), so its own result decides, whatever happened to the context meanwhile.
	kind := classify(err)
	if errors.Is(kind, ErrBusy) || errors.Is(kind, ErrFull) || errors.Is(kind, ErrConstraint) {
		return kind
	}
	return &kindError{ErrOutcomeUnknown, kind}
}

// Read runs fn in a read-only transaction: one consistent snapshot, never blocked by the writer.
func (s *store) Read(ctx context.Context, fn func(txn) error) error {
	if err := s.check(); err != nil {
		return err
	}
	return run(ctx, s.read, &sql.TxOptions{ReadOnly: true}, fn)
}

// run runs fn in a transaction and commits it: Read and the migration. A failed COMMIT is reported
// with its kind; Write has its own rule for what a failed COMMIT means.
func run(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(txn) error) error {
	tx, err := begin(ctx, db, opts, fn)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		// A context cancelled during fn ends the transaction before Commit, which then reports only
		// that the transaction is done. If both happened, cancellation is the kind reported; the
		// commit's own error stays reachable through errors.As.
		return classify(errors.Join(ctx.Err(), err))
	}
	return nil
}

// begin starts a transaction and runs fn in it. When fn fails, the transaction is rolled back and
// fn's error returned with its kind; when fn panics, it is rolled back before the panic goes on, so a
// caller that recovers is not left with the connection held until its context ends.
func begin(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(txn) error) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return nil, classify(err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback() // the panic is what the caller must learn of
			panic(p)
		}
	}()
	if err := fn(txn{ctx, tx}); err != nil {
		_ = tx.Rollback() // the transaction is discarded; fn's error is the one that matters
		return nil, classify(err)
	}
	return tx, nil
}

// Close closes the read pool, checkpoints the whole WAL into the database file and truncates it,
// then closes the writer. A checkpoint that another process's reader blocks is ErrBusy: nothing is
// lost, the WAL stays and the next open uses it. On a full disk the checkpoint fails with ErrFull,
// with the same outcome. On a read-only file it cannot run (ErrReadOnly), and nothing was written.
// The checkpoint waits at most busyTimeout (5 s) for another process's reader. Close runs once;
// later calls return its result.
func (s *store) Close() error {
	s.closeOnce.Do(func() {
		errs := []error{s.read.Close()}
		var busy, logged, done int
		err := s.write.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &done)
		switch {
		case err != nil:
			errs = append(errs, classify(err))
		case busy != 0:
			errs = append(errs, &kindError{ErrBusy, fmt.Errorf("the checkpoint copied %d of %d WAL frames", done, logged)})
		}
		s.closeErr = errors.Join(append(errs, s.write.Close())...)
	})
	return s.closeErr
}
