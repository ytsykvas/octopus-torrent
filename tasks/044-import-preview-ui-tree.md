# 044 — Build the add preview with a tree

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [043](043-modal-focus-operations.md), [037](037-native-dialogs-clipboard.md), [022](022-pending-import-coordinator.md), [023](023-durable-import-commit.md)  
**Requirements:** `ADD-01`, `ADD-02`, `ADD-04`, `ADD-07`, `ADD-08`, `ADD-09`

## Outcome

Build the real import preview with file selection in a tree.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-01`, `ADD-02`, `ADD-04`, `ADD-07`, `ADD-08`, `ADD-09`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Handle the system .torrent selection and magnet pasting, including waiting, timeout, cancellation and error.
2. Build a validated tree without a duplicated root name; separate collapsing from the checkbox.
3. Folder selection takes into account hidden descendants, the indeterminate state and the exact sum of selected bytes.
4. Folder selection/creation, starting after adding and the absence of selected files are validated by Go.
5. A duplicate focuses the existing UUID; a double confirmation does not create a second operation.
6. Full paths are available to assistive technologies; do not replace the preview checkboxes with the buttons of an already added torrent.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/import`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/044-import-preview-ui-tree.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A .torrent shows its contents immediately, a magnet after the real metadata.
- [ ] Cancellation does not add an entry or a file transfer.
- [ ] Collapsing does not change the selected sum.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check a single file, nested folders, pad/zero-length and repeated names.
- Check no selection, a partial folder, a duplicate and cancellation concurrent with metadata.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

The preview checkboxes are kept. Replacement with play/pause is needed only in the files of an already added torrent; bulk checkboxes are a separate mechanism.

**Basis for technical decisions:** [W3C treeview](https://www.w3.org/WAI/ARIA/apg/patterns/treeview/). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

The file selection button and magnet pasting must work without Finder and drag and drop being ready. Tasks 036/038 connect additional sources to this same preview; they do not create a second version of it. The full MVP checks all sources in 054.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** a minimal correct implementation, tests and the required regression; **100% of all owned executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Write a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock it and the independent tasks that can continue.
- Update the documentation, the contract and the README if the API, structure or build commands changed; do not leave required context only in the chat.
