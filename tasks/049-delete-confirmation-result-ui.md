# 049 — Confirm deletion and show partial results

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [048](048-persistent-bulk-selection-ui.md), [047](047-inspector-full-path-recovery.md), [032](032-safe-payload-trash-journal.md)  
**Requirements:** `DEL-01`, `DEL-02`, `DEL-04`, `DEL-05`, `UI-13`

## Outcome

Confirm single and bulk deletion with the real file list and partial results.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `DEL-01`, `DEL-02`, `DEL-04`, `DEL-05`, `UI-13`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. Show the names and actual paths from the versioned Go list.
2. Keep files by default; an explicit checkbox reveals the data list.
3. Cancellation is safe; the card does not disappear until the actual result.
4. Show partial record/file errors and the exact paths of leftovers.
5. After a restart, show the restored unfinished operation without a false success.

6. Connect both deletion modes to the facade: record only, and record with files. Confirmation passes the list version and a single `operationID`; a partial result from 032 is shown without losing the stopped record.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/delete`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/049-delete-confirmation-result-ui.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Cancellation does not change data.
- [ ] The list contains no unselected records.
- [ ] A Trash error is not called success.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] An error on the second file after the first was moved successfully shows a partial result and a stopped record; reloading the UI or the `.app` does not hide it.

## Checks

- Check cancel/close and long paths.
- Check a change of the list before confirm, a partial failure and a restart.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [W3C modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and evidence of checks.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
