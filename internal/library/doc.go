// Package library is the only place that talks to SQLite (STACK-04). Store (Open, Write, Read, Close)
// opens every database the way task 008 decided: modernc.org/sqlite, WAL, a full sync of every commit
// (F_FULLFSYNC on macOS), one writer taking its lock at BEGIN, a small read-only pool, never a reset or
// deleted file; its errors are classified into the package's kinds (ErrBusy, ErrFull, ...).
// CompletionStore (task 009) keeps the advisory verified-piece marks in their own database, with an
// interim user_version 1 schema that 014's migration policy takes over. SQLiteVersion reports the
// bundled SQLite. Decisions: docs/decisions/sqlite-driver.md, docs/decisions/engine-storage.md.
package library
