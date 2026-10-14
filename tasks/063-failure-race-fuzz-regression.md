# 063 — Check failures, races and crash recovery

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [054](054-global-upload-ui-mvp-gate.md), [062](062-automation-end-to-end-gate.md), [032](032-safe-payload-trash-journal.md), [029](029-restore-recheck-reconciliation.md)  
**Requirements:** `AI-08`, `STACK-03`, `STACK-04`, `TR-05`, `DEL-04`, `QA-02`, `QA-05`, `QA-06`, `QA-08`

## Outcome

Close the risks of failures, races and crashes at the appropriate system boundary.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-08`, `STACK-03`, `STACK-04`, `TR-05`, `DEL-04`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Build an import/pause/wanted/global/delete/quit matrix with controlled DB, disk and network failures.
2. Perform forced termination only with a test helper over our own fixtures.
3. The race detector must exercise the real concurrent code paths.
4. Bounded fuzzing of our own parser/path/filter inputs starts from prepared examples.
5. Keep unit/repository/real engine/fake UI/WebKit/native evidence separate.
6. Every bug found gets a specific non-regression check; exact 100% coverage of all own executable code per QA-02/QA-05 must be verified.

7. Check the quality mechanisms themselves with negative examples: a hidden uncovered module, a missing profile, a disabled test, inconsistent sources and an undocumented feature must block completion. Compare the 100% with the errors, resources and boundaries actually checked; eliminate any workarounds and debt found within the current task.

## Artifacts and change locations

**Proposed locations:** `testdata corpus; docs/verification/failure-matrix.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/063-failure-race-fuzz-regression.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A failure does not delete unconfirmed data.
- [ ] Races are eliminated or the check is explicitly blocked.
- [ ] The test bridge does not replace evidence of network integrity.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check commit/apply/corruption and symlink failures.
- Check cancel/delete/late events and bounded fuzz/crash checkpoints.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Go race detector](https://go.dev/doc/articles/race_detector), [Go fuzzing](https://go.dev/doc/tutorial/fuzz). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
