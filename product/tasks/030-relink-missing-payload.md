# 030 — Re-select the location of missing data

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [029](029-restore-recheck-reconciliation.md), [020](020-destination-filesystem-service.md)  
**Requirements:** `SET-03`, `TR-05`

## Outcome

Explicitly locate missing data and verify it without automatic moving.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-03`, `TR-05`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. For a problematic record, offer a choice of an existing location, keeping the old path until confirmation.
2. Verify the new structure and contents; forbid transfer while relinking.
3. Recreate storage according to the proven contract; do not treat MergeSpec.Storage as a way to change the folder.
4. Record the path change in the journal, preserving the UUID, wanted, manualPaused and the global policy.
5. Verify the bytes before starting; cancellation does not change the default folder or the previous error.

## Artifacts and change locations

**Proposed locations:** `internal/app/relink_service; internal/files`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/030-relink-missing-payload.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] There is no silent replacement of the location or moving of files.
- [ ] Correct data that is found is reused.
- [ ] A wrongly chosen folder is not overwritten.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check a wrong root, partially present data and a read-only folder.
- Cancel the verification and force-quit the application during relinking.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Locating and rechecking existing data is agreed. Do not add arbitrary moving of an active download as a new feature.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): a folder handed to the engine is checked by the engine itself, which renames every file of a failing piece to `<name>.part` and later overwrites it (finding 13). "A wrongly chosen folder is not overwritten" therefore needs the application's own hash check of the candidate folder before the engine sees it.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): your input is `Decision.Missing` from `files.Decide`. After the owner picks a location, the files there are not trusted: run `Decide` without a record (everything is verified) and let the engine hash. A volume that was only replugged (another device number) is not missing: it is verified in full.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and evidence of checks.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
