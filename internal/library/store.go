package library

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"modernc.org/sqlite"
)

// readers is the size of the read-only pool.
const readers = 4

// walPragma switches a database to WAL; a seam for the tests.
var walPragma = "PRAGMA journal_mode=WAL"

// busyTimeout is how long a connection waits for another one's lock before ErrBusy; a seam for the
// tests.
var busyTimeout = 5 * time.Second

// Store is one SQLite database opened the way task 008 decided:
//
//   - WAL, so readers never wait for the writer and the writer never waits for readers;
//   - synchronous=FULL with fullfsync on every connection: a commit returns only after the WAL is
//     on stable storage (F_FULLFSYNC; a plain fsync on macOS does not reach it);
//   - one writer connection whose transactions take the write lock at BEGIN (BEGIN IMMEDIATE), so
//     writes are applied in one order and a read lock is never upgraded into a deadlock;
//   - a read-only pool of readers connections (query_only);
//   - only the writer checkpoints: automatically, and fully when the store closes.
//
// Open never creates a database in place of an existing file and never deletes one.
type Store struct {
	write *sql.DB
	read  *sql.DB

	closing sync.Once
	closed  error
}

// connector opens connections of the modernc driver with a fixed DSN; unlike sql.Open it cannot fail.
type connector struct{ dsn string }

func (c connector) Connect(context.Context) (driver.Conn, error) { return c.Driver().Open(c.dsn) }
func (c connector) Driver() driver.Driver                        { return &sqlite.Driver{} }

// dsn is the URI of path with the settings every connection applies when it opens. The path is
// escaped, so a space, '?', '#' or '%' in a folder name is part of the file name.
func dsn(path string, params ...string) string {
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

// Open opens the database at the absolute path, creating it only when no file exists there, and
// switches it to WAL.
func Open(ctx context.Context, path string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("the library database path %q is not absolute", path)
	}
	write := sql.OpenDB(connector{dsn(path, "_txlock", "immediate")})
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	var mode string
	if err := write.QueryRowContext(ctx, walPragma).Scan(&mode); err != nil {
		return nil, errors.Join(classify(err), write.Close())
	}
	if mode != "wal" {
		// SQLite answers with the mode it kept when it cannot switch, and a read-only file is already
		// an error above; any other mode would silently bring other guarantees.
		return nil, errors.Join(fmt.Errorf("SQLite kept journal_mode %q instead of wal", mode), write.Close())
	}
	read := sql.OpenDB(connector{dsn(path, "_pragma", "query_only(1)")})
	read.SetMaxOpenConns(readers)
	read.SetMaxIdleConns(readers)
	return &Store{write: write, read: read}, nil
}

// Tx is a transaction whose every statement runs under the context of its operation, so cancelling
// the operation interrupts the statement that is running (database/sql's methods without a context
// would run it to the end).
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

// Write runs fn in a write transaction and commits it. Writes are applied one at a time, in the order
// they get the writer connection. It returns nil only when the commit succeeded; on any error nothing
// of fn's changes is kept.
func (s *Store) Write(ctx context.Context, fn func(Tx) error) error {
	return run(ctx, s.write, nil, fn)
}

// Read runs fn in a read-only transaction: one consistent snapshot, never blocked by the writer.
func (s *Store) Read(ctx context.Context, fn func(Tx) error) error {
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
		// that the transaction is done.
		return classify(errors.Join(ctx.Err(), err))
	}
	return nil
}

// Close closes the read pool, checkpoints the whole WAL into the database file and truncates it,
// then closes the writer. A checkpoint that another process's reader blocks is ErrBusy: nothing is
// lost, the WAL stays and the next open uses it. Close runs once; later calls return its result.
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
