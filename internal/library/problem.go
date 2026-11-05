package library

import (
	"errors"

	"octopustorrent/internal/app/contracts"
)

// codes maps the library's kinds to AppError codes, in order: the first kind an error matches
// decides, so a joined error (a failed migration and its full disk, an unknown outcome and its I/O
// error) gets the code that tells the user what happened.
var codes = []struct {
	kind error
	code string
}{
	{ErrOutcomeUnknown, contracts.CodeLibraryOutcomeUnknown},
	{ErrMigrationFailed, contracts.CodeLibraryMigrationFailed},
	{ErrUnsupportedVersion, contracts.CodeLibraryNewerVersion},
	{ErrUnknownSchema, contracts.CodeLibraryNotALibrary},
	{ErrCanceled, contracts.CodeOperationCancelled},
	{ErrBusy, contracts.CodeLibraryBusy},
	{ErrFull, contracts.CodeLibraryDiskFull},
	{ErrReadOnly, contracts.CodeLibraryReadOnly},
	{ErrCorrupt, contracts.CodeLibraryDamaged},
	{ErrCannotOpen, contracts.CodeLibraryUnavailable},
	{ErrReplaced, contracts.CodeLibraryUnavailable},
	{ErrIO, contracts.CodeLibraryUnavailable},
}

// Problem is the one mapping of the library's errors to AppError codes (nil for nil); the error stays
// attached as the cause, for diagnostics, and never reaches the UI. It is for the library: the marks
// store's errors are the engine adapter's to map. ErrInvalid and ErrConstraint (a bug of the caller),
// a repeat of a stored operation (the service answers it from the journal first), and anything
// unclassified are internal.unexpected.
func Problem(err error) *contracts.AppError {
	if err == nil {
		return nil
	}
	for _, c := range codes {
		if errors.Is(err, c.kind) {
			return contracts.NewAppError(c.code, nil).WithCause(err)
		}
	}
	var duplicate *DuplicateError
	var missing *NotFoundError
	var exists *OperationExistsError
	switch {
	case errors.As(err, &duplicate):
		return contracts.NewAppError(contracts.CodeTorrentDuplicate, nil).WithTorrent(duplicate.Existing).WithCause(err)
	case errors.As(err, &missing):
		return contracts.NewAppError(contracts.CodeTorrentNotFound, nil).WithTorrent(missing.Torrent).WithCause(err)
	case errors.As(err, &exists) && !exists.SameRequest:
		return contracts.NewAppError(contracts.CodeOperationIDConflict, nil).WithCause(err)
	}
	return contracts.NewAppError(contracts.CodeInternalUnexpected, nil).WithCause(err)
}
