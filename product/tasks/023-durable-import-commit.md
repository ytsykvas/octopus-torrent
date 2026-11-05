# 023 — Persist the import and policies before file transfer starts

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [022](022-pending-import-coordinator.md), [015](015-transactional-repositories.md), [016](016-migration-backup-corruption-recovery.md), [013](013-transfer-policy-state-machine.md), [009](009-storage-and-completion-spike.md)  
**Requirements:** `ADD-04`, `ADD-07`, `SET-02`, `STACK-04`

## Outcome

Reliably persist a confirmed import and its settings before file data download or upload is allowed.

## Read before starting

- [Task execution rules](README.md): start and completion conditions, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-04`, `ADD-07`, `SET-02`, `STACK-04`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory prerequisite tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Write the torrent entry, the file selection, the final path, `manualPaused` and the operation intent to the journal.
2. Create the engine object with file data transfer forbidden and with a verified final storage.
3. Persist the creation result and only then allow transfer according to the current revision of the global policy.
4. On error, remove only the new objects created by this import; do not delete files that existed before it.
5. Return the current state snapshot; reject adding with no files selected and ensure that a repeated confirmation gives the same result without creating a duplicate.

## Artifacts and change locations

**Proposed locations:** `internal/app/import_commit.go; operation journal`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/023-durable-import-commit.md` or an exact link to an existing check or build report in the handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The initial pause choice is persisted before the first file bytes are transferred.
- [ ] A transaction commit error does not leave an active torrent without a corresponding library entry.
- [ ] After a crash, either a recoverable entry or an explicit incomplete-import error remains.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Inject an error at each boundary: DB write, storage creation and engine object creation; make sure existing data is preserved and there is no active untracked torrent.
- Confirm the import twice, change the global policy during adding and force-terminate the process; after recovery, exactly one entry with the correct policies must remain.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Creating the entry, the storage and the engine object passes through several separate checkpoints. The journal must allow determining, after a failure, which actions had completed. Rollback is limited to the objects of the specific new import and never deletes previously existing data.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a completed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): confirming an import is not cancellable once it runs; a partial result is `failed` with a code whose params list what was done.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): The engine truncates existing files at zero-length paths as soon as a torrent is added; the import commit adds the torrent to the engine only after the path plan (020) is confirmed.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): besides truncation at zero-length paths, a failed piece check renames existing files of that piece to `<name>.part` and the download then overwrites them (finding 13): validate the full path plan before the engine sees the torrent. The commit stores an infohash beside info bytes, and the engine would accept a mismatching pair (finding 11): verify the pair before it is stored and when it is read back.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): in the same transaction that commits the import, store the torrent's record for the rehash rule (initially none, so the first start verifies) and the "not clean" marker. The marks of the new torrent are advisory and live in `piece-completion.sqlite`, not in the library.

**From 011** ([magnet-pending-storage](../../docs/decisions/magnet-pending-storage.md), [feature](../../docs/app/magnet-metadata.md)): persist the record from `Pending.InfoBytes()` before `Confirm` allows download; `Confirm` with `Download=false` keeps the torrent attached but idle until the record is durable. The attach already creates the zero-length files and their folders in the chosen folder: a failed record write must drop the torrent and remove exactly those files (DEL rules: an explicit list). Start the download afterwards through `transferswitch.ResumeDownload`, like every resume (finding 25 cannot apply to a torrent that never requested, but one rule is simpler). Existing library records were `not_run` in 011 (no store): prove here that a cancelled import changes none.

**From 013** ([transfer-status-model](../../docs/decisions/transfer-status-model.md), [feature](../../docs/app/transfer-policy.md)): after the durable commit, run the new torrent's first step with `order.Run(ctx, {commandorder.Torrent(newID)}, …)`: `registry.Add` (with the stored `manualPaused` and `PhaseReady`: the zero phase is refused), then `Reconcile`. The reconcile reads the current global value itself, so a global change during the import cannot be missed. A failed import never reaches the registry.

**From 014** ([library-schema-and-data-folder](../../docs/decisions/library-schema-and-data-folder.md)): commit `torrents` (with `source_metainfo` or `source_magnet`) and `torrent_files` in the import's one transaction; `info` is the verified info bytes, and both infohashes are `UNIQUE` — of two concurrent imports of one hash exactly one commits and the other is `ErrConstraint` (proven in 014), which the service maps to "already in the library". This is the first task that puts the owner's records into the library, so 016 is a hard dependency (owner decision, 2026-11-04): a branch build launched by hand without `OCTOPUS_DATA_DIR` that carries a new migration upgrades the owner's library past what `main`'s build reads, and only 016's backup before a migration undoes that ([data folder](../../docs/app/data-folder.md#usage-and-recovery)).

**From 015** ([library-repositories](../../docs/decisions/library-repositories.md), [feature](../../docs/app/library-repositories.md)): this replaces the `ErrConstraint` part of the 014 note. Call `lib.AddTorrent(ctx, library.Torrent{…}, library.OperationIntent{…})` once, after the destination and the files are verified and outside any engine call: it writes the record, its files with their selection and the import's operation (`running`, the request JSON, the fingerprint) in one transaction. Of two concurrent imports of one hash exactly one commits; the other is `ErrDuplicate` naming the existing record (`library.Problem` → `torrent.duplicate`). A reused operation ID is `ErrOperationExists`; when `SameRequest`, answer from `lib.Operation` with the stored status (api-contract), never through `Problem`, which gives `internal.unexpected`. After the engine add, `FinishOperation` with the outcome. Write no payload row at import: no row already means "verify everything", which is the 009 note's "initially none" (029 writes `clean = 0` before the first payload write). `library.outcomeUnknown` is not a success: the operation fails with it, and 029's start decides what was stored. `AddTorrent` refuses (`ErrInvalid`) infohashes that are not the hashes of `Info` and file paths that are not relative to the save path, `/`-separated and clean (the torrent's folder as their first element): refuse a torrent's repeated, NUL, unsafe or too many paths with a code of your own first (019/020), so `ErrInvalid` stays a bug.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what changed, exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
