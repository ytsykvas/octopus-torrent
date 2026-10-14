# 052 — Accept the first working end-to-end scenario

**Status:** `planned`  
**Phase:** P5 — Working main scenario and full MVP  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [034](034-wails-facade-real-bindings.md), [035](035-react-bridge-state-resync.md), [040](040-app-shell-fixed-geometry.md), [044](044-import-preview-ui-tree.md), [045](045-torrent-cards-status-actions.md), [029](029-restore-recheck-reconciliation.md)  
**Requirements:** `AI-08`, `STACK-05`, `TR-02`

## Outcome

Get an early working `.app`: choose a `.torrent` or paste a magnet, preview the contents, choose files and a folder, download, pause and restore the state after a restart. This is a narrow control check of the basic integration, not the full MVP.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-08`, `STACK-05`, `TR-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Build a real `.app` with the basic facade, SQLite and anacrolix/torrent. Use the controlled files from 006; start with an empty test library.
2. Choose a `.torrent` through the button, preview the tree, specify the folder and files and add it paused. There must be no transfer of file data until an explicit start.
3. Start the torrent, see real progress in the card, pause and resume the transfer. Compare the received bytes with the test sources.
4. Repeat the core import by pasting a magnet; before confirmation only the required metadata is transferred, and file data lands in the chosen folder.
5. Close and relaunch the `.app`; check a single entry without duplication, the file selection, path, progress and manual pause. Existing correct data is not downloaded again.
6. Keep evidence of exactly this slice. Finder, drag and drop, group actions, Trash, full settings and independent upload have separate acceptance in 054 and are not a condition of this early demonstration.

## Artifacts and change locations

**Proposed locations:** `docs/verification/core-slice.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/052-first-working-core-gate.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The owner can repeat add → start → pause → resume → restart in the real `.app` without the test bridge.
- [ ] Progress and completed files match the bytes and counters of the real engine; selection and pause survive a restart.
- [ ] Before import confirmation and with the initial pause, file data is not transferred.
- [ ] Missing group deletion, Finder or Trash do not block this slice; it is forbidden to call it the full MVP or evidence of full coverage of the 72 requirements.

## Checks

- For `.torrent` and magnet, walk the route above with checks of hashes, real counters and persisted SQLite values.
- Reopen the same torrent; check that there is no duplicate. Cancel the preview; an active entry must not appear.
- Quit the program on a manual pause and reopen it; the transfer does not resume until an explicit start.

**How to run:** use the actual build command from 004 and the specific test data from 006. Record the `.app` version, commands and results. AS-01/02/03/09/19 are checked here only in the stated parts; system opening, other screens and full acceptance remain in 054/065/068.

## Decisions, risks and boundaries

This checkpoint must show the gaps between the engine, the DB, Wails and React before all functions are completed. A failed 010 does not forbid independent download checks, but the manual pause must pass the own checks of 025. Unproven independent upload is not marked successful; the full MVP is not accepted before 054.

## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
