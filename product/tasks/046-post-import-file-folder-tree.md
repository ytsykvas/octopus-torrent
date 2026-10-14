# 046 — Build the tree of file and folder buttons

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [044](044-import-preview-ui-tree.md), [024](024-file-folder-wanted-commands.md), [045](045-torrent-cards-status-actions.md)  
**Requirements:** `UI-09`, `UI-11`, `UI-18`, `TR-06`, `TR-07`

## Outcome

Build a shared tree of file and folder buttons with visible nesting in the card and in the details.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-09`, `UI-11`, `UI-18`, `TR-06`, `TR-07`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. A wanted file has pause, an excluded one has play, even after completion; there is no checkbox in an added torrent.
2. A fully selected folder has pause; a partially selected one has play to select all descendants.
3. Collapsing changes only the view; controls are unavailable before metadata.
4. Shift the whole child row with the button, name, size and progress bar to the right at every level.
5. The size sits above the progress bar; compactness does not remove the vertical gaps between hit areas.
6. Command pending state, error and focus are the same in both places; bytes already received are not reset to zero.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/files/FileTree`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/046-post-import-file-folder-tree.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A child's button is to the right of the parent folder's button.
- [ ] The hit areas of neighboring buttons do not overlap.
- [ ] Partial selection, collapsing and completed files are consistent in the card and in the details.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check deep/collapsed descendants and a partial folder.
- Check a completed excluded file, a shared piece and focus after a failed command.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

The whole child row must be shifted, including the button, not just the name. Enlarging hit areas must not destroy the compactness and readability of the tree.

**Basis for technical decisions:** [File source](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
