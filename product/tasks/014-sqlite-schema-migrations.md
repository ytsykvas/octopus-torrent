# 014 — Create the SQLite schema and safe migrations

**Status:** `planned`  
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

- [ ] An empty DB and all supported previous schemas migrate to the current version.
- [ ] A migration error does not cause loss of existing records or files.
- [ ] Identity uniqueness constraints and foreign keys are actually enforced by SQLite.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Interrupt a migration, open a DB of a newer version and try to write a duplicate full hash; each case must produce a defined result without destroying previous data.
- Reproduce an access denial, a full disk and concurrent transaction commits; errors must be returned to the caller, and schema constraints must be preserved.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The schema contains library data, not internal engine objects. Migrations run through the defined SQLite access layer. An unsupported version or a migration error requires an explicit message; automatically creating an empty DB in place of the existing one is unacceptable.

**Parallel workspaces (added 2026-10-15, [agent-tooling ADR](../../docs/decisions/agent-tooling.md)):** several octopus/Conductor worktrees run `wails dev` at once. The library location must be configurable (for example an environment variable that `go tool devtool workspace run` sets per worktree) so a workspace, a test or a spike never opens the owner's real library; record the contract in the architecture.

**Basis for technical decisions:** [SQLite WAL](https://www.sqlite.org/wal.html), [PRAGMA checks](https://www.sqlite.org/pragma.html#pragma_integrity_check). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 008** ([sqlite-driver](../../docs/decisions/sqlite-driver.md)): open the database only with `library.Open` (WAL, `synchronous=FULL` + `fullfsync`, `busy_timeout` 5 s, foreign keys, one `BEGIN IMMEDIATE` writer, four `query_only` readers) and write through `Write(ctx, func(library.Tx) error)`; batch rows into one transaction, since each commit costs about 4 ms with `fullfsync`. The data directory contract is fixed: `~/Library/Application Support/dev.tsykvas.octopustorrent/library.sqlite`, override `OCTOPUS_DATA_DIR` (an absolute folder, validated in Go). If this is the first task that reads it, implement the override, make `go tool devtool workspace run` set it per workspace, and make the gate's `.app` run pass a temporary folder (`open --env OCTOPUS_DATA_DIR=…`) and fail if the default folder changes ([quality-gate](../../docs/decisions/quality-gate.md#consequences)). `Open` never resets a database; a migration failure must keep that property. Before switching or migrating a file, set and verify `PRAGMA application_id`, so a valid SQLite file of another application is refused rather than converted. Validate `OCTOPUS_DATA_DIR` as the ADR specifies (absolute, clean, symlinks resolved, owned by the user, not group- or world-writable, not inside a save folder; created `0700`), and turn "the gate and workspaces never open the owner's library" into a failing test as soon as the default path function exists. The application-level part of 008's read-only-folder and full-disk checks (the reason shown to the user) is `not_run` in 008, which opens no file database in the app.

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
