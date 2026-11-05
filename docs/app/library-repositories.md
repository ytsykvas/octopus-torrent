# Library repositories: storing torrents, file selection, pause, settings and the journal

**Status:** `implemented` (not called by the product before [023](../../product/tasks/023-durable-import-commit.md), [024](../../product/tasks/024-file-folder-wanted-commands.md), [025](../../product/tasks/025-pause-resume-application.md), [050](../../product/tasks/050-settings-ui-save-feedback.md))  
**Requirements:** `STACK-04`, `SET-02`, `DATA-01`  
**Related tasks:** [015](../../product/tasks/015-transactional-repositories.md); next [021](../../product/tasks/021-full-hash-deduplication.md), [023](../../product/tasks/023-durable-import-commit.md), [024](../../product/tasks/024-file-folder-wanted-commands.md), [025](../../product/tasks/025-pause-resume-application.md), [026](../../product/tasks/026-rate-limits-seed-network.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md), [031](../../product/tasks/031-delete-record-lifecycle.md), [050](../../product/tasks/050-settings-ui-save-feedback.md), [053](../../product/tasks/053-global-upload-policy-production.md)  
**See also:** [ADR library-repositories](../decisions/library-repositories.md), [library store](library-store.md), [transfer policy](transfer-policy.md)  
**Last verified version:** branch `task/015-transactional-repositories`  
**Last updated:** 2026-11-04

## Goal and behavior

```
service step, under the torrent's key (013)                          [023, 024, 025, 050, 053]
  ├─ values prepared in memory: Torrent, OperationIntent, Settings, indexes…   no engine call inside
  ├─ lib.AddTorrent / SetFilesWanted / SetManualPaused / SaveSettings / FinishOperation
  │    └─ one store.Write: BEGIN IMMEDIATE → lookup → statements → COMMIT
  │         nil      → durable: a reopen reads it back
  │         error    → library.Problem(err) → the operation's AppError (the cause goes to diagnostics)
  └─ only after nil: registry change → Reconcile → engine (its failure is the registry's outcome)
read at start or for a view                                                     [029, 027]
  └─ lib.Torrents / Settings / Operation   one snapshot on the read pool
```

- **What is implemented.** The library stores and reads every change the scenarios need before
  payload records:
  - a confirmed import, with its record, identities, file selection and the operation that imports
    it, as one change;
  - the manual pause of a torrent;
  - which of its files are wanted;
  - all settings, the global upload stop and the limits included;
  - how an operation ended, with each torrent's outcome.
- **The promise.** A method returns nil only when its transaction committed into the file at the
  library's path. A change that returned nil is read back after a reopen. A change that returned an
  error stored nothing, except one case: `ErrOutcomeUnknown`, where the next open decides. That
  covers a `COMMIT` that failed after it may have written, and a library deleted or moved while the
  transaction ran.
- **Errors.** Every failure becomes one `AppError` code through `library.Problem`.
- **Not implemented.** No facade method calls these yet. Also left to later tasks: payload records
  (029), removing a record (031), tags (056).

## Architecture analysis before the change

- **Layer.** The repositories are methods of `library.Library`, beside the store they run on.
  `internal/library` stays the only package with SQL: the runner `store`/`txn` is unexported, and
  the gate's `product isolation` step fails on any other own package (or frontend package) that
  reaches SQLite ([quality gate](../tooling/quality-gate.md)).
- **Types.** The contract's: `contracts.TorrentID`, `ImportSource`, `OperationKind`,
  `OperationState`, `Settings`, `Request`, `AppError`. `internal/library` imports
  `internal/app/contracts`, which imports only the standard library.
- **State owner.** SQLite. The registry (013) mirrors it in memory and is rebuilt from it at start
  (029).
- **Side effects.** Only the library file and its WAL.
- **Cancellation.** Every statement runs under the caller's context. A cancel before `COMMIT`
  stores nothing; once `COMMIT` ran, its own result decides (ADR, "What a failed COMMIT means").
- **After a crash.** SQLite's WAL replay. An operation left `running` is 029's to resolve.
- **Reuse.**
  - `store.Write`/`Read` (008, 014);
  - `contracts.IsUUID`, `CheckFolderPath`, `Settings.Validate` and `Fingerprint` (005);
  - the schema's constraints (014).

## Contracts and data

