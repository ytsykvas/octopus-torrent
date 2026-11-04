# 014 — Create the SQLite schema and safe migrations

**Status:** `done`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [005](005-domain-dto-and-bridge-contract.md), [008](008-sqlite-driver-durability-spike.md)  
**Requirements:** `STACK-04`, `SET-02`

## Outcome

Create the base schema of the local library and migrations that do not destroy existing data on error.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `STACK-04`, `SET-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Add tables for torrent records, full hashes and their mappings, file selection, settings and operation execution intents.
2. Store the initial metadata, UUID, file indexes, import source, paths and `manualPaused` without loss of values.
3. Version the migrations and refuse to open a newer unsupported schema without resetting or overwriting it.
4. Apply foreign keys, the connection pool and the data durability decision from task 008; place the DB separately from torrent files.
5. Prepare test DBs of previous schemas and verify rollback of an unfinished migration on error.

6. Store unfinished and completed non-trivial operations independently of torrent record deletion: a cascading delete must not destroy the log needed for recovery and for a repeated `operationID`. Verify this with a real SQLite transaction.

## Artifacts and change locations

**Proposed locations:** `internal/library/migrations; schema fixtures`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/014-sqlite-schema-migrations.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] An empty DB and all supported previous schemas migrate to the current version.
- [x] A migration error does not cause loss of existing records or files.
- [x] Identity uniqueness constraints and foreign keys are actually enforced by SQLite.
- [x] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Interrupt a migration, open a DB of a newer version and try to write a duplicate full hash; each case must produce a defined result without destroying previous data.
- Reproduce an access denial, a full disk and concurrent transaction commits; errors must be returned to the caller, and schema constraints must be preserved.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The schema contains library data, not internal engine objects. Migrations run through the defined SQLite access layer. An unsupported version or a migration error requires an explicit message; automatically creating an empty DB in place of the existing one is unacceptable.

**Parallel workspaces (added 2026-10-15, [agent-tooling ADR](../../docs/decisions/agent-tooling.md)):** several octopus/Conductor worktrees run `wails dev` at once. The library location must be configurable (for example an environment variable that `go tool devtool workspace run` sets per worktree) so a workspace, a test or a spike never opens the owner's real library; record the contract in the architecture.

**Basis for technical decisions:** [SQLite WAL](https://www.sqlite.org/wal.html), [PRAGMA checks](https://www.sqlite.org/pragma.html#pragma_integrity_check). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 008** ([sqlite-driver](../../docs/decisions/sqlite-driver.md)): open the database only with `library.Open` (WAL, `synchronous=FULL` + `fullfsync`, `busy_timeout` 5 s, foreign keys, one `BEGIN IMMEDIATE` writer, four `query_only` readers) and write through `Write(ctx, func(library.Tx) error)`; batch rows into one transaction, since each commit costs about 4 ms with `fullfsync`. The data directory contract is fixed: `~/Library/Application Support/dev.tsykvas.octopustorrent/library.sqlite`, override `OCTOPUS_DATA_DIR` (an absolute folder, validated in Go). If this is the first task that reads it, implement the override, make `go tool devtool workspace run` set it per workspace, and make the gate's `.app` run pass a temporary folder (`open --env OCTOPUS_DATA_DIR=…`) and fail if the default folder changes ([quality-gate](../../docs/decisions/quality-gate.md#consequences)). `Open` never resets a database; a migration failure must keep that property. Before switching or migrating a file, set and verify `PRAGMA application_id`, so a valid SQLite file of another application is refused rather than converted. Validate `OCTOPUS_DATA_DIR` as the ADR specifies (absolute, clean, symlinks resolved, owned by the user, not group- or world-writable, not inside a save folder; created `0700`), and turn "the gate and workspaces never open the owner's library" into a failing test as soon as the default path function exists. The application-level part of 008's read-only-folder and full-disk checks (the reason shown to the user) is `not_run` in 008, which opens no file database in the app.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): the verified-piece marks are a second database file, `<dataDir>/piece-completion.sqlite`, opened by `library.OpenCompletion` (table `piece_completion`, `user_version` 1). Its schema precedes your migration policy: apply the same policy to it, and never treat a newer `user_version` as damage (`ErrUnsupportedVersion`). Your schema must also hold, per torrent, the `files.Record` that a clean quit leaves (files, fingerprints, piece ranges, file system type, device, clean flag) and the "not clean" marker that 029 writes before the first payload write.

**From the 009 checkpoint** ([refactor after 009](../../docs/verification/refactor-after-009.md)): when the migration policy generalizes the version check, move `ErrUnsupportedVersion` from `internal/library/completion.go` into `errors.go`, and restructure `ensureCompletionSchema` as: the query error first, then a switch over an empty store (create), the current version (nil) and anything else (`ErrUnsupportedVersion`). The ended-transaction, full-disk and three unsupported-version tests are the branch evidence.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

## Execution result

**Date:** 2026-11-04. **Status:** `done`.

- **Changes:**
  - **Schemas.** `internal/library` opens every database only through its schema: an
    `application_id` ("OTLB" library, "OTPC" marks), embedded numbered migrations and a structural
    check from pragmas against a replay of the migrations. `prepare` runs on its own connection with
    foreign keys off:
    1. a read-only examination before anything is written (another application's file →
       `ErrUnknownSchema`, ours newer → `ErrUnsupportedVersion`, both unchanged);
    2. the WAL switch;
    3. one `BEGIN IMMEDIATE` transaction that examines again, migrates and runs `foreign_key_check`.
       A failure is `ErrMigrationFailed` with the cause's kind, and the old version and rows are
       kept.
  - **Library schema 1** (`migrations/library/0001_initial.sql`, seven STRICT tables): closed lists
    are `CHECK`s; the operation kind is checked in Go, so a later kind needs no migration (owner
    decision). A frozen `testdata/schemas/library-v1.sql`: its text must equal what migration 1
    builds, and its rows must open at the current version. The marks store is under the same
    policy. The exported raw `Open` is gone. A path that is not a regular file is refused.
  - **Data folder.** New `internal/datadir`: the default folder or a validated `OCTOPUS_DATA_DIR`,
    created `0700`, symlinks resolved, the user's own, with no permission bit for group or others.
  - **Start and quit.** `main.run`: SQLite version → data folder → `OpenLibrary` → window → `Close`
    last. The build-mode files keep the bindings generator out of every folder and make `wails dev`
    require the variable.
  - **Tooling.** devtool: `workspace run` sets `build/workspace-data`; the gate gives Wails commands
    `build/tool-data`, which must stay empty, and each `.app` run its own folder, and its last step
    fails if the owner's folder changed. The `macos-qa` agent always passes its own folder.
  - **Documents.** ADR
    [library-schema-and-data-folder](../../docs/decisions/library-schema-and-data-folder.md) (closes
    014's part of O4); feature docs [data folder](../../docs/app/data-folder.md) and
    [library store](../../docs/app/library-store.md); report
    [014](../../docs/verification/014-sqlite-schema-migrations.md); "From 014" notes in 015, 016, 017,
    020, 023, 024, 028, 029, 031, 050 and 053.
- **Checks:**
  - `go tool devtool check` → `GATE PASSED` three times: before the review, after its fixes, and
    after the owner's decisions. The handoff edits after run 3 are docs only, checked with
    `check-docs` and `check-plan`. Every own Go file and
    the native source are at 100%. `not_run`: engine integration (018) and test-bridge UI tests
    (035).
  - Real SQLite throughout: test schemas for every row of the decision table, a full HFS+ image
    during a migration, another process's lock, a cancel and a SIGKILL mid-migration, two stores
    migrating one file, a concurrent duplicate infohash (exactly one commit), and about 50
    constraint cases. 8 mutation checks, each caught.
  - `/qa` on the real `.app`: 14 runs `pass`, the last four on the final build (isolated start and clean quit, restart unchanged,
    newer and foreign files refused unchanged, group-writable, readable, read-only and relative folders
    refused, a symlinked library refused with its target unchanged, `wails dev` refusing without the
    variable). The owner's folder was never created.
  - `not_run`: power loss; a full disk at the application start (028's form); an automated run of the
    `dev` build (it does not link outside the Wails CLI).
- **Review:** three agents found no critical defect. Fixed test-first:
  - a symlink at the database path was followed;
  - a readable data folder was accepted;
  - the gate ignored a read error of its tool folder;
  - the marks table was not STRICT.

  The other findings were fixed in the docs or answered in the report. Owner decisions afterwards:
  the operation kind is checked in Go only; 016 is a hard dependency of 023. While making the first
  of them, a mutation showed the frozen fixture did not guard `CHECK` text; a text test now does.
- **Limitations:**
  - No repositories yet (015), no backup before a migration (016), and the start failure is shown on
    stderr only (028).
  - A branch build launched by hand without `OCTOPUS_DATA_DIR` is the application build and opens the
    owner's folder (documented; 016's backup makes such an upgrade reversible).
  - CHECK contents are outside the structural comparison (the frozen fixture guards the released
    schema).
  - The branch is based on PR #19 (on #18), which merge first.
- **Next:** [015](015-transactional-repositories.md) (unblocked by this task), and
  [017](017-local-diagnostics-privacy.md), [018](018-torrent-engine-adapter.md) and
  [035](035-react-bridge-state-resync.md), as before.
