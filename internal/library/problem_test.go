package library

import (
	"context"
	"errors"
	"testing"

	"octopustorrent/internal/app/contracts"
)

func TestEveryKindHasItsCodeAndKeepsItsCause(t *testing.T) {
	cause := errors.New("cause")
	kinded := func(kind error) error { return &kindError{kind, cause} }
	for _, c := range []struct {
		err     error
		code    string
		torrent contracts.TorrentID
	}{
		{kinded(ErrOutcomeUnknown), contracts.CodeLibraryOutcomeUnknown, ""},
		{kinded(ErrMigrationFailed), contracts.CodeLibraryMigrationFailed, ""},
		{kinded(ErrUnsupportedVersion), contracts.CodeLibraryNewerVersion, ""},
		{kinded(ErrUnknownSchema), contracts.CodeLibraryNotALibrary, ""},
		{kinded(ErrCanceled), contracts.CodeOperationCancelled, ""},
		{kinded(ErrBusy), contracts.CodeLibraryBusy, ""},
		{kinded(ErrFull), contracts.CodeLibraryDiskFull, ""},
		{kinded(ErrReadOnly), contracts.CodeLibraryReadOnly, ""},
		{kinded(ErrCorrupt), contracts.CodeLibraryDamaged, ""},
		{kinded(ErrCannotOpen), contracts.CodeLibraryUnavailable, ""},
		{kinded(ErrReplaced), contracts.CodeLibraryUnavailable, ""},
		{kinded(ErrIO), contracts.CodeLibraryUnavailable, ""},
		{&DuplicateError{idA}, contracts.CodeTorrentDuplicate, idA},
		{&OperationExistsError{ID: opA}, contracts.CodeOperationIDConflict, ""},
		{&NotFoundError{idB}, contracts.CodeTorrentNotFound, idB},
		// A repeat of the same request is the service's to answer from the journal: reaching the
		// mapping means it did not.
		{&OperationExistsError{ID: opA, SameRequest: true}, contracts.CodeInternalUnexpected, ""},
		{kinded(ErrInvalid), contracts.CodeInternalUnexpected, ""},
		{kinded(ErrConstraint), contracts.CodeInternalUnexpected, ""},
		{errors.New("sql: database is closed"), contracts.CodeInternalUnexpected, ""},
		// Joined kinds: the first of the table wins.
		{&kindError{ErrOutcomeUnknown, kinded(ErrIO)}, contracts.CodeLibraryOutcomeUnknown, ""},
		{&kindError{ErrMigrationFailed, kinded(ErrFull)}, contracts.CodeLibraryMigrationFailed, ""},
		{&kindError{ErrMigrationFailed, kinded(ErrCanceled)}, contracts.CodeLibraryMigrationFailed, ""},
		{&kindError{ErrOutcomeUnknown, &kindError{ErrCanceled, context.Canceled}}, contracts.CodeLibraryOutcomeUnknown, ""},
	} {
		problem := Problem(c.err)
		switch {
		case problem == nil || problem.Code != c.code:
			t.Errorf("Problem(%v) = %v, want %s", c.err, problem, c.code)
		case c.torrent == "" && problem.TorrentID != nil, c.torrent != "" && (problem.TorrentID == nil || *problem.TorrentID != c.torrent):
			t.Errorf("Problem(%v) names torrent %v, want %q", c.err, problem.TorrentID, c.torrent)
		case problem.OperationID != nil || len(problem.Params) != 0:
			t.Errorf("Problem(%v) = %+v: no operation and no parameters expected", c.err, problem)
		case !errors.Is(problem, c.err):
			t.Errorf("Problem(%v) lost its cause", c.err)
		}
	}
	if Problem(nil) != nil {
		t.Error("Problem(nil) is not nil")
	}
}

func TestTheRepositoriesErrorsNameWhatTheyAreAbout(t *testing.T) {
	for err, want := range map[error]string{
		&NotFoundError{idA}:  "library: no torrent " + idA,
		&DuplicateError{idB}: "library: torrent " + idB + " already has this infohash",
		&OperationExistsError{ID: opA, SameRequest: true}: "library: the journal already has operation " + opA + " (same request: true)",
	} {
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}
