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
| Waiting for metadata | `gate.Fetch(ctx, client, Request{InfoHash, Peers, Wait, Limits})` on a client built with `NewGate(cfg)` | Bounded by `Wait` and the context. No payload either way, no file anywhere. The gate collects the metadata pieces and the engine never sees them; the bytes are hashed against the requested infohash and checked by `infocheck.CheckBytes` before the engine parses them. A cancel always wins. |
| The dialog is open | `*Pending`: `Info()`, `InfoBytes()` | The engine torrent stays connected and moves nothing; its storage (`filestore.Deferred`) refuses and counts every piece operation. |
| The user decides | `Confirm(ctx, Final{Storage, Download, Upload})` or `Cancel()` | Confirm refuses a folder that already holds a path of the torrent, then attaches the chosen folder's guarded storage in place: no `Drop`, no second add, the connections stay. A refused folder leaves the import waiting. Cancel drops only this import's torrent. |

The errors are:

| Error | When |
|---|---|
| `ErrInvalidWait`, `ErrInvalidSource`, `infocheck.ErrInvalidLimits` | The request cannot work. Returned before any engine call. |
| `ErrDuplicate` | The infohash is already in the client. The other holder's torrent is never dropped. |
| `ErrClientClosed`, `ErrTorrentClosed` | The client was shut down, or another holder dropped the torrent. |
| `ErrNoMetadata` | The wait ended with no metadata. It wraps `DeadlineExceeded`. |
| `infocheck.ErrMalformed`, `ErrV2Only`, `ErrInconsistentInfo`, `ErrInfoTooLarge` | The assembled metadata was refused before the engine parsed it: not well-formed or too costly to decode, v2-only, parts that disagree or a file tree node the engine would trust, past the engine's index, or more bytes, files or pieces than `Limits` (findings 33–43 crash or stall the pinned engine, several before any check). Like `ErrInfoMismatch`, it may come from one liar on a legitimate magnet: 022 retries with a new `Fetch` without that peer. |
| `ErrInfoMismatch`, `ErrInfoReplaced` | The metadata is not the requested torrent's, or a piece cannot belong to it (`ErrInfoMismatch`); another add path set the torrent's info first (`ErrInfoReplaced`, finding 31). |
| `filestore.ErrNoFinalStorage`, `filestore.ErrPathExists`, the guard's errors | `Confirm` got no storage, or a folder that already holds a path of the torrent, or one the guard refuses (a symlinked torrent folder, a changed folder). The import keeps waiting. |
| `ErrConfirmed`, `ErrCanceled` | A call came after the decision. |

**Hostile metadata (findings 33–43):** the pinned engine assembles, parses and applies a peer's
metadata as soon as its pieces arrive, before `Fetch` can look at it, and many shapes crash or stall
it, several before any storage hook; a few other peer messages crash it outright. A peer can make
its bytes match the btih. So the client is built with `pendingmagnet.NewGate(cfg)`, whose callbacks
run before the engine handles each message: the gate collects the metadata pieces itself (the engine
never assembles an info from peers), checks every extended message against `infocheck.CheckMessage`,
clears a metadata size the engine cannot hold, turns a request for an impossible piece into a reject,
and turns v2 hash messages into keep-alives. `Fetch` then hashes the assembled bytes against the
btih, runs `infocheck.CheckBytes` and only then calls `SetInfoBytes`. The engine parses our vetted
bytes once; the cost is in the [ADR](../decisions/magnet-pending-storage.md).

## Architecture analysis before the change

- **Layer:** the engine adapter side (`internal/torrentengine/...`). The product binary does not link
  it; the isolation step keeps anacrolix out until 018.
- **No policy here.** The pendingImportID, the queue per infohash, opening an existing record on
  `ErrDuplicate`, the dialog and the durable record are 022/023.
- **Reused:** the guarded storage and marks of 009 (`filestore.NewTorrentStorage`, `Refusing`,
  `Layout`, `ErrUnsafePath`), the switches of 010, and the swarm and fixtures of 006.
- **Added to the swarm:** `PeerInfos` (the trusted entries `Connect` gives), `Peer.Lie` (a peer
  that serves another torrent's info, finding 11) and `MetadataPeer` (a peer that speaks only the
  handshake, BEP 10 and BEP 9 and serves fixed info bytes: an engine peer cannot serve info that
  panics the engine). The three tests that used to build a liar by hand now use `Lie`.
