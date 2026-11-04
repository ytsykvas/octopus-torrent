# Library store: how SQLite is opened, written and read

**Status:** `implemented` (the library's schema 1 and its migrations since 014; the repositories are 015's)  
**Requirements:** `STACK-04`, `SET-02`, `AI-07`  
**Related tasks:** [008](../../product/tasks/008-sqlite-driver-durability-spike.md), [014](../../product/tasks/014-sqlite-schema-migrations.md); next [015](../../product/tasks/015-transactional-repositories.md), [016](../../product/tasks/016-migration-backup-corruption-recovery.md), [028](../../product/tasks/028-application-startup-quit.md)  
**Last verified version:** branch `task/014-sqlite-schema`  
**Last updated:** 2026-11-04

## Goal and behavior

```
main.run (the application start) or a test
  → library.OpenLibrary(ctx, "<dataDir>/library.sqlite")
      open → prepare (its own connection, foreign keys off):
        examine in a read-only transaction   whose file? refuse another application's or a newer one, unchanged
        PRAGMA journal_mode=WAL
        migrate in one BEGIN IMMEDIATE        examine again, application_id, pending migrations, user_version, foreign_key_check
      → the store's pools                     writer of 1 (BEGIN IMMEDIATE) + 4 read-only readers, foreign keys on
  → store.Write(ctx, func(tx library.Tx) error)   nil only after a durable commit
  → store.Read(ctx, func(tx library.Tx) error)    one consistent snapshot, never waits for the writer
  → Library.Close()                               readers closed, WAL checkpointed and truncated, writer closed
```

- **Implemented in 008:** the way every database is opened, written, read and closed; its error
  kinds; `library.SQLiteVersion`, which the product shows in the runtime check.
- **Implemented in 014:** every database is opened through its schema (`application_id`, embedded
  migrations, a structural check); the library's schema 1; the marks store under the same policy;
  the application opens the library in its data folder ([data folder](data-folder.md)) before the
  window and closes it last.
- **Not yet implemented:** repositories and the mapping to `AppError` (015); backup before a
  migration and corruption recovery (016); the start failure shown to the user (028).

The decisions and the alternatives are in the ADRs [sqlite-driver](../decisions/sqlite-driver.md)
(opening, writing, errors) and [library-schema-and-data-folder](../decisions/library-schema-and-data-folder.md)
(schema 1, the migration policy, the data folder).

