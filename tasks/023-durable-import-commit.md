# 023 — Persist the import and policies before file transfer starts

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [022](022-pending-import-coordinator.md), [015](015-transactional-repositories.md), [013](013-transfer-policy-state-machine.md), [009](009-storage-and-completion-spike.md)  
**Requirements:** `ADD-04`, `ADD-07`, `SET-02`, `STACK-04`

## Outcome

Reliably persist a confirmed import and its settings before file data download or upload is allowed.

## Read before starting

- [Task execution rules](README.md): start and completion conditions, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-04`, `ADD-07`, `SET-02`, `STACK-04`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory prerequisite tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Write the torrent entry, the file selection, the final path, `manualPaused` and the operation intent to the journal.
2. Create the engine object with file data transfer forbidden and with a verified final storage.
3. Persist the creation result and only then allow transfer according to the current revision of the global policy.
4. On error, remove only the new objects created by this import; do not delete files that existed before it.
5. Return the current state snapshot; reject adding with no files selected and ensure that a repeated confirmation gives the same result without creating a duplicate.

## Artifacts and change locations

**Proposed locations:** `internal/app/import_commit.go; operation journal`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/023-durable-import-commit.md` or an exact link to an existing check or build report in the handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The initial pause choice is persisted before the first file bytes are transferred.
- [ ] A transaction commit error does not leave an active torrent without a corresponding library entry.
- [ ] After a crash, either a recoverable entry or an explicit incomplete-import error remains.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Inject an error at each boundary: DB write, storage creation and engine object creation; make sure existing data is preserved and there is no active untracked torrent.
- Confirm the import twice, change the global policy during adding and force-terminate the process; after recovery, exactly one entry with the correct policies must remain.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Creating the entry, the storage and the engine object passes through several separate checkpoints. The journal must allow determining, after a failure, which actions had completed. Rollback is limited to the objects of the specific new import and never deletes previously existing data.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a completed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what changed, exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
