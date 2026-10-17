# Library store: how SQLite is opened, written and read

**Status:** `implemented` (prototype: no schema yet)  
**Requirements:** `STACK-04`, `SET-02`, `AI-07`  
**Related tasks:** [008](../../product/tasks/008-sqlite-driver-durability-spike.md); next [014](../../product/tasks/014-sqlite-schema-migrations.md), [015](../../product/tasks/015-transactional-repositories.md), [016](../../product/tasks/016-migration-backup-corruption-recovery.md), [028](../../product/tasks/028-application-startup-quit.md)  
**Last verified version:** branch `task/008-sqlite-driver`  
**Last updated:** 2026-10-17

## Goal and behavior

```
caller (014 migrations, 015 repositories)
  → library.Open(ctx, "/…/library.sqlite")   WAL; writer pool of 1 (BEGIN IMMEDIATE) + 4 read-only readers
  → store.Write(ctx, func(tx library.Tx) error)   nil only after a durable commit
  → store.Read(ctx, func(tx library.Tx) error)    one consistent snapshot, never waits for the writer
  → store.Close()                                  readers closed, WAL checkpointed and truncated, writer closed
```

- **Implemented in 008:**
  - the way every database is opened, written, read and closed;
  - its error kinds;
  - `library.SQLiteVersion`, which the product shows in the runtime check.
- **Not yet implemented:**
  - the schema and migrations (014);
  - repositories and the mapping to `AppError` (015);
  - backup (016);
  - the data directory and its override (014/028).
- The product binary links the package, but it opens only an in-memory database.

The decision and the alternatives are in the ADR [sqlite-driver](../decisions/sqlite-driver.md).

**A second database, from 009.** `library.OpenCompletion(ctx, path)` opens the verified-piece marks of
the engine, `<dataDir>/piece-completion.sqlite`, through `library.Open`: table `piece_completion(infohash,
piece, complete)`, `user_version` 1. `SaveMarks` writes a batch in one transaction, `LoadMarks` returns one
torrent's marks, and a database of another version or purpose is `ErrUnsupportedVersion`, never
`ErrCorrupt`. Its user is `filestore.Completion`; see [engine storage](engine-storage.md).

## Architecture analysis before the change

- **Layer.** `internal/library` is the repository layer of the
  [architecture](../../product/APPLICATION_ARCHITECTURE.md#6-sqlite-and-restore). It is the only
  package that imports the driver or sends SQL.
- **Owner.** The `Store` belongs to whoever opened it: the application start (028), or a test.
- **Side effects.** It writes only the database file and its `-wal` and `-shm` files. It never
  deletes them.
- **Cancellation.** Every statement runs under the context of its operation, through
  `library.Tx`.
- **After a crash.** The next `Open` replays the WAL.
- **Reuse.** No earlier code to reuse; the test patterns follow 007 (child processes, seams as
  package variables, bounded waits).

## Contracts and data

| API | Contract |
|---|---|
| `Open(ctx, path)` | `path` must be absolute and clean. A file is created (`0600`, `O_EXCL`) only if nothing exists there. An empty or missing main file beside a WAL with commits is refused. The journal is switched to WAL; another mode is an error. |
| `Write(ctx, fn)` | One write at a time, taking the lock at `BEGIN`; serialized, not FIFO (concurrent callers get the writer in no particular order, so an order between operations is the service's). Returns nil only when the commit succeeded. An error before `COMMIT`, or busy/full/constraint at `COMMIT`, keeps nothing; an I/O error from `COMMIT` has an unknown outcome, so the caller reads again. An error of `fn`'s own comes back unchanged; one that SQLite or the context caused comes back with its kind, wrapping the original. If the context was cancelled and the commit also failed, the kind is `ErrCanceled`. |
| `Read(ctx, fn)` | A read-only transaction on one of 4 readers (`mode=ro`): a consistent snapshot. `Write` and `Read` first check that the path still names the opened file. |
| `Tx.Exec`, `Query`, `QueryRow` | Run under the operation's context, so a cancel interrupts the running statement. |
| `Close()` | Runs once. Its checkpoint waits at most `busy_timeout` (5 s) for another process's reader. It returns `ErrBusy` if another process's reader blocks the checkpoint, or `ErrFull` on a full disk. In both cases nothing is lost: the WAL stays for the next open. |
| `SQLiteVersion(ctx)` | `sqlite_version()` of the bundled SQLite, from an in-memory database. |

Every connection has `synchronous=FULL`, `fullfsync=1`, `busy_timeout=5000` and `foreign_keys=1`.
Readers also have `query_only=1` and open with `mode=ro`.

**Errors.** A classified error matches its kind with `errors.Is`. `errors.As` reaches the driver's
`*sqlite.Error`.

| Kind | SQLite result | Example that proves it |
|---|---|---|
| `ErrBusy` | `SQLITE_BUSY` | another process holds `BEGIN IMMEDIATE` |
| `ErrFull` | `SQLITE_FULL` | a full disk image; `max_page_count` |
| `ErrCannotOpen` | `SQLITE_CANTOPEN` | a new database in a read-only folder |
| `ErrReadOnly` | `SQLITE_READONLY` | an existing database in a read-only folder; a read-only file |
| `ErrCanceled` | a cancelled or expired context | a cancelled read or write |
| `ErrConstraint` | `SQLITE_CONSTRAINT` | a deferred foreign key failing at `COMMIT` |
| `ErrCorrupt` | `SQLITE_NOTADB`, `SQLITE_CORRUPT`; an empty or missing file beside a WAL with commits | a file that is not a database; a damaged page; a copied WAL beside an empty file |
| `ErrReplaced` | the path no longer names the opened file | the file deleted or replaced while the store is open |

## Scenarios and TDD

The scenarios and their RED/GREEN record are in
[report 008](../verification/008-sqlite-driver-durability-spike.md#stage-b--internallibrary). Every
check runs on real SQLite in a temporary folder, including:
- another process holding the lock;
- a process killed with SIGKILL mid-commit;
- a real full HFS+ volume (`hdiutil`).

## Implementation and reuse

| File | Responsibility |
|---|---|
| `internal/library/store.go` | DSN (escaped path, per-connection PRAGMAs), `Open`, `Tx`, `Write`, `Read`, `Close` |
| `internal/library/errors.go` | the kinds, and the one `classify` |
| `internal/library/version.go` | `SQLiteVersion` |
| `internal/library/*_test.go` | in-process, child-process and disk-image tests; `TestMain` refuses to run while a test image is still attached |

**Seams:**
- `busyTimeout`: the tests shorten the wait;
- `walPragma`: reaches the guard against another journal mode, which SQLite does not produce for
  a file.

## Checks and 100% coverage

- `go test -race -count=1 ./internal/library/`: `pass`, about 8 s.
- Coverage: 100.0% of statements, with no zero block, in the gate's merged profile.
- These checks touch real SQLite and the real file system. No test double replaces SQLite.

## Refactoring and debt check

After the first implementation, a cancelled read hung the package: database/sql's context-free
statement methods ignore the operation's context. `library.Tx` replaced `*sql.Tx` in the callback,
so cancellation cannot be forgotten. No ignore directive, no skipped test, no TODO.

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
