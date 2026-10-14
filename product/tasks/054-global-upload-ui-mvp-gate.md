# 054 — Finish the upload settings and accept the full MVP

**Status:** `planned`  
**Phase:** P5 — Working main scenario and full MVP  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [053](053-global-upload-policy-production.md), [052](052-first-working-core-gate.md), [036](036-finder-magnet-open-queue.md), [038](038-native-drop-menus-shortcuts.md), [046](046-post-import-file-folder-tree.md), [047](047-inspector-full-path-recovery.md), [048](048-persistent-bulk-selection-ui.md), [049](049-delete-confirmation-result-ui.md), [050](050-settings-ui-save-feedback.md), [051](051-offline-errors-notifications.md)  
**Requirements:** `SET-08`, `TR-08`, `AI-08`

## Outcome

Finish the global upload interface and accept the entire agreed MVP: the basic slice from 052 together with system inputs, files/folders after adding, details, bulk actions, deletion, settings and errors. Search, collections and automation are accepted at the next stage.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-08`, `TR-08`, `AI-08`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Add a localized upload toggle, an explanation of the consequences and pending/error states; the source of confirmation is Go and the verified strategy from 053.
2. On existing, new and restored records, run AS-18 with local and remote file data counters; manual pauses are preserved.
3. After the early slice from 052, check Finder and magnet on cold/repeated launch, drag-and-drop, system dialogs, file selection after adding, details and full paths.
4. Check bulk start/pause and both deletion modes, including cancellation, a partial Trash error and preservation of foreign files.
5. Check the default folder, language/theme, limits, network and disk messages, and restore after restart. Confirm consistency of all new facade methods and generated bindings.
6. Record the coverage of MVP requirements and the results of AS-01–14/18/19. Search and heavy loads are additionally checked in 055/064, full accessibility in 065, organization and automation in 062. An unproven TR-08 or any other MVP requirement prohibits completing this task.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/settings/TransferSettings; docs/verification/mvp.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/054-global-upload-ui-mvp-gate.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] System import sources, the file tree, details, bulk actions, both deletion modes, settings and errors are verified in the real `.app`.
- [ ] Global upload applies to all relevant records, persists after restart and does not cancel manual pauses; the verified time to apply is explained in the report.
- [ ] Early partial evidence from 052 is supplemented by actual integration checks, not automatically credited to the whole MVP.
- [ ] Every unfulfilled check has `not_run` or `fail`; the `done` state is impossible with an unfulfilled agreed MVP requirement.

## Checks

- Run AS-01–14/18/19 at the appropriate boundaries, noting the parts that will be finally verified in 055/064/065/066. Do not credit collection and automation scenarios ahead of time.
- Cause a settings persistence failure, a timeout while stopping upload and an error on one file during bulk deletion; show the truthful result without losing other records.

**How to run:** use the build and data from 004/006. Keep the date, version, configuration and evidence for each function. Results of the network engine, React and system capabilities do not substitute for one another.

## Decisions, risks and boundaries

Respect the boundaries of own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Upload implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/torrent.go). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