| Method | One transaction that | Errors (kinds) |
|---|---|---|
| `AddTorrent(ctx, Torrent, OperationIntent)` | looks up both infohashes and the operation ID under the write lock, then inserts the record, its files (index = position in `Files`), the operation (`running`, request JSON, fingerprint) and the torrent it names | `ErrDuplicate` (`*DuplicateError{Existing}`: the record with the v1 hash, else the v2); `ErrOperationExists` (`*OperationExistsError{ID, SameRequest}`); `ErrInvalid` |
| `SetManualPaused(ctx, id, paused)` | updates the record | `ErrNotFound` (`*NotFoundError{Torrent}`); `ErrInvalid` |
| `SetFilesWanted(ctx, id, indexes, wanted)` | updates every listed file (a repeated index counts once; any count, through `json_each`) | `ErrNotFound`; `ErrInvalid` for an empty list, a negative index or one the torrent lacks (no file changes) |
| `SaveSettings(ctx, contracts.Settings)` | inserts or replaces the one settings row | `ErrInvalid` (`Settings.Validate`) |
| `FinishOperation(ctx, id, OperationEnd)` | moves a running operation to `succeeded`/`failed`/`cancelled` with its error and outcomes, and replaces its request with `{}`: no path of it outlives it, and the fingerprint still tells a repeat | `ErrInvalid`: an unknown or finished operation, a state that is not final, an error that does not match the state, an outcome for a torrent it does not name, an error without a code |
| `Torrents(ctx)` | every record with its files, in the order added; the full read for the start (info and source included) | read errors; a file row of no record or a gap in a torrent's file indexes is an error, never a file moved to another torrent |
| `Settings(ctx)` | `(settings, found, err)`; `found` is false before the first save, and the caller uses its defaults (SET-06) | read errors |
| `Operation(ctx, id)` | `(op, found, err)`, with its torrents and their outcomes; `Request` is the request's JSON while it runs, `{}` once finished | read errors; `ErrInvalid` for an ID that is not a UUID |

- **Validation before SQLite.** What each record must satisfy is in the ADR. Two rules matter most
  to callers:
  - each infohash set is the hash of `Info`;
  - each `TorrentFile.Path` is relative to `SavePath`, `/`-separated and clean, with the torrent's
    own folder (if any) as its first element.

  Anything else is `ErrInvalid`, and nothing is written. The import refuses a torrent's bad paths
  with a code of its own before it.
- **Stored values.**
  - times as Unix nanoseconds, read back in UTC (years 1678–2262 only);
  - limits as the bit pattern of their `uint64`;
  - an absent `.torrent` or magnet as NULL;
  - a journal `AppError` as `{code, torrentId, operationId, params}`, without its cause.
