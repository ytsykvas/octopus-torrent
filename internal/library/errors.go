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
)

// kinds lists every kind, for the tests.
var kinds = []error{ErrBusy, ErrFull, ErrCannotOpen, ErrReadOnly, ErrCanceled, ErrConstraint, ErrCorrupt}

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