- **Added to the fixtures:** `fixture.HostileInfos()`, the measured shapes of findings 33–37,
  built from the catalog's specs (`spec.dicts`).

## Contracts and data

- `filestore.Deferred`:
  - `NewDeferred(admit)`, `OpenTorrent` (called by the engine; runs `admit` first), `Attach(ctx,
    final)`, `Touched()`; a refused info is the engine's `SetInfoBytes` error;
  - errors `ErrNotAttached`, `ErrAttached`, `ErrAttaching`, `ErrNotOpened`, `ErrDeferredClosed`,
    `ErrNoFinalStorage`; a final storage that offers more than `Piece` and `Close`, or no `Piece`,
    is refused and closed, and so is one opened for a torrent the engine closed meanwhile.
- `filestore.TorrentStorage.CheckNew(info)` and `ErrPathExists`; `filestore.Layout` keeps a folder
  torrent's files strictly inside its folder.
- `infocheck`: `CheckBytes(b, Limits)` (the entry point for raw bytes: size, scan, parse, `Check`),
  `CheckMessage(payload)` (the same scan for a peer's extended message), `Check(info, Limits)`,
  `Limits{Files, Pieces, MetadataBytes}` with `Validate()`, `ErrMalformed`, `ErrV2Only`,
  `ErrInconsistentInfo`, `ErrInfoTooLarge`, `ErrInvalidLimits`. 019 reuses it for `.torrent` files.
- `pendingmagnet`: `NewGate(cfg)` → `*Gate` (installs the callbacks; must be last to set them),
  `Gate.Fetch`, `Request` (with `Limits`), `PeerError` (`Peers` IPs, `Err`), `Pending` (`Info`,
  `InfoBytes`, `Touched`, `Confirm`, `Cancel`) and `Final` (its `Storage` is a
  `*filestore.TorrentStorage`).
- No DTO crosses the bridge. 022 maps the errors to `AppError` codes.

## Scenarios and TDD

| Scenario | Test | Level |
|---|---|---|
| `Deferred` refuses and counts until attached; delegates after; a failed attach stays detached; no attach before the metadata, after the engine closed it, without a storage, or of a storage it cannot pass on; an attach in progress blocks neither the close nor a second attach; a refused info is not recorded and the first refusal is kept | `filestore/deferred_test.go` | unit (the inner storage is a recording stand-in, `innerStorage`) |
| `CheckNew`: a user's file at a torrent path, a file where the torrent needs a folder, other files only, after `Stop`; `Layout` refuses paths that leave the torrent's folder | `filestore/guard_hardening_test.go`, `guard_test.go` | unit, real folders |
| The rule: every fixture the engine handles is admitted from its bytes; every shape measured to crash or stall the engine or to leave it two disagreeing views is refused; each file tree node the engine would trust; the scan's syntax, depth and tree-work bounds; the engine's index bounds; a sum overflow; the limits | `infocheck/check_test.go`, `infocheck/bytes_test.go` | unit |
| For any bytes `CheckBytes` never panics, and bytes it admits never panic the engine applied through a `Deferred` and read back | `infocheck/fuzz_test.go` (`FuzzCheckBytes`; seeds run in every gate, the fuzz budget is in the report) | fuzz, real engine |
| The gate: pieces in any order, the first copy kept, every data message rewritten into a reject, requests and other extensions left alone; a piece that does not fit or a size over the limit ends the assembly | `pendingmagnet/gate_test.go` | unit |
| Verification: match, mismatch, truncated v2 btih, unreadable, bytes that are not the applied info; a request that cannot work never reaches the engine | `pendingmagnet/verify_test.go` | unit |
| Metadata verified; a 2 s dialog with no payload, no touch, no file (`v1-single`, `v1-tree`, `hybrid-tree`) | `TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload` | real engine |
| The limit; a cancel; a cancel at the moment of `GotInfo` (20 runs, both outcomes) | `TestFetchWithNoPeerEndsAtItsLimit`, `TestACanceledFetchReturnsTheContextErrorAndLeavesNothing`, `TestACancelAsTheMetadataArrivesLeavesNothing` | real engine |
| A liar's metadata | `TestFetchRefusesMetadataThatDoesNotMatchTheInfohash` | real engine |
| A hostile peer serves each info shape of findings 33–39 under its own SHA-1; an info over the file limit; metadata over the size limit | `TestFetchRefusesHostileMetadataBeforeTheEngineParsesIt` (`swarm.MetadataPeer`) | real engine, over the wire |
| A peer serves the metadata but also sends a crashing message (findings 41–43); a peer that lies about one piece is named | `TestFetchSurvivesAPeersCrashingMessages`, `TestFetchNamesThePeersThatSentRefusedMetadata` | real engine, over the wire |
| An existing torrent still downloading is left alone | `TestFetchOfAKnownInfohashLeavesTheExistingTorrentAlone` | real engine |
| Shutdown during the wait, before a fetch, before a confirm; a torrent dropped by another holder | `TestShutdownEndsAFetchAndRefusesLaterCalls`, `TestATorrentClosedByAnotherHolderEndsTheImport` | real engine |
| The files land exactly in the chosen folder after a missing storage and two refused folders (a user's file at a torrent path, `ErrPathExists`, kept intact; a symlinked torrent folder, `ErrUnsafePath`) | `TestConfirmPutsTheFilesExactlyInTheChosenFolder` | real engine |
| Parallel decisions; the switches; calls after the end | `TestConfirmAndCancelRaceToExactlyOneOutcome`, `TestConfirmAppliesTheSwitches`, `TestCallsAfterTheEndAreRefused` | real engine |
| Findings 31, 32, 33–43 | `TestPinnedEngineKeepsThePeersInfoWhenSetInfoBytesComesLate`, `TestPinnedEngineAddsATorrentToAClosedClient`, `TestPinnedEnginePanicsOnHostileInfo`, `TestPinnedEngineDecodesAFileTreeInQuadraticWork`, `TestPinnedEnginePanicsPlanningPastItsRequestIndex`, `TestPinnedEnginePanicsOnAPeersMessage` (child processes) | real engine |

RED/GREEN commands and the measurements are in the
[report](../verification/011-magnet-metadata-storage-spike.md).

## Implementation and reuse

- `internal/torrentengine/filestore/deferred.go`: the storage. A mutex orders `OpenTorrent`,
  `Close` and each step of `Attach`; the final storage is opened without it, so a stalled volume
  cannot hold up the engine's close. The attached storage is read without the lock on every piece
  access.
- `internal/torrentengine/filestore/safepath.go`: `CheckNew`.
- `internal/torrentengine/infocheck/check.go`, `bytes.go`: the pure rule for a parsed info and the entry point for raw bytes (the iterative scan).
- `internal/torrentengine/pendingmagnet/gate.go`: the engine callback (a thin adapter over `onMessage`) and the assembly; `verify.go`: `vet` and `sameInfo`.
- `internal/torrentengine/pendingmagnet`:
  - `fetch.go`: the request, the add, the wait and its fixed classification;
  - `verify.go`: the pure hash and format check;
  - `pending.go`: the state and the decision.
- No goroutine of its own; every call returns when its condition holds or its context ends.

## Checks and 100% coverage

`go tool devtool check`: `pendingmagnet`, `infocheck` and `deferred.go` are covered by their tests;
the swarm and fixture additions by their own tests. The package's engine tests take about 20 s with `-race`.

## Refactoring and debt check

- The liar setup exists once (`swarm.Peer.Lie`), used by the 007 test, finding 31 and 011.
- `swarm.Connect` builds on `PeerInfos`; there is no second loop.
- `childDir` holds the child-process folder rule once, for findings 9 and 33–43; `runChild` and `childPeer` serve the hostile children.
- The hostile shapes exist once (`fixture.HostileInfos`), used by `infocheck`, the engine tests and
  the wire test; `fixture` builds them from its specs through `spec.dicts`, not a second generator.
- Open for later tasks, not debt: the product limits (019/022), the coordinator and its expiry (022), the
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
| 2026-10-18 | Hostile metadata never reaches the engine (findings 33–43): the gate over three engine callbacks, `infocheck.CheckBytes`/`CheckMessage`, `FuzzCheckBytes`, `Request.Limits` | [report 011, follow-up](../verification/011-magnet-metadata-storage-spike.md#follow-up-hostile-metadata-findings-33-to-43) |
