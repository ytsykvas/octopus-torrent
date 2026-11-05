package library

import (
	"context"
	"errors"
	"fmt"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"octopustorrent/internal/app/contracts"
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
	ErrIO         = errors.New("a disk read or write of the library database failed")
	// ErrOutcomeUnknown: a COMMIT of a write failed after it began to write (an I/O error, ...): the
	// change may be durable or not, and only the next open of the file shows which. It is joined
	// with the cause's own kind.
	ErrOutcomeUnknown = errors.New("it is not known whether the library change was stored")
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

// The answers of the repositories (task 015) that a service acts on. Each typed error matches its
// kind with errors.Is.
var (
	// ErrInvalid: a value the caller should have validated (a malformed ID, a path, a time the library
	// cannot store, ...). Nothing was written; it is a bug of the caller, never a user's path.
	ErrInvalid = errors.New("the library was given a value it does not store")
	// ErrNotFound: the library has no such torrent (NotFoundError).
	ErrNotFound = errors.New("the library has no such torrent")
	// ErrDuplicate: the library already has a torrent with one of the infohashes (DuplicateError).
	ErrDuplicate = errors.New("the library already has this torrent")
	// ErrOperationExists: the journal already has an operation with this ID (OperationExistsError).
	ErrOperationExists = errors.New("the operation journal already has this operation")
)

// NotFoundError names the torrent the library does not have.
type NotFoundError struct{ Torrent contracts.TorrentID }

func (e *NotFoundError) Error() string { return "library: no torrent " + string(e.Torrent) }
func (e *NotFoundError) Unwrap() error { return ErrNotFound }

// DuplicateError names the record that already has the torrent: the one with its v1 infohash, else
// the one with its v2 infohash.
type DuplicateError struct{ Existing contracts.TorrentID }

func (e *DuplicateError) Error() string {
	return "library: torrent " + string(e.Existing) + " already has this infohash"
}
func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// OperationExistsError names the operation the journal already has. SameRequest is true when the
// stored operation has the same fingerprint: a repeat of it, which a service answers with the stored
// status (api-contract), never by running it again.
type OperationExistsError struct {
	ID          contracts.OperationID
	SameRequest bool
}

func (e *OperationExistsError) Error() string {
	return fmt.Sprintf("library: the journal already has operation %s (same request: %v)", e.ID, e.SameRequest)
}
func (e *OperationExistsError) Unwrap() error { return ErrOperationExists }

// invalid is ErrInvalid with what was wrong.
func invalid(format string, args ...any) error {
	return &kindError{ErrInvalid, fmt.Errorf(format, args...)}
}

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
	sqlite3.SQLITE_IOERR:      ErrIO,
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
