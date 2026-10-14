# 011 — Check magnet metadata retrieval and the final file folder

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [009](009-storage-and-completion-spike.md)  
**Requirements:** `ADD-02`, `ADD-04`, `ADD-07`

## Outcome

Prove that magnet metadata can be obtained without transferring file data before confirmation, and that after confirmation the folder actually selected is used.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-02`, `ADD-04`, `ADD-07`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Create a separate context for the pending import and temporary storage owned by it; forbid transfer of file data from the start.
2. Wait for metadata with cancellation support and a bounded wait time; record the created file objects and network counters.
3. After confirmation, use a different `saveRoot` through a verified removal and re-add or another supported strategy. Do not try to change `Storage` through `MergeSpec`.
4. Cancel the import before and after the metadata is obtained. Do not call `Drop` on the object of an already existing torrent that this import does not manage.
5. Describe cleanup of only our own temporary objects and handling of calls that arrive after cancellation.

## Artifacts and change locations

**Proposed locations:** `docs/decisions/pending-magnet.md; docs/verification/magnet-import.md`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/011-magnet-metadata-storage-spike.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Before explicit import confirmation, file data is neither downloaded nor uploaded.
- [ ] After confirmation, physical files are created exactly in the selected directory.
- [ ] Cancelling the import does not change existing library records or their data.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Do not connect any other client, wait for the time limit, and separately cancel the import simultaneously with the `GotInfo` signal; in all cases the tasks must finish and file data must not be transferred.
- Repeat adding with `new=false`, double confirmation and program shutdown; the existing torrent must remain intact, and the new record must not be duplicated.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

A pending import has no right to remove the object of an existing torrent that it does not manage. `MergeSpec` does not change `Storage`; correctness of the final folder is proven by checking the physical files, not only by the field value in the state snapshot.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

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
