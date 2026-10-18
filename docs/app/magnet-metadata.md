# Magnet metadata: fetched without payload, confirmed into the chosen folder

**Status:** `implemented` (prototype packages and engine tests; the product does not link them before [018](../../product/tasks/018-torrent-engine-adapter.md))  
**Requirements:** `ADD-02`, `ADD-04`, `ADD-07`  
**Related tasks:** [011](../../product/tasks/011-magnet-metadata-storage-spike.md); next [018](../../product/tasks/018-torrent-engine-adapter.md), [019](../../product/tasks/019-metainfo-parser-file-tree.md), [020](../../product/tasks/020-destination-filesystem-service.md), [021](../../product/tasks/021-full-hash-deduplication.md), [022](../../product/tasks/022-pending-import-coordinator.md), [023](../../product/tasks/023-durable-import-commit.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md)  
**See also:** [ADR magnet-pending-storage](../decisions/magnet-pending-storage.md), [engine storage](engine-storage.md), [transfer switches](transfer-switches.md), [test torrents and swarm](../tooling/test-torrents.md)  
**Last verified version:** branch `task/011-magnet-metadata`  
**Last updated:** 2026-10-18

## Goal and behavior

A magnet import has three moments. Each guarantees something:

| Moment | Call | Guarantee |
|---|---|---|
| Waiting for metadata | `pendingmagnet.Fetch(ctx, client, Request{InfoHash, Peers, Wait})` | Bounded by `Wait` and the context. No payload either way, no file anywhere. The metadata is hashed against the requested infohash. A cancel always wins. |
| The dialog is open | `*Pending`: `Info()`, `InfoBytes()` | The engine torrent stays connected and moves nothing; its storage (`filestore.Deferred`) refuses and counts every piece operation. |
| The user decides | `Confirm(ctx, Final{Storage, Download, Upload})` or `Cancel()` | Confirm refuses a folder that already holds a path of the torrent, then attaches the chosen folder's guarded storage in place: no `Drop`, no second add, the connections stay. A refused folder leaves the import waiting. Cancel drops only this import's torrent. |

The errors are:

| Error | When |
|---|---|
| `ErrInvalidWait`, `ErrInvalidSource` | The request cannot work. Returned before any engine call. |
| `ErrDuplicate` | The infohash is already in the client. The other holder's torrent is never dropped. |
| `ErrClientClosed`, `ErrTorrentClosed` | The client was shut down, or another holder dropped the torrent. |
| `ErrNoMetadata` | The wait ended with no metadata. It wraps `DeadlineExceeded`. |
| `ErrInfoMismatch`, `ErrBadInfo`, `ErrV2Only`, `ErrInfoReplaced` | The received metadata is refused (`ErrInfoReplaced`: another add path replaced the bytes of a torrent whose info was already set, finding 31). |
| `filestore.ErrNoFinalStorage`, `filestore.ErrPathExists`, the guard's errors | `Confirm` got no storage, or a folder that already holds a path of the torrent, or one the guard refuses (a symlinked torrent folder, a changed folder). The import keeps waiting. |
| `ErrConfirmed`, `ErrCanceled` | A call came after the decision. |

**Known residual (finding 33):** the pinned engine applies a peer's metadata before `Fetch` verifies
it. v2-only info under a v1 infohash panics an engine goroutine, which ends the application, and a
16 MiB info of tiny files costs about 600 MiB before it can be refused. A peer can make such bytes
match the btih, so the hash check is not a defence against it. 018/022 close it before magnets reach
the product ([ADR](../decisions/magnet-pending-storage.md)).

## Architecture analysis before the change

- **Layer:** the engine adapter side (`internal/torrentengine/...`). The product binary does not link
  it; the isolation step keeps anacrolix out until 018.
- **No policy here.** The pendingImportID, the queue per infohash, opening an existing record on
  `ErrDuplicate`, the dialog and the durable record are 022/023.
- **Reused:** the guarded storage and marks of 009 (`filestore.NewTorrentStorage`, `Refusing`,
  `Layout`, `ErrUnsafePath`), the switches of 010, and the swarm and fixtures of 006.
