# 015 — Isolate SQL and ensure transactional writes

**Status:** `planned`  
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

- [ ] A persistence confirmation matches the result that is actually written and available after reopening the DB.
- [ ] Request cancellation and a failed transaction commit return `AppError`.
- [ ] React and the engine adapter do not execute SQL directly.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Concurrently reserve a torrent identity and change the file selection, the global policy and pause; transactions must not create duplicates or lose persisted changes.
- Reproduce a lost DB connection, a transaction commit error and request cancellation; success must not be returned for unpersisted changes.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

A successful write of a policy to SQLite does not prove it was applied in the engine. These results must be tracked separately and reconciled after a failure. Repository dependencies are passed explicitly; engine objects are not stored as DB data.

**Basis for technical decisions:** [database/sql](https://pkg.go.dev/database/sql). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
