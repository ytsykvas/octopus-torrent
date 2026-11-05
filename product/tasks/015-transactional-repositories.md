# 015 — Isolate SQL and ensure transactional writes

**Status:** `done`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [014](014-sqlite-schema-migrations.md), [013](013-transfer-policy-state-machine.md)  
**Requirements:** `STACK-04`, `SET-02`, `DATA-01`

## Outcome

Isolate SQL in data repositories and return a persistence confirmation only after a transaction has completed successfully.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `STACK-04`, `SET-02`, `DATA-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If required evidence is missing, the dependent part of the work must not start.

## Work order

1. Implement parameterized queries with `context`; close read results and finish each transaction with a commit or a rollback.
2. Atomically write the torrent record, its identities, the file selection and the operation intent where they form a single change.
3. Separate the DB transaction commit from applying the policy in the engine, and give each stage an explicit error state.
4. Apply the write order from task 008; do not wait for network operations inside an open transaction.
5. Test the repositories on real SQLite, and reproduce individual failures with controlled error injection.

## Artifacts and change locations

**Proposed locations:** `internal/library; internal/app persistence contracts`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/015-transactional-repositories.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] A persistence confirmation matches the result that is actually written and available after reopening the DB.
- [x] Request cancellation and a failed transaction commit return `AppError`.
- [x] React and the engine adapter do not execute SQL directly.
- [x] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Concurrently reserve a torrent identity and change the file selection, the global policy and pause; transactions must not create duplicates or lose persisted changes.
- Reproduce a lost DB connection, a transaction commit error and request cancellation; success must not be returned for unpersisted changes.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

A successful write of a policy to SQLite does not prove it was applied in the engine. These results must be tracked separately and reconciled after a failure. Repository dependencies are passed explicitly; engine objects are not stored as DB data.

**Basis for technical decisions:** [database/sql](https://pkg.go.dev/database/sql). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a performed integration check.

**From 008** ([sqlite-driver](../../docs/decisions/sqlite-driver.md)): repositories use `library.Store`: `Write` is the single serialized writer (not FIFO: concurrent writes commit in any order, so per-torrent order comes from the service's queue) and returns nil only after a durable commit; an I/O error from `COMMIT` (`SQLITE_IOERR`, unclassified in 008) has an unknown outcome, so map it and re-read the state; `Read` is a snapshot on the read pool. Statements go through `library.Tx`, which binds them to the operation's context. Map the error kinds (`ErrBusy`, `ErrFull`, `ErrCannotOpen`, `ErrReadOnly`, `ErrCanceled`, `ErrConstraint`, `ErrCorrupt`, `ErrReplaced`) to `AppError` codes in one place; the cause stays in diagnostics. `Store` and `Tx` are exported today, so SQL-only-in-the-repository (STACK-04) is a convention: enforce it (an unexported surface, or an import test that only repositories import `internal/library`).

**From 013** ([transfer-status-model](../../docs/decisions/transfer-status-model.md), [feature](../../docs/app/transfer-policy.md)): `transfer.Registry` is the in-memory mirror of the durable intent: persist first (the repository commits), then change the registry, then `Reconcile`. A failed write never touches the registry, so there is no rollback path. The registry is per runtime; after a restart SQLite is the truth (029 rebuilds it).

**From 014** ([library-schema-and-data-folder](../../docs/decisions/library-schema-and-data-folder.md)): schema 1 is `internal/library/migrations/library/0001_initial.sql` (the ADR lists every column and `CHECK`). `OpenLibrary` returns a `Library` that exposes only `Close`: add the repositories on it, its store stays unexported. Validate UUIDs (lower case), hashes (20/32 bytes) and JSON in Go before the `CHECK`s see them, and the operation kind against the vocabulary (SQLite checks only that it is non-empty: owner decision, so a new kind needs no migration), so a constraint error is a bug, not a user path; unsigned 64-bit values (limits, inode, device) are stored as their int64 bit pattern, times as Unix nanoseconds UTC. Map `ErrUnknownSchema`, `ErrUnsupportedVersion` and `ErrMigrationFailed` to `AppError` too. `main.go`'s `system.openLibrary` returns an `io.Closer` today: make it return `*library.Library` when the repositories are passed to `app.New`. A new column or a value of a closed enumeration is a new migration file (a table rebuild for a `CHECK` list; the runner keeps child rows) plus a frozen `testdata/schemas/library-vN.sql` that `TestTheFrozenSchemaIsWhatItsMigrationsBuild` compares; a released migration is never edited.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

## Execution result

**Date:** 2026-11-04. **Status:** `done`.

- **Changes:**
  - **Repositories.** They are methods of `library.Library`, and each is one transaction over values
    prepared in memory, with no callback.
    - Writes: `AddTorrent` stores the record, both identities, the file selection and the import's
      operation (`running`, request JSON, fingerprint) as one change. Its duplicate and
      operation-ID lookup runs under `BEGIN IMMEDIATE`. The others are `SetManualPaused`,
      `SetFilesWanted` (`json_each`, any count), `SaveSettings`, and `FinishOperation`. States only
      move forward, and a finished operation's request becomes `{}`, so no path of it outlives it.
    - Reads: `Torrents`, `Settings`, `Operation`.
    - Values are validated in Go before a `CHECK` sees them: UUIDs, sources, kinds, the save path,
      each infohash being the hash of `Info`, file paths relative to the save path and clean, and
      storable times.
  - **The commit rule** (`store.Write` only). A `COMMIT` that database/sql did not run is
    `ErrCanceled`. Busy, full or constraint at `COMMIT` is its kind, with nothing stored. Anything
    else is that kind + `ErrOutcomeUnknown`, and so is a commit into a file deleted, moved or replaced
    meanwhile. The new kind is `ErrIO`. A panic inside a transaction rolls it back. `Read` and the
    migration keep their classification.
  - **Errors.** `library.Problem` is the one mapping to `AppError`: 11 new codes in
    `contracts/errors.go`, `vocabulary.json` and the TS catalogue. `AppError` has an unexported cause
    (`WithCause`, `Unwrap`). It is never encoded and never written to stderr (DATA-03; 017's
    redacting log may).
  - **SQL isolation.** The runner is unexported (`store`, `txn`). The gate's `product isolation`
    step fails when another own package, in any of four build modes and with test imports included,
    or a frontend package, reaches SQL.
  - **Documents.**
    - new: ADR [library-repositories](../../docs/decisions/library-repositories.md), feature doc
      [library repositories](../../docs/app/library-repositories.md), report
      [015](../../docs/verification/015-transactional-repositories.md);
    - updated: ADRs api-contract, sqlite-driver and library-schema-and-data-folder; the library store,
      facade, quality gate and testing docs; the architecture; the README;
    - "From 015" notes in 016, 017, 021, 023, 024, 025, 026, 028, 029, 031, 050 and 053.
- **Checks:**
  - Real SQLite throughout, with no fake driver:
    - a commit hook that refused a `COMMIT` that ran (before: reported as cancelled);
    - `RLIMIT_FSIZE` at the commit point: EFBIG, `SQLITE_IOERR_WRITE`, an unknown outcome;
    - a full HFS+ image at `COMMIT`: a known outcome;
    - every write method failed at its commit (cancel, cap, full) and unchanged after a reopen;
    - the library deleted or moved between writes and at the commit point;
    - concurrent imports of one hash (exactly one commits, also from two `Library` values), pauses,
      file selections and settings while readers read, nothing lost after a reopen;
    - 19 tables changed behind the library's back.
  - 9 mutation checks. 8 were caught at once. One (the listing without test imports) survived and
    gained a test that runs the real `go list`.
  - `/qa` regression run on the real `.app`: 6 scenarios `pass`. The isolated start and quit, a
    restart unchanged, seeded rows kept, a newer library refused unchanged, and the owner's folder
    never created.
  - `go tool devtool check`: run 1 failed on the known swarm stall (finding 22, unchanged package,
    recorded in the report); run 2 `GATE PASSED`; run 3, after the review's fixes, `GATE PASSED`;
    run 4, after the owner's decision of 2026-11-05 and on the final tree, `GATE PASSED`. Every own Go file and the native source at 100%.
  - `not_run`: a volume detached under an open library (it could abort the test binary); power loss;
    a repository called from the `.app` (no caller before 023); engine integration (018); test-bridge
    UI tests (035).
- **Review:** three agents. Fixed test-first:
  - a library lost during a write was confirmed;
  - an orphan file row or an index gap was misread by `Torrents`;
  - unsafe file paths and infohashes of other info were stored;
  - a panic held the writer;
  - the SQL rule missed release-only files;
  - the behind-the-store tests asserted only "an error".

  The other findings are fixed in the docs or recorded for 016/017/020/023/029/031/050 in the report.
  By the owner's decision (2026-11-05), two of them were then fixed here too: a finished operation's
  request is `{}`, and no coded error's cause reaches stderr.
- **Limitations:**
  - No facade method calls the repositories yet (023, 024, 025, 050), so criterion 2 is proven at
    the repository and mapping level.
  - No coded error's cause is logged before 017's redacting log.
  - `CLAUDE.md` still names `library.Tx`/`Store`, until `/update-rules` with the owner.
  - The branch is stacked on PR #20 (on #19, on #18), which merge first.
- **Next:** [016](016-migration-backup-corruption-recovery.md) (unblocked by this task), and
  [017](017-local-diagnostics-privacy.md), [018](018-torrent-engine-adapter.md) and
  [035](035-react-bridge-state-resync.md), as before.
