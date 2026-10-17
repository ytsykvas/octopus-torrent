# 029 — Restore the library after restart and crash

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [028](028-application-startup-quit.md), [023](023-durable-import-commit.md), [024](024-file-folder-wanted-commands.md), [027](027-metrics-status-snapshots.md), [016](016-migration-backup-corruption-recovery.md), [009](009-storage-and-completion-spike.md)  
**Requirements:** `SET-02`, `TR-05`, `DATA-01`

## Outcome

Restore the persisted state before transfer is allowed, after both a normal and a crash startup.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-02`, `TR-05`, `DATA-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Open and migrate the library; read unfinished operations.
2. Restore identity, metadata, folder, wanted, manual and global policy; add engine objects with payload initially disallowed.
3. Check changed files and untrusted completion markers according to decision 009, with the ability to cancel.
4. Reconcile unfinished import/deletion through the journal without cleaning up unknown files.
5. Isolate an error of a single entry. Do not replace a missing folder with a new download to an arbitrary location.

## Artifacts and change locations

**Proposed locations:** `internal/app/recovery.go; recovery fixtures`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/029-restore-recheck-reconciliation.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Existing correct bytes are not downloaded again, as confirmed by the network counters.
- [ ] A manually stopped entry does not transfer payload even briefly at startup.
- [ ] Changed data is not shown as fully ready.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check normal and forced termination at checkpoints.
- Corrupt the service completion markers, change bytes without changing the size, disconnect the disk and interrupt import/file selection.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Checking the disk after a crash does not mean downloading again from the network. TR-05 is proven by checking hashes and counters, not merely by the presence of a restored entry.

**Basis for technical decisions:** [Storage API](https://pkg.go.dev/github.com/anacrolix/torrent/storage). Cross-check signatures and limitations against the pinned version; reading the API is not a passed integration check.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): restore re-adds torrents from stored info bytes, and the engine accepts bytes that do not match the stored infohash (finding 11): verify the pair before the add. A recheck of a file whose bytes changed makes the engine rename that file to `<name>.part` and later overwrite it (finding 13): check existing bytes with the application's own hashing before the engine sees the folder.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): the rule is `files.Decide(record, saveRoot, pieces, files.System{})` and its output goes to `Completion.Distrust`; nothing here needs a hand-written recheck. Marks are trusted only for files of a clean earlier run on APFS whose fingerprint is unchanged; after a crash, a storage problem or a late operation everything is verified; a vanished file is `Missing` and goes to 030 (nothing is added, nothing is downloaded). An unreadable marks store is an explicit error; rebuilding it is a separate, explicit action. A newer-version store is `ErrUnsupportedVersion` and must not be rebuilt. The `.app` restart and crash scenarios were `not_run` in 009 and are yours.

## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
