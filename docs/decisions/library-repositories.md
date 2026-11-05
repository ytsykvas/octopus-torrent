# The library's repositories: one transaction per change, one mapping of errors

**Status:** `accepted`  
**Date:** 2026-11-04  
**Task:** [015](../../product/tasks/015-transactional-repositories.md)  
**Register item:** new  
**Requirements:** `STACK-04`, `SET-02`, `DATA-01`

## Question

[014](library-schema-and-data-folder.md) left a library with schema 1 that only opens and closes. The
import (023), file selection (024), pause (025), limits (026), restore (029), removal (031),
settings (050) and the global upload stop (053) need ways to store and read it. For each of them:

- a confirmation must mean the change is durable;
- a failure must reach the UI as an `AppError`;
- SQL must stay in `internal/library` (STACK-04).

[008](sqlite-driver.md) left four things to 015:
- an I/O error from `COMMIT` was unclassified;
- the kinds had no `AppError` codes;
- the runner was exported;
- the rule "no network or engine wait inside a transaction" was a convention.

## Decision

### Each repository method is one transaction over prepared values

- The repositories are methods of `library.Library`:

  | Writes | Reads |
  |---|---|
  | `AddTorrent`, `SetManualPaused`, `SetFilesWanted`, `SaveSettings`, `FinishOperation` | `Torrents`, `Settings`, `Operation` |

- Each method is one transaction over values the caller prepared in memory. None takes a callback,
  so no caller can wait for the engine, a file or the network while a transaction is open: 008's
  rule is structural.
- A write returns nil only after a durable commit. A later task adds its own methods the same way:
  029 payload records, 031 the removal, 056 tags.
- **One change, one transaction.** `AddTorrent` stores, in one transaction:
  - the record and both identities;
  - the files, each wanted or not, with a file's index being its position in `Files`;
  - the operation that imports it, in the journal: state `running`, the request as JSON, the
    fingerprint computed with `contracts.Fingerprint`, and the torrent it names.

  `FinishOperation` moves an operation to its final state, with its error and the outcome of each
  torrent. The move is guarded by `finished_at IS NULL`: states only move forward. It also replaces
  the stored request with `{}` (owner decision, 2026-11-05):
  - a finished operation needs no request, because its fingerprint still tells a repeat and its
    state and error answer one;
  - the request's paths (an import's destination, a default folder) do not outlive the operation;
  - a running operation keeps its request, for 029 to resolve after a crash.
- **Under the write lock.** `AddTorrent` looks up the infohashes and the operation ID inside its
  `BEGIN IMMEDIATE` transaction. Two concurrent imports of one torrent therefore commit exactly once,
  even from two stores on one file. The others get `ErrDuplicate` naming the record: the one with
  the v1 infohash, otherwise the one with the v2. A reused operation ID is `ErrOperationExists`, and
  `SameRequest` reports whether the fingerprint is equal.
- **The types are the contract's.** `internal/library` imports `internal/app/contracts` for
  identifiers, enumerations, `Settings`, `Request` and `AppError`, as `commandorder` and `transfer`
  do. `contracts` imports only the standard library, so there is no cycle, and no second vocabulary
  to keep equal to `vocabulary.json`.
