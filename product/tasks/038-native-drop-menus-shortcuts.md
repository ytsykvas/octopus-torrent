# 038 — Add native drag and drop, menus and keyboard

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [034](034-wails-facade-real-bindings.md), [036](036-finder-magnet-open-queue.md), [037](037-native-dialogs-clipboard.md)  
**Requirements:** `ADD-06`, `UI-04`, `UI-17`

## Outcome

Implement system drag-and-drop and the standard Mac menus and keyboard commands.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-06`, `UI-04`, `UI-17`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Set up EnableFileDrop/OnFileDrop with absolute paths and cleanup via OnFileDropOff.
2. Check the count, type and metadata in Go; the highlight rejects a known unacceptable type.
3. Keep the enlarged drop zone from the mockup; clicking it opens the system file picker.
4. On the Mac, add AppMenu first and EditMenu for Cmd+C/V/Z.
5. Pasting a magnet and the Add shortcut lead into the shared import without duplicated keyboard listeners.

## Artifacts and change locations

**Proposed locations:** `internal/platform/menu; frontend/src/bridge/drop`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/038-native-drop-menus-shortcuts.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A single valid file opens the preview.
- [ ] Several or corrupted files produce a clear rejection.
- [ ] Cmd+C/V work in the real window.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Drag from Finder a Unicode-named file, several files and an invalid file.
- Go through adding/pasting using only the keyboard and check cleanup after reload.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Wails system drag-and-drop returns absolute paths. Do not use the browser File.path as the contract of the system drop.

**Basis for technical decisions:** [Wails drag/drop](https://wails.io/docs/reference/runtime/draganddrop/), [Menus](https://wails.io/docs/reference/menus/). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
