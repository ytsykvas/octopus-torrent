# 050 — Build settings with a truthful save result

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [037](037-native-dialogs-clipboard.md), [041](041-six-languages-formatters.md), [042](042-appearance-theme-opaque.md), [026](026-rate-limits-seed-network.md), [015](015-transactional-repositories.md)  
**Requirements:** `SET-01`, `SET-04`, `SET-05`, `SET-06`, `SET-07`, `TR-04`

## Outcome

Build settings with explicit saving and a truthful result.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-01`, `SET-04`, `SET-05`, `SET-06`, `SET-07`, `TR-04`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Separate the draft from the saved settings; invoke folder selection/creation through Go.
2. The default folder affects only future imports.
3. Validate the unlimited mode, positive limits and consistent units.
4. Apply theme/language through 041/042, default-application instructions through 036.
5. A save failure keeps the confirmed value and shows an error.
6. Integrate global upload into 053/054; account/plan/sync are not added.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/settings`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/050-settings-ui-save-feedback.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Settings are preserved after a restart.
- [ ] Changing the default folder does not move existing files.
- [ ] Cancellation/creation error does not change the previous choice.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called passed.

## Checks

- Check invalid limits, overflow and a save failure.
- Check system cancel, six languages and the keyboard.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Stay within the boundaries of our own DTOs, repository and engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
