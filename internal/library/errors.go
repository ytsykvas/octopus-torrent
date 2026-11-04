package library

import (
	"context"
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// The kinds of failure a caller can act on. A classified error matches its kind with errors.Is and
// still carries the driver's error, with SQLite's own message, for errors.As and diagnostics.
var (
	ErrBusy       = errors.New("the library database is locked by another connection")
	ErrFull       = errors.New("the disk holding the library database is full")
	ErrCannotOpen = errors.New("the library database cannot be opened")
	ErrReadOnly   = errors.New("the library database is read-only")
	ErrCanceled   = errors.New("the library operation was cancelled")
	ErrConstraint = errors.New("the library change breaks a constraint")
	ErrCorrupt    = errors.New("the library database is damaged or not a database")
	ErrReplaced   = errors.New("the library database file was deleted, moved or replaced while open")
)

// The refusals and the failure of a database's schema (task 014). None of them is ErrCorrupt, so a
// caller that rebuilds a damaged database cannot destroy a file written by a newer build or by
// another application. A refused file is never changed.
var (
	// ErrUnsupportedVersion: the file is ours, at a version a newer build wrote.
	ErrUnsupportedVersion = errors.New("the database was written by a newer version of the application")
	// ErrUnknownSchema: the file is not a database of this purpose: another application's, an
	// unstamped file with tables, or ours with another structure than its migrations build.
	ErrUnknownSchema = errors.New("the database is not one this application made for this purpose")
	// ErrMigrationFailed: the migration to the current version failed and kept nothing; the file is
	// at its old version with every row. It is joined with the cause's own kind (ErrFull, ErrBusy,
	// ErrReadOnly, ErrCanceled, ErrConstraint, ...).
	ErrMigrationFailed = errors.New("the database could not be migrated to the current version")
)

// kindError is a classified error: the kind, and what caused it.
type kindError struct{ kind, cause error }

func (e *kindError) Error() string   { return e.kind.Error() + ": " + e.cause.Error() }
func (e *kindError) Unwrap() []error { return []error{e.kind, e.cause} }

// primary maps SQLite's primary result codes to kinds.
var primary = map[int]error{
	sqlite3.SQLITE_BUSY:       ErrBusy,
	sqlite3.SQLITE_FULL:       ErrFull,
	sqlite3.SQLITE_CANTOPEN:   ErrCannotOpen,
	sqlite3.SQLITE_READONLY:   ErrReadOnly,
	sqlite3.SQLITE_CONSTRAINT: ErrConstraint,
	sqlite3.SQLITE_NOTADB:     ErrCorrupt,
	sqlite3.SQLITE_CORRUPT:    ErrCorrupt,
}

// classify gives err its kind. A cancelled or expired context is ErrCanceled; a SQLite error is
// classified by its primary result code (the driver reports extended codes); anything else, such as
// the caller's own error, is returned unchanged.
func classify(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &kindError{ErrCanceled, err}
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		if kind, ok := primary[se.Code()&0xff]; ok {
			return &kindError{kind, err}
		}
	}
	return err
}