- **Every write also fails with** the store's kinds: `ErrCanceled`, `ErrBusy`, `ErrFull`,
  `ErrReadOnly`, `ErrCannotOpen`, `ErrReplaced`, `ErrCorrupt`, `ErrIO`, and `ErrOutcomeUnknown`
  (with its cause's kind) when `COMMIT` failed after it may have written ([library store](library-store.md)).

**`library.Problem(err)`** is the one mapping to codes. The precedence table is in the
[ADR](../decisions/library-repositories.md#one-mapping-of-errors-to-apperror). It also has these
properties:
- the error stays the `AppError`'s cause: `errors.Is(problem, library.ErrFull)` holds. Nothing
  writes it before 017's redacting log, because it can carry full local paths (DATA-03);
- `json.Marshal` never includes the cause;
- `torrent.duplicate` names the existing record, and `torrent.notFound` the missing one.

## Scenarios and TDD

The RED/GREEN record of every step is in [report 015](../verification/015-transactional-repositories.md#implementation).
The tests run on real SQLite in `t.TempDir()`, with no fake driver.

| Scenario | Test |
|---|---|
| a record, its files, Unicode, binary info, maximum sizes and a non-UTC time read back equal after a reopen | `TestAnAddedTorrentIsReadBackExactlyAfterAReopen` |
| an absent source is NULL in SQL (`typeof`) | `TestAnAbsentSourceIsStoredAsNull` |
| a duplicate by v1, by v2, or a hybrid matching two records names the right record | `TestADuplicateNamesTheExistingRecordAndAddsNothing` |
| 8 concurrent adds of one hash, and two `Library` values on one file: exactly one commits | `TestConcurrentAddsOfOneHashCommitExactlyOnce`, `TestTwoLibrariesOnOneFileAddOneHashOnce` |
| a reused operation ID, with the same or another request | `TestAReusedOperationIDIsRefusedAndToldApart` |
| a torrent of 32767 files is one change; selecting all of them is one statement | `TestAFolderOfManyFilesIsOneChange` |
| a `COMMIT` that ran while the context ended: refused is `ErrConstraint`, allowed is nil | `TestACommitThatRanIsReportedByItsOwnResultWhenTheContextEnds` |
| a `COMMIT` that cannot write (`RLIMIT_FSIZE`, EFBIG) is `ErrOutcomeUnknown` + `ErrIO`; a full HFS+ image is `ErrFull` | `TestACommitThatCannotWriteHasAnUnknownOutcome`, `TestAFullDiskAtCommitKeepsNothingAndIsAKnownOutcome` |
| each write method failed at its commit (cancel, file-size cap, full disk) stores nothing and maps to its code | `TestAWriteThatFailsAtItsCommitStoresNothingAndSaysWhy`, `TestAWriteOnAFullDiskStoresNothingAndSaysSo` |
| the library deleted or its folder moved: every change is `library.unavailable` and nothing is created | `TestALostLibraryRefusesEveryChangeAndCreatesNothing` |
| the same loss at the commit point of each write method is `library.outcomeUnknown`, never nil | `TestALibraryLostWhileAChangeCommitsIsNeverConfirmed` |
| a panic in a transaction's function rolls back and frees the writer | `TestAPanickingFunctionRollsBackAndFreesTheWriter` |
| a mismatched infohash, and absolute, escaping or unclean file paths, are refused | `TestAnInvalidTorrentOrIntentIsRefusedAndWritesNothing` |
| concurrent imports, pauses, file selections and settings saves while readers read: no duplicate, nothing lost, no record without its files | `TestConcurrentChangesNeitherDuplicateNorLoseAnything` |
| tables changed behind the library's back (19 shapes, an orphan file row and an index gap among them) fail the method as `internal.unexpected` and change no row | `TestATableChangedBehindTheLibrarysBackIsReportedNotHidden` |
| every kind gets its code; the cause stays attached | `TestEveryKindHasItsCodeAndKeepsItsCause` |
| only `internal/library` imports SQL, in every build mode, tests included | `TestOnlyTheLibraryMayImportSQL` (devtool) and the gate's `product isolation` step |

## Implementation and reuse

| File | Responsibility |
|---|---|
| `internal/library/torrents.go` | `Torrent`, `TorrentFile`, `AddTorrent`, `Torrents`, `SetManualPaused`, `SetFilesWanted` |
| `internal/library/operations.go` | `OperationIntent`, `Operation`, `OperationTorrent`, `OperationEnd`, `FinishOperation`, `Operation` |
| `internal/library/library_settings.go` | `Settings`, `SaveSettings` |
| `internal/library/problem.go` | `Problem` |
| `internal/library/repository.go` | `execAll` (each distinct statement prepared once per call), `queryEach`, the encoders |
| `internal/library/errors.go` | the kinds and the three typed errors |

**Invariants.**
- No method takes a callback.
- `OperationIntent.ID` is the request's own `operationID`: the journal is keyed by it.
- Each runs at most one lookup query before its statements.
- `RowsAffected` decides "not found", because modernc's never fails.

## Checks and 100% coverage

- `go test -race -count=1 ./internal/library`: `pass`, 100.0% of statements with
  `-covermode=atomic`, about 51–61 s.
- The gate proves the merged profile; see [report 015](../verification/015-transactional-repositories.md).
- No repository method runs in the `.app` yet: no facade calls one.

## Refactoring and debt check

- `Store`/`Tx` became `store`/`txn`.
- `Write` classifies a failed commit by whether `COMMIT` ran.
- One error mapping replaces none.
- No ignore directive, skipped test or TODO.

## Usage and recovery

- **A new change** is a new method: one transaction over prepared values, validated in Go before
  any `CHECK` sees it.
- **Recipe for 023.**
  1. Verify the destination and files.
  2. Call `AddTorrent` outside any engine call.
  3. On `ErrOperationExists` with `SameRequest`, answer from `Operation`.
  4. On any other error, return `Problem(err)`.
  5. After the engine add, call `FinishOperation`.
- **Recipe for 024 and 025.** Under the torrent's key:
  1. `CheckIntentChange`;
  2. `SetFilesWanted` or `SetManualPaused`;
  3. the registry change;
  4. `Reconcile`.

  A repository error ends the step before the registry changes.
- **Recipe for 050 and 053.** Under the `Settings` key, `SaveSettings` with the whole settings.
- **`library.outcomeUnknown`.** Do not retry blindly. The state is known again after the next open;
  029 reconciles at start.

## Change history and evidence

- 2026-11-04, task 015: the repositories, the commit outcome rule, `Problem`, the SQL isolation step —
  [report 015](../verification/015-transactional-repositories.md), ADR
  [library-repositories](../decisions/library-repositories.md).