- **Validated before SQLite sees a value** (`ErrInvalid`, and nothing is written). The checks:
  - UUIDs;
  - at least one infohash and one file;
  - a non-empty info dictionary, and each infohash set is its hash: SHA-1 for v1, SHA-256 for v2,
    over the same bytes for a hybrid. The engine would take info of another hash (finding 11), and
    029 restores from these bytes;
  - the import source, and the operation kind (the owner's Go-only kind check);
  - the save path, with `contracts.CheckFolderPath`;
  - file paths that are non-empty, unique and without a NUL (SQLite's `length()` stops at a NUL);
  - file paths that are relative to the save path, `/`-separated and clean (`filepath.IsLocal`,
    `path.Clean(p) == p`, not `.`), so a stored path never names a file outside the torrent's
    folder (031 removes by these paths). The torrent's own folder, if it has one, is the first
    element;
  - every time storable as Unix nanoseconds (years 1678–2262);
  - the settings, with `Settings.Validate`;
  - a journal error with a code.

  The repository makes the request JSON and the fingerprint itself, so they are always valid.

  These checks are the library's backstop. A torrent's untrusted input is refused earlier, with a
  code of its own: a repeated path, a NUL, an unsafe path, or too many files. That happens in
  `infocheck` (011), the parser (019), the path plan (020) and the import (023). So `ErrInvalid` and
  `ErrConstraint` reaching `Problem` are a caller's bug (`internal.unexpected`). Two paths that differ
  only in case name one file on a case-insensitive APFS volume; that collision is the path plan's
  (020), not the library's.
- **Encoding.**
  - JSON is bound as a string: a `[]byte` binds as a BLOB, which a STRICT `TEXT` column refuses.
  - An empty metainfo or magnet is bound as NULL.
  - `uint64` limits are bound as their int64 bit pattern.
  - Times are stored as Unix nanoseconds and read back in UTC.
  - The journal stores an `AppError`'s code, IDs and parameters (`{}` when none), never its cause,
    and a finished operation's request as `{}`.
- **Shape.**
  - One `execAll` prepares each distinct statement once per call, so a torrent of 32767 files is one
    statement run per file.
  - One `queryEach` serves the reads.
  - `SetFilesWanted` passes its indexes as one JSON array (`json_each`), so SQLite's limit of 32766
    bound parameters does not apply.
  - A read checks a stored infohash's length rather than panic on a file edited by hand.

### What a failed `COMMIT` means (`store.Write`)

| `Commit` returned | Kinds | What was stored |
|---|---|---|
| the context's error or `ErrTxDone`: database/sql did not run `COMMIT` | `ErrCanceled` | nothing |
| busy, full, constraint | its kind | nothing (SQLite rolled back) |
| anything else (I/O, …) | its kind + `ErrOutcomeUnknown` | unknown until the next open, which may recover the change from the WAL; 029 reconciles at start |
| nil, but the path no longer names the file (deleted, moved or replaced while the transaction ran) | `ErrOutcomeUnknown` + `ErrReplaced` | committed into a file that is not the library at its path: SQLite commits into an unlinked or moved inode without an error |

- This rule is verified in the pinned sources:
  - `Tx.Commit` checks the context and then a compare-and-swap that `awaitDone` races on, so exactly
    one of commit and rollback runs (Go 1.27.3 `database/sql/sql.go:2292-2325`);
  - modernc commits under `context.Background()` and rolls back a transaction that SQLite left open
    (`tx.go:35-54`).

  So once `COMMIT` ran, its own result decides, whatever happened to the context. 008 reported a
  cancel plus a failed `COMMIT` as `ErrCanceled`, which could call cancelled a commit that ran.
- `Write` checks before `BEGIN` and again after a successful `COMMIT` that the path still names the
  opened file, so a library lost meanwhile is never confirmed.
- A panic in a transaction's function rolls the transaction back before it goes on, so a caller that
  recovers it (the ledger, 028) is not left holding the writer until its context ends.
- New kind `ErrIO` (`SQLITE_IOERR`).
- The rule is `Write`'s only. `Read` and the migration keep their classification: a migration's
  failed `COMMIT` stays `ErrMigrationFailed`, because the next open examines the version again.

### SQL cannot leave `internal/library`

- The runner is unexported (`store`, `txn`), so code outside the package cannot run a statement.
- The gate's `product isolation` step also fails when either of these holds:
  - an own package other than `internal/library`, in any build mode (`""`, `dev`, `bindings` and
    the release tags `desktop,wv2runtime.download,production`), test files included, imports
    `database/sql…` or a path containing `sqlite`;
  - a package in `frontend/package-lock.json` has a name containing `sql`.
- The engine's own SQLite stores stay excluded by the `nosqlite` tag (018).

### One mapping of errors to `AppError`

`library.Problem(err)` is the only place a library error becomes a code. The error stays attached
as the `AppError`'s unexported cause (`WithCause`, `Unwrap`), for `errors.Is` and for 017's
redacting log. It is never encoded, and `ErrorFormatter` writes it nowhere, because it can carry
full local paths (DATA-03; owner decision, 2026-11-05). The first match wins:

| Kind | Code |
|---|---|
| `ErrOutcomeUnknown` | `library.outcomeUnknown` |
| `ErrMigrationFailed` | `library.migrationFailed` |
| `ErrUnsupportedVersion` | `library.newerVersion` |
| `ErrUnknownSchema` | `library.notALibrary` |
| `ErrCanceled` | `operation.cancelled` |
| `ErrBusy` | `library.busy` |
| `ErrFull` | `library.diskFull` |
| `ErrReadOnly` | `library.readOnly` |
| `ErrCorrupt` | `library.damaged` |
| `ErrCannotOpen`, `ErrReplaced`, `ErrIO` | `library.unavailable` |
| `ErrDuplicate` | `torrent.duplicate`, `TorrentID` = the existing record |
| `ErrOperationExists`, another request | `operation.idConflict` |
| `ErrNotFound` | `torrent.notFound`, `TorrentID` set |
| `ErrInvalid`, `ErrConstraint`, a repeat of the same request, a use after `Close`, anything else | `internal.unexpected` |

The table's other rules:
- A repeat of the same request is the service's to answer from the journal with the stored status
  (api-contract), before mapping.
- `Problem` is for the library: the marks store's errors are the engine adapter's (018).

### Two stages, two error states

The repository answers only for the commit. Applying the change to the engine is the service's next
step under the torrent's key (013): `CheckIntentChange`, then the repository, then the registry,
then `Reconcile`.
- A failed commit is the operation's `AppError` (through `Problem`) and never reaches the registry.
- A failed apply is the registry's recorded outcome (`View.Problem`).
- After a crash between the two, SQLite is the truth, and 029 rebuilds the registry from it.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| A unit of work: `lib.Write(ctx, func(tx) { tx.AddTorrent(…); tx.Record(…) })` | Services compose any atomic change | The callback can wait for the engine or the network with the transaction open | One method per change; a new combined change is a new method |
| Repository-owned record types | The repository depends on nothing | A second vocabulary of sources, kinds, states, themes and languages beside `contracts`, mapped both ways | The contract's types; the schema's `CHECK` lists already equal `vocabulary.json` |
| The mapping in a package under `internal/app` | `AppError` stays in the facade layer | Services under `internal/app/…` would import it, and the facade imports them: a cycle, or a third package | `library.Problem`, beside the kinds it maps |
| Report a cancel plus a failed `COMMIT` as `ErrCanceled` (008) | One rule for every transaction | Calls cancelled a commit that ran (`SQLITE_CONSTRAINT_COMMITHOOK` reproduced it) | `Write` classifies by whether `COMMIT` ran |
| A separate gate step for SQL | Its own line in the report | Two steps that list the same packages | Inside `product isolation` |
| A fake driver to reach error branches | Any error on demand | Proves the fake, not SQLite | Real failures: a commit hook, `RLIMIT_FSIZE` (EFBIG), a full HFS+ image, tables changed behind the store |
| A row per index in `IN (?, ?, …)` | Plain SQL | Fails past 32766 indexes | `json_each(?)` |

## Verified grounds

All on Apple M4, macOS 27.0.1, Go 1.27.3, modernc.org/sqlite v1.60.1 (SQLite 3.53.4); the commands
and results are in [the 015 report](../verification/015-transactional-repositories.md).

- **A commit hook that cancels the context and refuses the commit.** Before the change it was
  `ErrCanceled`; now it is `ErrConstraint` with nothing stored. With the commit allowed, the result
  is nil and the row is kept.
- **A file-size cap.** `RLIMIT_FSIZE = 0` set at the commit point, after `wal_checkpoint(TRUNCATE)`,
  gives `SQLITE_IOERR_WRITE` (778) at `COMMIT`. Go ignores the SIGXFSZ on darwin
  (`runtime/signal_darwin.go`, notify only). The result is `ErrOutcomeUnknown` + `ErrIO`; the next
  write and a reopen are fine (`integrity_check` ok), and nothing was stored. A migration's `COMMIT`
  capped the same way is `ErrMigrationFailed` + `ErrIO` only, and the next open migrates.
- **A full 8 MiB HFS+ image at `COMMIT`.** The result is `ErrFull` (13), a known outcome: nothing
  stored, for every write method. A full disk cannot leave a commit half written. The pinned build
  has `SQLITE_POWERSAFE_OVERWRITE = 1` (modernc `lib/sqlite.go:4178`), so a WAL commit writes no
  padding frames after its commit frame. A commit frame that could not be written whole fails its
  checksum, and recovery stops before it.
- **Concurrency.** Writes run concurrently while readers read:
  - 8 imports of one hash;
  - 8 imports of distinct hashes;
  - 8 pause togglers;
  - 8 file-selection togglers on disjoint files;
  - one settings writer.

  Exactly one import of the shared hash commits, no reader sees a record without its files, and
  every change that returned nil is present after a reopen. Two `Library` values on one file also
  commit one hash once.
- **Lost files and changed tables.**
  - A library deleted, or its folder moved, between changes: every change is refused as
    `library.unavailable`, and nothing is created.
  - The same loss at the commit point of each write method: the commit returned nil into the old
    file, and the result is `library.outcomeUnknown` (before the review's fix it was nil).
  - Nineteen changes of the tables behind the store's back each fail the method as
    `internal.unexpected`, with every table's rows unchanged, never with success or a panic. They
    include a file row of no torrent (before the fix: attached to another torrent) and a gap in a
    torrent's file indexes.
- **A panic in a transaction's function.** It rolls the transaction back. The next write runs at
  once; before the fix it waited for the panicking call's context.
- **SQL isolation.** In each of the four build modes, only `internal/library` imports
  `database/sql` or modernc; the frontend lockfile has no SQL package.

## Consequences

- **021** builds deduplication on `ErrDuplicate`'s existing ID and refines `torrent.duplicate` if it
  needs more.
- **023**:
  - calls `AddTorrent` once, after the destination and files are verified, outside any engine call,
    with each file path relative to the save path and both infohashes of the stored info bytes;
  - refuses a torrent's repeated, unsafe or too many paths with a code of its own before it, so
    `ErrInvalid` stays a bug;
  - answers a repeated confirmation from the journal (`Operation`, `SameRequest`) before mapping;
  - calls `FinishOperation` after the engine add;
  - adds no payload row: no row already means "verify everything".
- **024** checks the indexes against the torrent before `SetFilesWanted`, and adds file priority with
  a migration if it decides on one.
- **025** calls `SetManualPaused` between `CheckIntentChange` and the registry change, and maps
  `ErrNotFound`.
- **026**, **050**, **053** save the whole settings with `SaveSettings`, under the `Settings` key, so
  two saves never interleave.
- **028** maps a start failure with `Problem`; whether the window then shows it is 028's design.
- **029** reads `Torrents`, `Settings` and the unfinished operations, and treats
  `library.outcomeUnknown` as resolved by its start. A stored path is lexically safe, but the folder
  may have changed on disk since: symlinks are resolved and `os.Root` confines before any write, as
  for every path (020).
- **031** adds its journal write and the removal as methods of their own; the journal outlives the
  record.
- **017** logs the cause of an `AppError` (`Unwrap`) at one place, with redaction and a length cap
  (DATA-03). Until then no coded error's cause is written anywhere. A cause can carry full local
  paths, as can an `invalid` message, and torrent text up to the metadata limit. The journal keeps a
  request only while its operation runs; a diagnostics export still excludes `operations.request`,
  and a backup (016) stays in the data folder.
- Each new error code goes into `contracts/errors.go`, `vocabulary.json`, the TS catalogue and
  [api-contract](api-contract.md#errors).