**The schemas.** A schema is an `application_id`, a name and the list of
`internal/library/migrations/<schema>/NNNN_<what>.sql` files, embedded one by one. The library
(`OpenLibrary`, "OTLB") holds `torrents`, `torrent_files`, `payload_records`, `payload_files`,
`settings`, `operations` and `operation_torrents` (the ADR has every column and constraint);
`Library` exposes only `Close` until 015 adds the repositories. A new file (no id, version 0, no
object) is stamped and migrated; ours at an older version with the structure its migrations build is
migrated; ours at a newer version is `ErrUnsupportedVersion`; anything else is `ErrUnknownSchema`.
A failed migration is `ErrMigrationFailed` (with the cause's kind) and keeps the old version and
every row.

**A second database, from 009.** `library.OpenCompletion(ctx, path)` opens the verified-piece marks of
the engine, `<dataDir>/piece-completion.sqlite`, under the same policy ("OTPC", migration 1 is 009's
table `piece_completion(infohash, piece, complete)`). `SaveMarks` writes a batch in one transaction,
`LoadMarks` returns one torrent's marks. A newer store is `ErrUnsupportedVersion` and another
purpose (a library, 009's unstamped file) is `ErrUnknownSchema`, never `ErrCorrupt`. Its user is
`filestore.Completion`; see [engine storage](engine-storage.md).

## Architecture analysis before the change

- **Layer.** `internal/library` is the repository layer of the
  [architecture](../../product/APPLICATION_ARCHITECTURE.md#6-sqlite-and-restore). It is the only
  package that imports the driver or sends SQL.
- **Owner.** The `Store` belongs to whoever opened it: the application start (`main.run`, 014;
  its user-visible form is 028), or a test.
- **Side effects.** It writes only the database file and its `-wal` and `-shm` files. It never
  deletes them.
- **Cancellation.** Every statement runs under the context of its operation, through
  `library.Tx`.
- **After a crash.** The next open replays the WAL; a migration killed midway left only
  uncommitted pages, which SQLite discards, and the next open migrates again.
- **Reuse.** No earlier code to reuse; the test patterns follow 007 (child processes, seams as
  package variables, bounded waits).

## Contracts and data

| API | Contract |
|---|---|
| `OpenLibrary(ctx, path)`, `OpenCompletion(ctx, path)` | `path` must be absolute and clean. A file is created (`0600`, `O_EXCL`) only if nothing exists there. A path that is not a regular file is `ErrCannotOpen` (a symlink, even to an empty file elsewhere that SQLite would make a database; a folder). An empty or missing main file beside a WAL with commits is refused. Whose file it is, is read before anything is written (a refusal leaves bytes, inode and journal mode unchanged); the journal is switched to WAL (another mode is an error); pending migrations run in one transaction. |
| `Write(ctx, fn)` | One write at a time, taking the lock at `BEGIN`; serialized, not FIFO (concurrent callers get the writer in no particular order, so an order between operations is the service's). Returns nil only when the commit succeeded. An error before `COMMIT`, or busy/full/constraint at `COMMIT`, keeps nothing; an I/O error from `COMMIT` has an unknown outcome, so the caller reads again. An error of `fn`'s own comes back unchanged; one that SQLite or the context caused comes back with its kind, wrapping the original. If the context was cancelled and the commit also failed, the kind is `ErrCanceled`. |
| `Read(ctx, fn)` | A read-only transaction on one of 4 readers (`mode=ro`): a consistent snapshot. `Write` and `Read` first check that the path still names the opened file. |
| `Tx.Exec`, `Query`, `QueryRow` | Run under the operation's context, so a cancel interrupts the running statement. |
| `Close()` | Runs once. Its checkpoint waits at most `busy_timeout` (5 s) for another process's reader. It returns `ErrBusy` if another process's reader (or another store's checkpoint) blocks the checkpoint, or `ErrFull` on a full disk. In both cases nothing is lost: the WAL stays for the next open. On a read-only file the checkpoint cannot run: `ErrReadOnly`, and nothing was written. |
| `SQLiteVersion(ctx)` | `sqlite_version()` of the bundled SQLite, from an in-memory database. |

Every connection has `synchronous=FULL`, `fullfsync=1` and `busy_timeout=5000`; a store's
connections have `foreign_keys=1`, the opening connection `foreign_keys=0` (a table rebuild in a
migration would otherwise cascade-delete child rows; `foreign_key_check` runs before `COMMIT`).
Readers also have `query_only=1` and open with `mode=ro`.

**Errors.** A classified error matches its kind with `errors.Is`. `errors.As` reaches the driver's
`*sqlite.Error`.

| Kind | SQLite result | Example that proves it |
|---|---|---|
| `ErrBusy` | `SQLITE_BUSY` | another process holds `BEGIN IMMEDIATE` |
| `ErrFull` | `SQLITE_FULL` | a full disk image; `max_page_count` |
| `ErrCannotOpen` | `SQLITE_CANTOPEN`; a path that is not a regular file | a new database in a read-only folder; a symlink at the path |
| `ErrReadOnly` | `SQLITE_READONLY` | an existing database in a read-only folder; a read-only file |
| `ErrCanceled` | a cancelled or expired context | a cancelled read or write |
| `ErrConstraint` | `SQLITE_CONSTRAINT` | a deferred foreign key failing at `COMMIT` |
| `ErrCorrupt` | `SQLITE_NOTADB`, `SQLITE_CORRUPT`; an empty or missing file beside a WAL with commits | a file that is not a database; a damaged page; a copied WAL beside an empty file |
| `ErrReplaced` | the path no longer names the opened file | the file deleted or replaced while the store is open |
| `ErrUnsupportedVersion` | ours, at a newer `user_version` | a library written by a newer build |
| `ErrUnknownSchema` | another `application_id`, an unstamped file with objects, ours with another structure | another application's SQLite file; a marks file opened as a library |
| `ErrMigrationFailed` | a migration's error, joined with its kind | a failing statement; a full disk; another process's lock; a cancel |

## Scenarios and TDD

The scenarios and their RED/GREEN record are in
[report 008](../verification/008-sqlite-driver-durability-spike.md#stage-b--internallibrary) and
[report 014](../verification/014-sqlite-schema-migrations.md). Every check runs on real SQLite in a
temporary folder, including:
- another process holding the lock (also during a migration);
- a process killed with SIGKILL mid-commit and mid-migration;
- a real full HFS+ volume (`hdiutil`), also during a migration;
- test schemas (`schema_test.go`) for every row of the decision table, and the frozen
  `testdata/schemas/library-v1.sql` for the released schema (its text equals what migration 1
  builds; its rows open at the current version).

## Implementation and reuse

| File | Responsibility |
|---|---|
| `internal/library/store.go` | DSN (escaped path, per-connection PRAGMAs), `open`, `prepare`, `Tx`, `Write`, `Read`, `Close` |
| `internal/library/schema.go` | `schema`, `inspect`, `reference`, `examine`, `migrate` |
| `internal/library/library.go`, `migrations/library/` | `OpenLibrary`, `Library`, schema 1 |
| `internal/library/completion.go`, `migrations/completion/` | `OpenCompletion`, `CompletionStore`, the marks schema |
| `internal/library/errors.go` | the kinds, and the one `classify` |
| `internal/library/version.go` | `SQLiteVersion` |
| `internal/library/*_test.go` | in-process, child-process and disk-image tests; `TestMain` refuses to run while a test image is still attached |

**Seams** (the unexported `settings`):
- `busyTimeout`: the tests shorten the wait;
- `walPragma`: reaches the guard against another journal mode, which SQLite does not produce for
  a file;
- `afterCheck`: runs between the read-only check and the migration's write lock, where a test
  migrates the file further, as another store could.

## Checks and 100% coverage

- `go test -race -count=1 ./internal/library/`: `pass`, about 31 s (014).
- Coverage: 100.0% of statements, with no zero block, in the gate's merged profile.
- These checks touch real SQLite and the real file system. No test double replaces SQLite.

## Refactoring and debt check

After the first implementation, a cancelled read hung the package: database/sql's context-free
statement methods ignore the operation's context. `library.Tx` replaced `*sql.Tx` in the callback,
so cancellation cannot be forgotten. No ignore directive, no skipped test, no TODO.

014: the exported `Open` is gone (no path opens a file without its schema); 009's
`ensureCompletionSchema` became the generic `examine`; `ErrUnsupportedVersion` moved to `errors.go`.
The planned pinned migration connection became the separate opening connection, because restoring
foreign keys on a pinned one would have left error paths no test reaches.

## Usage and recovery

- **A test** opens a store in `t.TempDir()`, never in the user's folders.
- **A disk image left after an interrupted test run** fails the package's `TestMain` before any
  test runs, and the message names `hdiutil detach -force <mountpoint>`.
- **`ErrBusy` at `Close`, or `ErrFull`**, leaves the WAL in place, and the next `Open` uses it. Never
  delete the `-wal` file by hand: it holds committed changes.

## Change history and evidence

- 2026-10-17, task 008: the driver, `internal/library` and the product's SQLite version —
  [report 008](../verification/008-sqlite-driver-durability-spike.md), ADR
  [sqlite-driver](../decisions/sqlite-driver.md).
- 2026-11-04, task 014: schemas and migrations, library schema 1, the marks store under the policy,
  the exported `Open` removed — [report 014](../verification/014-sqlite-schema-migrations.md), ADR
  [library-schema-and-data-folder](../decisions/library-schema-and-data-folder.md).
