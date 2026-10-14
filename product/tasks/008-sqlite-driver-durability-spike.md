# 008 — Choose the SQLite driver and data durability mode

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [004](004-reproducible-check-commands.md)  
**Requirements:** `STACK-04`, `SET-02`, `AI-07`

## Outcome

Choose the SQLite driver based on the results of building on the Mac, checking persistence of committed changes and cancellation of operations.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `STACK-04`, `SET-02`, `AI-07`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If required evidence is missing, the dependent part of the work must not start.

## Work order

1. Compare suitable drivers by system dependencies, the need for CGO and an API for creating a consistent backup.
2. Check transactions, cancellation via `context`, the busy-database timeout and concurrent operation of readers and a writer.
3. Obtain the `sqlite_version()` value from the built program itself and record it in the report.
4. If WAL is chosen, check that the fixes in the bundled SQLite version are current, and check concurrent writes together with checkpointing changes from the journal into the DB.
5. Record the driver, connection pool parameters, write order, guarantees of persistence of committed changes and the application's local data directory.

## Artifacts and change locations

**Proposed locations:** `docs/decisions/sqlite-driver.md; internal/library prototype`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/008-sqlite-driver-durability-spike.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A failed transaction commit returns an error, not a message about successful persistence.
- [ ] The built program launches without libraries installed for the end user via Homebrew.
- [ ] The WAL decision is based on the verified SQLite version and the results of concurrent writes and journal checkpointing.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Perform concurrent writes and journal checkpointing, as well as cancellation and timeouts; each operation must end with a defined result without hidden loss of committed changes.
- Try to open the DB in a directory without write permission and write data with a full disk; the application must show the reason and keep the existing DB without automatically resetting it.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

SQL `PRAGMA` parameters must apply to all connections that need them. If WAL gives no practical benefit, a simpler mode may be chosen, describing its data persistence guarantees. Default settings must not be treated as verified guarantees.

**Parallel workspaces (added 2026-10-15, [agent-tooling ADR](../../docs/decisions/agent-tooling.md)):** several octopus/Conductor worktrees run `wails dev` at once. The library location must be configurable (for example an environment variable that `go tool devtool workspace run` sets per worktree) so a workspace, a test or a spike never opens the owner's real library; record the contract in the architecture.

**Basis for technical decisions:** [SQLite WAL](https://www.sqlite.org/wal.html), [database/sql](https://pkg.go.dev/database/sql). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a performed integration check.

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
