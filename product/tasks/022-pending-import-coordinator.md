# 022 — Create a single coordinator for pending imports

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [011](011-magnet-metadata-storage-spike.md), [019](019-metainfo-parser-file-tree.md), [020](020-destination-filesystem-service.md), [021](021-full-hash-deduplication.md)  
**Requirements:** `ADD-01`, `ADD-02`, `ADD-06`, `ADD-07`, `STACK-03`

## Outcome

Create an `ImportCoordinator` that handles all adding sources in the same way and supports canceling a pending import.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-01`, `ADD-02`, `ADD-06`, `ADD-07`, `STACK-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): the API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Describe the state table of a pending import, its identifier, version, source and cancellation context.
2. For `.torrent`, parse the metadata without active file transfer; implement magnet import according to the verified decision of task 011.
3. In Go, validate the number and format of the incoming objects, the identifiers and the version of the preview that the user confirms.
4. Organize a bounded queue of previews, navigation to an existing duplicate, cancellation and a timeout.
5. Provide a shared API for the button, Finder, drag-and-drop and the watch folder; perform the final persistence of the import through task 023.

## Artifacts and change locations

**Proposed locations:** `internal/app/import_coordinator.go; pending states`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/022-pending-import-coordinator.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A canceled or expired import cannot create an active record after a late confirmation.
- [ ] The metadata-waiting state matches the actual progress of the operation.
- [ ] A new input does not replace an open preview without an explicit user action.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Cancel an import simultaneously with the `GotInfo` signal and a confirmation; only the valid current operation may proceed to persistence.
- Pass forged identifiers and paths, reopen the interface, quit the program and repeat the preparation; no duplicates, stale confirmations or leftover tasks may arise.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The coordinator validates commands independently of the interface and does not allow a stale preview to confirm a different set of files. Accepting a request does not mean the import is complete. After cancellation, only the resources that belong to this specific pending import are cleaned up.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, check the signatures and constraints against the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
