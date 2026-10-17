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

// settings are what Open fixes for every database; the tests open with other values.
type settings struct {
	// busyTimeout is how long a connection waits for another one's lock before ErrBusy.
	busyTimeout time.Duration
	// walPragma switches the database to WAL.
	walPragma string
}

var defaults = settings{busyTimeout: 5 * time.Second, walPragma: "PRAGMA journal_mode=WAL"}

// Store is one SQLite database opened the way task 008 decided:
//
//   - WAL, so readers never wait for the writer and the writer never waits for readers;
//   - synchronous=FULL with fullfsync on every connection: a commit returns only after the WAL is
//     on stable storage (F_FULLFSYNC; a plain fsync on macOS does not reach it);
//   - one writer connection whose transactions take the write lock at BEGIN (BEGIN IMMEDIATE), so
//     writes are serialized (one at a time, not in submission order) and a read lock is never
//     upgraded into a deadlock;
//   - a read-only pool of readers connections (query_only);
//   - only the writer checkpoints: automatically, and fully when the store closes.
//
// Open never creates a database in place of an existing file and never deletes one.
type Store struct {
	path  string
	file  os.FileInfo // the file Open opened; a write or read checks the path still names it
	write *sql.DB
	read  *sql.DB

	closing sync.Once
	closed  error
}

// connector opens connections of the modernc driver with a fixed DSN; unlike sql.Open it cannot fail.
// Opening a connection takes no context: it runs the DSN's PRAGMAs, which wait at most busyTimeout.
type connector struct{ dsn string }

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.Driver().Open(c.dsn) }
func (c connector) Driver() driver.Driver                        { return &sqlite.Driver{} }

// dsn is the URI of path with the settings every connection applies when it opens. The path is
// escaped, so a space, '?', '#' or '%' in a folder name is part of the file name.
func dsn(path string, busyTimeout time.Duration, params ...string) string {
	q := url.Values{}
	for _, pragma := range []string{
		fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()),
		"synchronous(FULL)",
		"fullfsync(1)",
		"foreign_keys(1)",
	} {
		q.Add("_pragma", pragma)
	}
	for i := 0; i+1 < len(params); i += 2 {
		q.Add(params[i], params[i+1])
	}
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Open opens the database at the absolute, clean path, creating it (readable by its owner only) when
// no file exists there, and switches it to WAL. It refuses an empty or missing main file beside a WAL
// that holds commits: SQLite would discard that WAL.
func Open(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, defaults)
}

func open(ctx context.Context, path string, set settings) (*Store, error) {
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
	file, err := os.Stat(path)
	if err != nil {
		return nil, &kindError{ErrCannotOpen, err}
	}
	write := sql.OpenDB(connector{dsn(path, set.busyTimeout, "_txlock", "immediate")})
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	var mode string
	if err := write.QueryRowContext(ctx, set.walPragma).Scan(&mode); err != nil {
		return nil, errors.Join(classify(err), write.Close())
	}
	if mode != "wal" {
		// SQLite answers with the mode it kept when it cannot switch, and a read-only file is already
		// an error above; any other mode would silently bring other guarantees.
		return nil, errors.Join(fmt.Errorf("SQLite kept journal_mode %q instead of wal", mode), write.Close())
	}
	// mode=ro: a reader can neither write nor create a file, even if the path stops naming one.
	read := sql.OpenDB(connector{dsn(path, set.busyTimeout, "mode", "ro", "_pragma", "query_only(1)")})
	read.SetMaxOpenConns(readers)
	read.SetMaxIdleConns(readers)
	return &Store{path: path, file: file, write: write, read: read}, nil
}

// size is the size of the file at path, 0 when there is none.
func size(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// check refuses an operation once the path no longer names the file Open opened: the writer would
// commit into a deleted inode, and a new reader would find another file.
func (s *Store) check() error {
	info, err := os.Stat(s.path)
	if err != nil {
		return &kindError{ErrReplaced, err}
	}
	if !os.SameFile(info, s.file) {
		return &kindError{ErrReplaced, fmt.Errorf("%s is another file now", s.path)}
	}
	return nil
}

// Tx is a transaction whose every statement runs under the context of its operation, so cancelling
// the operation interrupts the statement that is running (database/sql's methods without a context
// would run it to the end). A Tx is valid only inside the function it was given to; rows from Query
// must be closed there too.
type Tx struct {
	ctx context.Context
	tx  *sql.Tx
}

// Exec runs a statement that returns no rows.
func (t Tx) Exec(query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(t.ctx, query, args...)
}

// Query runs a statement that returns rows.
func (t Tx) Query(query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(t.ctx, query, args...)
}

// QueryRow runs a statement that returns at most one row.
func (t Tx) QueryRow(query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(t.ctx, query, args...)
}

// Write runs fn in a write transaction and commits it. Writes are serialized: one at a time, but
// concurrent callers get the writer in no particular order (database/sql hands a freed connection to
// a random waiter), so an order between operations is the caller's to keep. Write returns nil only
// when the commit succeeded. An error before COMMIT, or a busy, full or constraint error at COMMIT,
// keeps nothing of fn's changes; an I/O error from COMMIT has an unknown outcome, and the caller reads
// the state again. An error of fn's own comes back unchanged; one that SQLite or the context caused
// comes back with its kind, wrapping the original.
func (s *Store) Write(ctx context.Context, fn func(Tx) error) error {
	if err := s.check(); err != nil {
		return err
	}
	return run(ctx, s.write, nil, fn)
}

// Read runs fn in a read-only transaction: one consistent snapshot, never blocked by the writer.
func (s *Store) Read(ctx context.Context, fn func(Tx) error) error {
	if err := s.check(); err != nil {
		return err
	}
	return run(ctx, s.read, &sql.TxOptions{ReadOnly: true}, fn)
}

func run(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return classify(err)
	}
	if err := fn(Tx{ctx, tx}); err != nil {
		_ = tx.Rollback() // the transaction is discarded; fn's error is the one that matters
		return classify(err)
	}
	if err := tx.Commit(); err != nil {
		// A context cancelled during fn ends the transaction before Commit, which then reports only
		// that the transaction is done. If both happened, cancellation is the kind reported; the
		// commit's own error stays reachable through errors.As.
		return classify(errors.Join(ctx.Err(), err))
	}
	return nil
}

// Close closes the read pool, checkpoints the whole WAL into the database file and truncates it,
// then closes the writer. A checkpoint that another process's reader blocks is ErrBusy: nothing is
// lost, the WAL stays and the next open uses it. On a full disk the checkpoint fails with ErrFull,
// with the same outcome. The checkpoint waits at most busyTimeout (5 s) for another process's
// reader. Close runs once; later calls return its result.
func (s *Store) Close() error {
	s.closing.Do(func() {
		errs := []error{s.read.Close()}
		var busy, logged, done int
		err := s.write.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &done)
		switch {
		case err != nil:
			errs = append(errs, classify(err))
		case busy != 0:
			errs = append(errs, &kindError{ErrBusy, fmt.Errorf("the checkpoint copied %d of %d WAL frames", done, logged)})
		}
		s.closed = errors.Join(append(errs, s.write.Close())...)
	})
	return s.closed
}
