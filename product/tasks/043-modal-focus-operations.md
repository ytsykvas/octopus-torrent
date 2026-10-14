# 043 — Implement modal dialogs, focus and cancellation

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [035](035-react-bridge-state-resync.md), [039](039-visual-primitives-tokens.md), [041](041-six-languages-formatters.md)  
**Requirements:** `UI-04`, `ADD-07`, `DEL-02`

## Outcome

Build a shared modal dialog mechanism with focus handling and real cancellation.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-04`, `ADD-07`, `DEL-02`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Define controlled state, title/description, focus trapping and focus return.
2. Define the Escape/outside-click policy; a destructive action is not the default option.
3. Long content scrolls; the bottom buttons remain reachable when zoomed.
4. Pending, success and error states come from the Go operation, not from an optimistic UI assumption.
5. Canceling metadata calls the coordinator; a late event is ignored, and a new opening gets a new pending ID.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/modals`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/043-modal-focus-operations.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Tab/ShiftTab do not leave the dialog.
- [ ] Cancellation does not change the library.
- [ ] Focus returns to the element that opened the dialog or to a defined fallback element.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check Escape pressed simultaneously with confirmation, and a late event.
- Check a native child dialog, a long tree and repeated opening/closing.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [W3C modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
