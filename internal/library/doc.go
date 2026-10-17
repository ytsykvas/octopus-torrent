// Package library is the only place that talks to SQLite (STACK-04). Task 008 chose the driver,
// modernc.org/sqlite, and fixed how every database of the application is opened: WAL, a full sync
// of every commit (F_FULLFSYNC on macOS), one writer connection taking its lock at BEGIN, a small
// read-only pool, and errors classified into the package's own kinds. The decision and its evidence
// are in docs/decisions/sqlite-driver.md; schemas and migrations arrive with task 014, repositories
// with 015.
package library
