# 011 — Check magnet metadata retrieval and the final file folder

**Status:** `done`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [009](009-storage-and-completion-spike.md)  
**Requirements:** `ADD-02`, `ADD-04`, `ADD-07`

## Outcome

Prove that magnet metadata can be obtained without transferring file data before confirmation, and that after confirmation the folder actually selected is used.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-02`, `ADD-04`, `ADD-07`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Create a separate context for the pending import and temporary storage owned by it; forbid transfer of file data from the start.
2. Wait for metadata with cancellation support and a bounded wait time; record the created file objects and network counters.
3. After confirmation, use a different `saveRoot` through a verified removal and re-add or another supported strategy. Do not try to change `Storage` through `MergeSpec`.
4. Cancel the import before and after the metadata is obtained. Do not call `Drop` on the object of an already existing torrent that this import does not manage.
5. Describe cleanup of only our own temporary objects and handling of calls that arrive after cancellation.

## Artifacts and change locations

**Actual locations:** `docs/decisions/magnet-pending-storage.md` (the register's name for O7), `docs/verification/011-magnet-metadata-storage-spike.md`, `docs/app/magnet-metadata.md`, `internal/torrentengine/pendingmagnet`, `internal/torrentengine/filestore/deferred.go`.
**Evidence of completion:** the record `docs/verification/011-magnet-metadata-storage-spike.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] Before explicit import confirmation, file data is neither downloaded nor uploaded.
- [x] After confirmation, physical files are created exactly in the selected directory.
- [x] Cancelling the import does not change existing library records or their data. (Data: proven at the engine boundary; records have no store before 014/015 and are handed to 023.)
- [x] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed. (The `.app` run is `not_run` by owner decision, owner 022.)

## Checks

- Do not connect any other client, wait for the time limit, and separately cancel the import simultaneously with the `GotInfo` signal; in all cases the tasks must finish and file data must not be transferred.
- Repeat adding with `new=false`, double confirmation and program shutdown; the existing torrent must remain intact, and the new record must not be duplicated.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

A pending import has no right to remove the object of an existing torrent that it does not manage. `MergeSpec` does not change `Storage`; correctness of the final folder is proven by checking the physical files, not only by the field value in the state snapshot.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): `swarm.Peer.AddInfohash` adds a torrent known only by its infohash (as a magnet) and `swarm.WaitInfo` bounds the wait for metadata; a BEP 52 single-file torrent is stored inside a folder of its name on v1.61.0.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): info bytes that do not match a v1 infohash are accepted, also over the wire from a lying peer for a magnet, and that metadata's zero-length files are created as soon as it arrives (finding 11). The engine consumes peer metadata itself, so no application code can check it first: fetch the metadata into storage the application owns (never the user's save folder) and wipe it afterwards, hash the received info bytes against the requested infohash, and only then add the verified metainfo with the real folder. (Superseded in 011: the storage is a `filestore.Deferred` that creates nothing, so there is nothing to wipe or bound, and the real folder is attached in place instead of a second add.) No payload is downloaded before the check. The engine accepts up to 16 MiB of metadata, enough for hundreds of thousands of zero-length entries: bound what the isolated storage may create.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): the final folder is used through `filestore.NewTorrentStorage(saveRoot, completion, session, logger)` and `AddTorrentOpts{InfoHash, Storage}` followed by `SetInfoBytes` (never `AddTorrentOpt` alone: it drops the storage error, finding 15). `filestore.Layout` says where every file will be. Metadata fetched into temporary storage must still be hashed against the infohash before it reaches the final folder (finding 11); the temporary storage is not the user's folder and must not be a `TorrentStorage` of it. (Resolved in 011: `filestore.Deferred` until `Pending.Confirm` attaches the folder's `TorrentStorage`.) A save folder on HFS+ is refused with `ErrFileSystem` (finding 21).

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

## Execution result

**Date:** 2026-10-18. **Status:** `done`.

**What changed:**
- `filestore.Deferred` (`internal/torrentengine/filestore/deferred.go`): the storage of a magnet waiting for confirmation. It creates nothing, reports every piece as known and not complete, refuses and counts every piece operation, and `Attach` opens the chosen folder's storage in place, outside its lock.
- `internal/torrentengine/pendingmagnet`: `Fetch` (bounded, cancellable, cancel wins; the metadata hashed against the requested infohash and compared with the applied info), `Pending.Confirm` (refuses a folder that already holds a path of the torrent, then attaches in place: no `Drop`, no second add) and `Cancel` (drops only its own torrent).
- `filestore`: `TorrentStorage.CheckNew` and `ErrPathExists`; `Layout` keeps a folder torrent's files strictly inside its folder.
- Swarm: `PeerInfos`, `Peer.Lie(ctx, …)`. Engine findings 31, 32 and 33 are pinned in `lifecycle_test.go`.
- ADR [magnet-pending-storage](../../docs/decisions/magnet-pending-storage.md) (O7 accepted for the engine part), feature doc [magnet-metadata](../../docs/app/magnet-metadata.md), notes for 018–023 and 029.

**Checks:** `go test -race -count=1 ./internal/torrentengine/...` `pass` twice, without `-race` `pass` once; `go tool devtool check` `GATE PASSED`. Not run: the `.app` scenario (owner decision, 022), library records (023), trackers and DHT on a real network (022/026). Evidence: [report 011](../../docs/verification/011-magnet-metadata-storage-spike.md).

**Follow-up (2026-10-18):** findings 33–43 are closed: the engine never assembles a peer's metadata (`pendingmagnet.Gate`), and it parses only bytes `infocheck.CheckBytes` admitted; see the report's follow-up section.

**Limitations (as of the handoff, closed by the follow-up):** finding 33 was a known residual. The pinned engine applies a peer's metadata before it can be verified; v2-only info under a v1 infohash panics the engine and ends the application, and a 16 MiB info costs about 600 MiB. The product links none of this code; 018/022 must close it before magnets ship.

**Next task:** the README's next tasks: [012](012-macos-trash-and-path-spike.md), [013](013-transfer-policy-state-machine.md), [014](014-sqlite-schema-migrations.md), [017](017-local-diagnostics-privacy.md), [035](035-react-bridge-state-resync.md).