- **Added to the swarm:** `PeerInfos` (the trusted entries `Connect` gives) and `Peer.Lie` (a peer
  that serves another torrent's info, finding 11). The three tests that used to build a liar by hand
  now use `Lie`.

## Contracts and data

- `filestore.Deferred`:
  - `NewDeferred()`, `OpenTorrent` (called by the engine), `Attach(ctx, final)`, `Touched()`;
  - errors `ErrNotAttached`, `ErrAttached`, `ErrAttaching`, `ErrNotOpened`, `ErrDeferredClosed`,
    `ErrNoFinalStorage`; a final storage that offers more than `Piece` and `Close`, or no `Piece`,
    is refused and closed, and so is one opened for a torrent the engine closed meanwhile.
- `filestore.TorrentStorage.CheckNew(info)` and `ErrPathExists`; `filestore.Layout` keeps a folder
  torrent's files strictly inside its folder.
- `pendingmagnet`: `Request`, `Fetch`, `Pending` (`Info`, `InfoBytes`, `Touched`, `Confirm`,
  `Cancel`) and `Final` (its `Storage` is a `*filestore.TorrentStorage`).
- No DTO crosses the bridge. 022 maps the errors to `AppError` codes.

## Scenarios and TDD

| Scenario | Test | Level |
|---|---|---|
| `Deferred` refuses and counts until attached; delegates after; a failed attach stays detached; no attach before the metadata, after the engine closed it, without a storage, or of a storage it cannot pass on; an attach in progress blocks neither the close nor a second attach | `filestore/deferred_test.go` | unit (the inner storage is a recording stand-in, `innerStorage`) |
| `CheckNew`: a user's file at a torrent path, a file where the torrent needs a folder, other files only, after `Stop`; `Layout` refuses paths that leave the torrent's folder | `filestore/guard_hardening_test.go`, `guard_test.go` | unit, real folders |
| Verification: match, mismatch, truncated v2 btih, unreadable, v2-only, bytes that are not the applied info; a request that cannot work never reaches the engine | `pendingmagnet/verify_test.go` | unit |
| Metadata verified; a 2 s dialog with no payload, no touch, no file (`v1-single`, `v1-tree`, `hybrid-tree`) | `TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload` | real engine |
| The limit; a cancel; a cancel at the moment of `GotInfo` (20 runs, both outcomes) | `TestFetchWithNoPeerEndsAtItsLimit`, `TestACanceledFetchReturnsTheContextErrorAndLeavesNothing`, `TestACancelAsTheMetadataArrivesLeavesNothing` | real engine |
| A liar's metadata | `TestFetchRefusesMetadataThatDoesNotMatchTheInfohash` | real engine |
| An existing torrent still downloading is left alone | `TestFetchOfAKnownInfohashLeavesTheExistingTorrentAlone` | real engine |
| Shutdown during the wait, before a fetch, before a confirm; a torrent dropped by another holder | `TestShutdownEndsAFetchAndRefusesLaterCalls`, `TestATorrentClosedByAnotherHolderEndsTheImport` | real engine |
| The files land exactly in the chosen folder after a missing storage and two refused folders (a user's file at a torrent path, `ErrPathExists`, kept intact; a symlinked torrent folder, `ErrUnsafePath`) | `TestConfirmPutsTheFilesExactlyInTheChosenFolder` | real engine |
| Parallel decisions; the switches; calls after the end | `TestConfirmAndCancelRaceToExactlyOneOutcome`, `TestConfirmAppliesTheSwitches`, `TestCallsAfterTheEndAreRefused` | real engine |
| Findings 31, 32, 33 | `TestPinnedEngineKeepsThePeersInfoWhenSetInfoBytesComesLate`, `TestPinnedEngineAddsATorrentToAClosedClient`, `TestPinnedEnginePanicsOnV2OnlyInfoUnderAV1Infohash` (child process) | real engine |

RED/GREEN commands and the measurements are in the
[report](../verification/011-magnet-metadata-storage-spike.md).

## Implementation and reuse

- `internal/torrentengine/filestore/deferred.go`: the storage. A mutex orders `OpenTorrent`,
  `Close` and each step of `Attach`; the final storage is opened without it, so a stalled volume
  cannot hold up the engine's close. The attached storage is read without the lock on every piece
  access.
- `internal/torrentengine/filestore/safepath.go`: `CheckNew`.
- `internal/torrentengine/pendingmagnet`:
  - `fetch.go`: the request, the add, the wait and its fixed classification;
  - `verify.go`: the pure hash and format check;
  - `pending.go`: the state and the decision.
- No goroutine of its own; every call returns when its condition holds or its context ends.

## Checks and 100% coverage

`go tool devtool check`: `pendingmagnet` and `deferred.go` are covered by their tests; the swarm
additions by the swarm's own tests. The package's engine tests take about 20 s with `-race`.

## Refactoring and debt check

- The liar setup exists once (`swarm.Peer.Lie`), used by the 007 test, finding 31 and 011.
- `swarm.Connect` builds on `PeerInfos`; there is no second loop.
- `childDir` holds the child-process folder rule once, for findings 9 and 33.
- Open for later tasks, not debt: finding 33 (018/022), the coordinator and its expiry (022), the
  durable record (023), the folder dialog's collision report (020), trackers and DHT (022/026).

## Usage and recovery

- **Start an import:** `Fetch` with the magnet's peers (and, in 022, its trackers) and a bounded wait.
  Show `Info()` in the dialog.
- **The user chooses a folder:** `Confirm` with that folder's guarded storage. On an error, show it
  and let the user choose again: the import is still waiting. Stop the refused folder's
  `TorrentStorage`; a refusal after the guard's checks can leave the torrent's zero-length files
  there. A successful attach creates them at once, even with `Download=false`. Check that the
  returned torrent is not `Closed()` before recording the import (a holder that drops it at the
  same moment can still win).
- **The user cancels, the app quits, or the dialog expires:** `Cancel`. Shutdown needs no extra step:
  the engine closes the torrent and the deferred storage refuses any late attach.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-18 | Created in task 011 | [report 011](../verification/011-magnet-metadata-storage-spike.md), [ADR](../decisions/magnet-pending-storage.md) |
