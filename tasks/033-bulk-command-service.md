# 033 — Execute bulk commands with partial results

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [025](025-pause-resume-application.md), [031](031-delete-record-lifecycle.md), [032](032-safe-payload-trash-journal.md)  
**Requirements:** `UI-07`, `DEL-05`, `STACK-03`

## Outcome

Execute a bulk command only for the selected UUIDs, with a result for each record.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-07`, `DEL-05`, `STACK-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. Validate an immutable list of UUIDs, remove duplicates and create an operationID.
2. Serialize the commands of each record without holding a global lock for the whole bulk operation.
3. Return success, an error or a missing record for each UUID.
4. Deletion confirmation covers only the selected records and their verified file list.
5. Reconcile the bulk command with the global policy; repeating the operation does not duplicate the action.

## Artifacts and change locations

**Proposed locations:** `internal/app/bulk_operations`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/033-bulk-command-service.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Unselected records are not changed.
- [ ] A partial failure is not shown as overall success.
- [ ] Empty and repeated requests have defined behavior.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check different record states, a missing UUID and one failed operation.
- Simultaneously run a bulk action, closing and the global toggle.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Go race detector](https://go.dev/doc/articles/race_detector). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and evidence of checks.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
