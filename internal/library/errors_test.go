package library

import (
	"errors"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// kinds lists every exported error.
var kinds = []error{ErrBusy, ErrFull, ErrCannotOpen, ErrReadOnly, ErrCanceled, ErrConstraint, ErrCorrupt, ErrReplaced, ErrUnsupportedVersion}

func TestAnUnclassifiedSQLiteErrorPassesThrough(t *testing.T) {
	s, _ := withTable(t)
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		_, err := tx.Exec("NOT SQL")
		return err
	})
	var se *sqlite.Error
	if !errors.As(err, &se) {
		t.Fatalf("Write = %v, want the driver's error", err)
	}
	for _, kind := range kinds {
		if errors.Is(err, kind) {
			t.Errorf("a syntax error was classified as %v", kind)
		}
	}
}
