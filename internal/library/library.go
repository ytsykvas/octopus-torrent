package library

import (
	"context"
	_ "embed" // the migrations are embedded
)

//go:embed migrations/library/0001_initial.sql
var libraryV1 string

// librarySchema is the library's: application_id "OTLB", one migration per file of
// migrations/library, in order.
var librarySchema = schema{name: "library", applicationID: 0x4F544C42, migrations: []string{libraryV1}}

// Library is the owner's library: torrent records, their files, what the last run left for the
// payload, the settings and the operation journal (task 014). Its methods are the repositories
// (task 015): each is one transaction over values prepared in memory (AddTorrent, SetManualPaused,
// SetFilesWanted, SaveSettings, FinishOperation; Torrents, Settings, Operation), and Problem maps
// their errors to AppError codes.
type Library struct {
	store *store
}

// OpenLibrary opens, creates or migrates the library at an absolute, clean path. A file that is not
// a library, or a library of a newer version, is refused unchanged (ErrUnknownSchema,
// ErrUnsupportedVersion); a failed migration is ErrMigrationFailed and keeps the old version.
func OpenLibrary(ctx context.Context, path string) (*Library, error) {
	s, err := open(ctx, path, defaults, librarySchema)
	if err != nil {
		return nil, err
	}
	return &Library{s}, nil
}

// Close checkpoints and closes the library; its error is the store's.
func (l *Library) Close() error {
	return l.store.Close()
}
