# 031 — Remove a record while keeping files

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [029](029-restore-recheck-reconciliation.md), [025](025-pause-resume-application.md)  
**Requirements:** `DEL-01`, `DEL-03`, `STACK-03`

## Outcome

Delete a record and stop its activity in the engine while keeping the downloaded files.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `DEL-01`, `DEL-03`, `STACK-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Record the confirmed UUIDs, `operationID` and the `record_only` mode in a durable journal BEFORE removing the engine object. Mark the record as being deleted and forbid starting it again.
2. Extract a shared preparation: stop transfer, cancel workers and remove only this record's object from the engine; wait for verified completion of file access.
3. For `record_only`, after preparation complete the operation transactionally: remove the library record and keep the summary/idempotency in a journal that does not disappear in a cascade together with the record.
4. For the future `with_files` mode, provide the same preparation but NOT the final deletion of the record: task 032 must keep the record and the listing until the file actions complete. Do not build 032 as a call to a fully completed `record_only`.
5. Clean up only service data belonging to this record; do not change the obtained user files in `record_only` mode.
6. After a crash between the journal, removal from the engine and the transaction, recover or explicitly show the unfinished operation. Late events and a repeated request do not revive the record and do not start transfer.

## Artifacts and change locations

**Proposed locations:** `internal/app/delete_coordinator`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/031-delete-record-lifecycle.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] All downloaded bytes remain on disk.
- [ ] The record does not come back after a restart or a late event.
- [ ] A borrowed handle of an existing torrent is not removed.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called passed.

- [ ] The deletion intent is saved before the engine stops; a crash after that does not start the record again.
- [ ] The shared preparation does not delete the DB row for the `with_files` mode; the journal of a completed `record_only` is available after the row is deleted.

## Checks

- Check active upload, pause, metadata waiting and a problem record.
- Concurrently run delete/start/file selection/close; check a repeated delete and a DB failure after Drop.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

- Force-kill the process after saving the intent, after removing the engine object and before the final transaction. Repeat the same `operationID`: there must remain one result without reviving the record.

## Decisions, risks and boundaries

Stay within the boundaries of our own DTOs, repository and engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
