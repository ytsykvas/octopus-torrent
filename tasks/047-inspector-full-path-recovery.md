# 047 — Build details, full path and missing-data search

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [046](046-post-import-file-folder-tree.md), [037](037-native-dialogs-clipboard.md), [030](030-relink-missing-payload.md)  
**Requirements:** `UI-02`, `UI-11`, `UI-12`, `UI-15`, `SET-03`

## Outcome

Build the details panel with exact data, the full path and location recovery.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-02`, `UI-11`, `UI-12`, `UI-15`, `SET-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Read the snapshot of the selected UUID for the name, progress ring, bytes, peers, ETA, actions, tree and date.
2. Shorten the path only visually; expanding/copying passes the full Unicode value.
3. For pause/completion, hide speeds according to the card rule.
4. When the selected record is deleted, apply the defined fallback state without a stale object.
5. A missing folder leads to the explicit search of 030; the default folder does not move existing data.
6. Keep independent scrolling of the panel.

6. Connect the path re-selection command from service 030 through the shared facade 034, regenerate the bindings and verify the real system dialog of 037; the test bridge does not replace this integration.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/inspector`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** a `docs/verification/047-inspector-full-path-recovery.md` record or an exact reference to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The card and the details are consistent by revision.
- [ ] The clipboard receives the full path.
- [ ] A canceled folder search preserves the previous state.
- [ ] The required checks have been performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Verify deletion of the selected record and a record hidden after a filter.
- Verify system copying of a long path and an invalid/missing folder.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, of the test UI and of system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undetermined parameters in an ADR together with the implementation.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
