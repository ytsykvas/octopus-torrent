# 032 — Safely move confirmed files to the Trash

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [031](031-delete-record-lifecycle.md), [020](020-destination-filesystem-service.md), [012](012-macos-trash-and-path-spike.md)  
**Requirements:** `DEL-02`, `DEL-04`, `DEL-05`, `STACK-01`

## Outcome

Move confirmed files to the Trash, record the result for each file and report partial errors.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `DEL-02`, `DEL-04`, `DEL-05`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. Prepare an immutable list of the actual files of the selected torrents, with checks of ownership, shared use and a control fingerprint. This list concerns deletion; `wanted` by itself is not permission to delete or keep a previously received file.
2. Show the list and the location before confirmation. Cancellation ends only the review: the record, the pause and the files do not change.
3. After confirmation, save the `with_files` mode, the list and the `operationID` in the journal, then call only the shared preparation from 031. The library row remains stopped; do not call the `record_only` mode finalization.
4. Before each move, recheck the identity, ownership and path of the file. Move only confirmed safe files; do not recursively delete the whole folder and do not touch unrelated content.
5. Save the intent and the result of each move. On a partial error, keep the record stopped and explain the verified leftovers; mark a result that is undetermined after a crash separately, rather than guessing from the absence of the original file.
6. Only after verified success of all required actions, finalize deletion of the DB row. After a crash, reconcile the journal with the actual files again without automatic retries over substituted paths; recovery does not enable transfer.

## Artifacts and change locations

**Proposed locations:** `internal/files/delete_plan; trash executor; operation journal`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/032-safe-payload-trash-journal.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Torrent files outside the confirmed list, shared files and unrelated files are not changed.
- [ ] Cancelling the review does not stop the torrent and does not change data.
- [ ] A partial error leaves a stopped record with verified leftovers and explicit undetermined results; the record does not disappear prematurely.
- [ ] The journal and the DB state are recovered after a crash at every boundary; a repeated request does not move the same file a second time and does not resume transfer.

## Checks

- Check a symbolic/hard link, a substituted file and a change of the list before confirmation.
- Check a Trash refusal and a forced shutdown before/after moving each file.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

- Make the move of the second file fail after the first succeeds. After a restart, the DB row must exist in a stopped state; the error and the leftover are visible. Separately, kill the process after the physical move but before the result is recorded: the state is marked undetermined until verified, not automatically successful.

## Decisions, risks and boundaries

Deletion by the name of the download folder is forbidden: we work with a verified list of each file. If ownership is ambiguous, we keep the file and explain the reason.

**Basis for technical decisions:** [Go filesystem roots](https://go.dev/blog/osroot). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): a deletion with files is confirmed by a Go-issued `deletionPlanID` bound to the exact torrents and files, single-use; quitting abandons a mutation after 5 s, so the journal must exist before any file moves and every commit phase must tolerate exit.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): an incomplete torrent's files can exist on disk only as `<name>.part` (the engine's default part-file mode, finding 13). The Trash journal must list and move the part-file names too, and never a user's file the engine renamed (DEL-02, DEL-04).

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
