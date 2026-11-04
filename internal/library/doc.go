// Package library is the only place that talks to SQLite (STACK-04). Every database is opened through
// its schema (task 014): an application_id that marks the file as ours, an explicit list of embedded
// SQL migrations (migrations/<schema>/NNNN_<what>.sql) and a structural check of the file against what
// its migrations build. A file of another application, or a newer version, is refused with its rows
// unchanged (ErrUnknownSchema, ErrUnsupportedVersion; SQLite may still checkpoint the file's own WAL or
// roll back its own hot journal, as any reader would); pending migrations run in one transaction, and a failed one
// keeps the old version and every row (ErrMigrationFailed). The openers are OpenLibrary (the owner's
// library: torrents, their files, payload records, settings and the operation journal) and
// OpenCompletion (the advisory verified-piece marks of task 009, in their own file).
//
// Each store is opened the way task 008 decided: modernc.org/sqlite, WAL, a full sync of every commit
// (F_FULLFSYNC on macOS), one writer taking its lock at BEGIN, a small read-only pool, never a reset or
// deleted file; its errors are classified into the package's kinds (ErrBusy, ErrFull, ...).
// SQLiteVersion reports the bundled SQLite. Decisions: docs/decisions/sqlite-driver.md,
// docs/decisions/engine-storage.md, docs/decisions/library-schema-and-data-folder.md.
package library
