# 048 — Implement persistent bulk selection

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [045](045-torrent-cards-status-actions.md), [033](033-bulk-command-service.md), [043](043-modal-focus-operations.md)  
**Requirements:** `UI-07`, `DEL-05`

## Outcome

Implement persistent bulk selection checkboxes separately from the open details.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-07`, `DEL-05`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Keep the selected UUIDs separate from the details UUID; a checkbox does not open the card.
2. The panel with the count, select all visible, clear and actions is shown at >=1; there is no Done/mode button.
3. Before a command, capture the IDs and remove hidden ones when the query/section changes.
4. Show the pending state and partial errors for each UUID from 033.
5. Deletion leads to 049; deleted UUIDs are removed from the selection.

6. Connect the bulk commands of service 033 to the shared facade, update the bindings and verify the actual result for each UUID. Their earlier absence must not block basic import.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/torrents/bulk`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** a `docs/verification/048-persistent-bulk-selection-ui.md` record or an exact reference to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Clicking a card does not change the bulk selection.
- [ ] Clearing the selection hides the panel.
- [ ] Hidden and unselected records do not receive the action.
- [ ] The required checks have been performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Verify different states, an empty selection and a deleted UUID.
- After 055, repeat a search with a hidden selection and select all visible.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, of the test UI and of system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Search and filters are introduced later in 055. Establish the rule that commands are not applied to hidden records now and re-verify it once search exists.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
