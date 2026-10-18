# A magnet's metadata without payload, then the chosen folder in place

**Status:** `accepted` (engine part; the import coordinator is [022](../../product/tasks/022-pending-import-coordinator.md), the durable record [023](../../product/tasks/023-durable-import-commit.md))  
**Date:** 2026-10-18  
**Task:** [011](../../product/tasks/011-magnet-metadata-storage-spike.md)  
**Register item:** O7 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `ADD-02`, `ADD-04`, `ADD-07`

## Question

A magnet link has no file list. The confirmation dialog (`ADD-04`, `ADD-07`) needs the real metadata
first, and before the user confirms, no file data may be downloaded or uploaded. After the user
confirms, the files must land exactly in the folder they chose. Several facts make this hard:

- The pinned engine (anacrolix/torrent v1.61.0) opens a torrent's storage the moment its metadata
  arrives.
- It takes whatever info dictionary a peer sends for a v1 infohash (finding 11) and applies it before
  any caller can look at it. The engine's file storage creates that dictionary's zero-length files at
  once, so a liar can name up to 16 MiB of entries.
- `MergeSpec` does not change a torrent's storage ([engine-lifecycle](engine-lifecycle.md)).
- A torrent added with `new=false` belongs to someone else and must not be dropped.

Cancelling must leave existing torrents and their data alone. The wait must be bounded and
cancellable, and a repeated add, a double confirmation or a shutdown must be safe.

## Decision

**No temporary folder.** The pending torrent's storage is `filestore.Deferred`:
- The engine opens it when the metadata arrives. It creates nothing.
- Every piece reports "known, not complete" (`Completion{Ok: true}`), so the engine queues no check
  that would read it.
- It refuses every read, write and mark with `ErrNotAttached`, and counts each refusal (`Touched()`).
  The engine only logs a storage error, so that count is what the tests assert.

**Fetch.** `pendingmagnet.Fetch(ctx, client, Request{InfoHash, Peers, Wait})` works in this order:

1. **Refuse a request that cannot work before touching the engine:** a wait that is not positive
   (`ErrInvalidWait`, so no wait is endless), a zero infohash (`ErrInvalidSource`; the engine panics
   on it), or a context that is already done.
2. **Add** with `AddTorrentOpts{InfoHash, Storage: deferred, DisableInitialPieceCheck: true}`.
   - `new=false` gives `ErrDuplicate`, with no `Drop` and no change, because the handle is borrowed.
   - A closed client gives `Drop` and then `ErrClientClosed`: the engine adds to a closed client and
     never closes that torrent (finding 32).
3. **Disallow both directions, then add the peers.**
4. **Wait in one `select`** on `GotInfo`, the context, the wait limit, `t.Closed()` and
   `cl.Closed()`. Then classify in a fixed order:
   1. the caller's cancel;
   2. a closed client (`ErrClientClosed`);
   3. a torrent another holder dropped (`ErrTorrentClosed`);
   4. the limit (`ErrNoMetadata`, which wraps `DeadlineExceeded`).

   **A cancel always wins.** A cancelled fetch never yields metadata, even when the metadata arrived
   at the same moment.
5. **Verify** the info bytes the engine parsed (`t.Metainfo().InfoBytes`):
   - their SHA-1 must equal the *requested* infohash (`ErrInfoMismatch`). Never compare with
     `t.InfoHash()`: a btih that is a truncated v2 hash makes the engine rewrite it;
   - they must parse (`ErrBadInfo`);
   - they must have a v1 part (`ErrV2Only`, the 007 decision);
   - parsed, they must equal the info the engine applied (`t.Info()`, `ErrInfoReplaced`): once the
     info is set, a later `SetInfoBytes` or merge on the same torrent replaces only the bytes
     (finding 31), and the deferred storage opens the folder with the applied info.
6. **Drop on every error after the add.** On success, return a `*Pending` that owns the torrent.

While the dialog is open, the pending torrent keeps its peer connections and moves no payload. It
holds no file.

**Confirm in place.** `Pending.Confirm(ctx, Final{Storage, Download, Upload})` works in this order:

1. **Refuse** a confirmed import (`ErrConfirmed`), a cancelled one (`ErrCanceled`), a done context, a
   closed client or a dropped torrent. A missing storage is `filestore.ErrNoFinalStorage`.
   `Final.Storage` is a `*filestore.TorrentStorage`, so every attach goes through the guard (save
   folder identity, symlinks, HFS+).
2. **Refuse a folder that already holds a path of the torrent** (`TorrentStorage.CheckNew`,
   `filestore.ErrPathExists`) before it is opened. Every piece is cached as not complete, so the
   engine would download over such a file and the user's bytes would be gone. A file where the
   torrent needs a folder is refused with the lookup's error. The check is not atomic with the open:
   a file put in place between the two is not seen.
3. **Attach:** `deferred.Attach` calls the final storage's `OpenTorrent` itself.
   - Its error comes back directly; finding 15 does not apply.
   - The import stays pending, so the user can choose another folder. Examples: a torrent folder
     that is a symlink (`filestore.ErrUnsafePath`), or a folder that changed since its storage was
     made (`ErrRootChanged`).
   - The open runs without the `Deferred`'s lock. It does file work in the user's folder, and the
     engine closes a torrent's storage under its client lock, so a stalled volume must not freeze
     `Drop`, `Client.Close` or the app's quit. A second attach meanwhile is `ErrAttaching`. A torrent
     closed during the open gets nothing attached, and what was opened for it is closed
     (`ErrDeferredClosed`).
   - The engine looks a piece's storage up on every access (`Piece.Storage()` →
     `TorrentImpl.Piece`, `piece.go:79-88`, `storage/wrappers.go:44-51`), so every later read and
     write goes to the final folder with no `Drop` and no second add. Two holders keep a piece for a
     while: a `torrent.Reader` caches its piece reader (`storage.go:22-47`), and the hasher holds a
     piece for one hash (`torrent.go:1263`). Neither exists before the attach: no reader is open,
     and nothing is hashed because every piece is cached as not complete.
   - The engine reads a torrent's optional storage fields (`PieceWithHash`, `Capacity`,
     `NewReader`, `NewPieceReader`) from the `Deferred`, so `Attach` refuses a final storage that
     offers them or lacks `Piece`, and closes it: the guard's rule (`unsupported`). A missing
     storage is `ErrNoFinalStorage`.
   - The engine's file storage creates the zero-length files and their folders in the chosen
     folder at the attach, even with download off (`storage/file-client.go:96-101`). A refused
     attach can leave such files after the guard's checks passed; the caller stops that folder's
     `TorrentStorage`.
4. **Switches:** allow upload if `Final.Upload` (otherwise the global stop of 010 holds). Allow
   download if `Final.Download` ("start after adding"). Download was never allowed while the torrent
   had its metadata, so no cancelled request exists and finding 25 cannot apply.
5. **Hand over:** the torrent now belongs to the caller. A holder that drops the torrent at the same
   moment can still win after the attach: `Confirm` then returns a torrent whose `Closed()` is
   done. Per-infohash serialization (022, 018) keeps any other holder away; the caller checks
   `Closed()` before it records the import.

**Paths.** `filestore.Layout` keeps a folder torrent's files strictly inside its own folder (011
tightened the 009 rule). A hash-valid info with name `x` and the path `../f` would otherwise write
`f` beside the user's other files in the save folder, and a file at the folder's own path would
replace it.

**Known residual: hostile metadata reaches the engine before `verify` (finding 33).** The hash check
runs after `GotInfo`, but the engine has already parsed and applied the peer's info by then, and the
pinned version offers no hook in between. A peer can choose the info and then hand out a magnet
whose btih is its SHA-1, so the bytes pass every hash check.
- **A crash.** v2-only info under a v1 infohash panics inside the engine (`piece request order has
  {} and pending pieces has …`, `TestPinnedEnginePanicsOnV2OnlyInfoUnderAV1Infohash`). Over the
  wire it happens in an engine goroutine with no recover, so the whole application ends, and
  `ErrV2Only` is reached only by bytes the engine survived. The check is sampled
  (`amortize.Try`: the 1st, 2nd, 4th, 8th… call in a process), so a long-running app is hit less
  often, but the result is still a crash. Other sampled engine asserts may exist.
- **Memory.** The security review measured a 16 MiB info of 600 000 one-byte files at about 600 MiB
  of heap and 0.6 s inside `SetInfoBytes` with the client lock held, before it could be refused.
- **Not affected:** 16 other hostile shapes (the `invalid-*` fixtures, huge or overflowing lengths,
  `..` and absolute names, empty paths, 300 000 empty files, a hybrid mismatch) neither panicked nor
  created anything; `Touched()` stayed 0.
- **Closed by 018/022, before the product links the engine:** either the adapter handles BEP 9
  itself (assembles the pieces, checks the size and the SHA-1, then calls `SetInfoBytes`), or an
  engine release fixes the assert and bounds the work. The product binary does not link any of this
  yet (`product isolation`).


**Cancel and later calls.**
- `Cancel` drops the torrent once. A second `Cancel` does nothing. A `Cancel` after `Confirm` is
  `ErrConfirmed` and leaves the torrent running.
- `Confirm` after `Cancel` is `ErrCanceled`.
- `Deferred` remembers that the engine closed it (on `Drop` or shutdown). An `Attach` after that is
  `ErrDeferredClosed`, so the user's folder is never opened for a torrent that is gone.
- One mutex orders `Confirm` and `Cancel`, so exactly one decides.

**Rules for the code that builds on this (018, 019, 020, 022, 023, 029):**
- **Cached completion.** At metadata time the engine cached "not complete" for every piece. That is
  right for a folder that holds none of the torrent's files: `Confirm` refuses any other
  (`ErrPathExists`), and 020 offers only such folders (findings 8 and 13 already require it). Adding data that is already on disk is the restore path
  (029), not a pending import.
- **Ownership across add paths.** While a fetch or a dialog is pending, any other add of the same
  infohash gets `new=false` and the deferred torrent, and its own storage is ignored. This covers a
  `.torrent` import, a restore and a second magnet. So per-infohash serialization (022, 018) covers
  every add path, not only magnets.
- **Peers only after the info is set** for every other add. Once any info is set, `SetInfoBytes`
  returns nil and keeps it (finding 31). 011 never adds a second time; 019/023 add a `.torrent` with
  no peers until `SetInfoBytes` has returned.
- **The facts depend on `IgnoreUnverifiedPieceCompletion` staying false** (otherwise the engine checks
  every piece). 018 keeps it so.
- **The product's wait** (its default, an expiry for a dialog left open, the waiting UI) and magnet
  URI trackers and DHT are 022.
- **Magnet peers are not trusted** (the engine never bans a `Trusted` peer). A mismatch ends the
  whole fetch, and the engine does not ask another peer, so one liar can block every fetch it joins:
  022 retries without the peer that lied, or bans it.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| The engine's file storage in a temporary folder owned by the import, wiped after (the task's first plan, 007's note) | Plain engine storage | A liar's zero-length files are created there (finding 11); the folder needs a bound, a wipe and crash cleanup; the final folder still needs a second torrent | `Deferred` creates nothing at all |
| Fetch into a storage that holds nothing, then `Drop` and re-add with the final storage (the first draft of this plan) | No storage switch inside a live torrent | Two windows where another peer's metadata can set the info before `SetInfoBytes`, which then silently keeps it (finding 31); closing them needs an extra guard on the final storage and a strict call order; peers are found again after every confirmation | The independent review found the windows; confirming in place has none |
| A separate engine client only for metadata | Full isolation | A second listener, a second DHT node, double shutdown work; still a second add | No benefit over a deferred storage |
| `AddTorrentOpt` with `InfoBytes` for the final add | One locked call | Drops the storage error (finding 15); the switches come after the info | Errors must reach the user |
| `filestore.Refusing` as the pending storage | Already exists | A refused `OpenTorrent` makes the engine discard the metadata and ask again: `GotInfo` never fires | Metadata could not be fetched |

## Verified grounds

The full record is in [report 011](../verification/011-magnet-metadata-storage-spike.md): Apple M4,
macOS 27.0.1, Go 1.27.2, anacrolix/torrent v1.61.0, the loopback swarm, classic IO, with and
without `-race`.

- **Stage A (scratch, measured):**
  - 5 runs per fixture with `-race` and 3 without, on `v1-single`, `v1-tree` and `hybrid-tree`.
  - Metadata arrived in 1–6 ms.
  - `Deferred` was touched 0 times; payload was 0 on both sides; protocol bytes 0.5–2 KiB per
    direction.
  - The seeder stayed connected through a 3 s hold.
  - After the attach, the download completed with no new connect in 4–15 ms, with exactly
    `filestore.Layout`'s files.
  - A liar's info failed the hash check 8 of 8, and nothing was written (a liar that serves an
    info whose SHA-1 *is* the btih is finding 33).
  - The scratch test is not committed, so its timings and byte counts cannot be re-run; the
    committed `TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload` and
    `TestConfirmPutsTheFilesExactlyInTheChosenFolder` are the reproducible form of the same claims.
- **Committed tests (`internal/torrentengine/pendingmagnet`, `filestore/deferred_test.go`):** each
  scenario of the plan, at 100% of statements:
  - payload and touches stay 0 through a 2 s dialog;
  - the limit, a cancel, a cancel at the moment of `GotInfo` (20 runs, both outcomes reached);
  - a liar;
  - an existing torrent still downloading;
  - shutdown and a dropped torrent;
  - the chosen folder after two refused ones, one holding a user's file at a torrent path, one
    whose torrent folder is a symlink (`v1-tree`, `hybrid-tree`);
  - an attach in progress blocks neither the engine's close nor a second attach (unit level; the
    engine's close racing a confirm is not reproduced with the real engine);
  - parallel `Confirm`/`Confirm` and `Confirm`/`Cancel`;
  - both switches;
  - calls after the end.
- **Pinned engine findings in `internal/torrentengine/lifecycle_test.go`:**
  - 31: `SetInfoBytes` after a peer's metadata returns nil and keeps the peer's info.
  - 32: `AddTorrentOpt` on a closed client returns a torrent that is never closed.
  - 33: v2-only info under a v1 infohash panics the engine (a child process, 20 of 20 runs).
- **Read in the source, asserted through the scenarios:** the facts listed in the report's session
  plan (storage opened at metadata time, no read with `Ok=true`, no request without download, the
  switches before info, `Drop` freeing every short hash).
- **`not_run`:**
  - existing library *records* (no store before 014/015; 023);
  - the `.app` (owner decision; 022);
  - magnet trackers and DHT, and a dialog left open for minutes on a real network (022, 026).

## Consequences

- **018** moves `Deferred` and `pendingmagnet` into the adapter, checks the client before every add
  (finding 32), keeps `IgnoreUnverifiedPieceCompletion` false, serializes adds per infohash across
  every add path, and closes finding 33 before a magnet reaches the product.
- **019/023** add a `.torrent` with no peers until its info is set (finding 31).
- **020** offers the user a folder that holds none of the torrent's paths; `Confirm` refuses one
  that does (`ErrPathExists`), so a mistake in 020 cannot overwrite a file.
- **021** gets the v1/v2 identities from the verified info.
- **022** builds the coordinator:
  - a pendingImportID per `Pending`;
  - per-infohash serialization;
  - `ErrDuplicate` opens the existing record;
  - the wait default and a dialog expiry;
  - magnet URI parsing with trackers and DHT, with peers that are never trusted, and a retry
    without a peer that lied;
  - with 018, finding 33;
  - the rejection of a `btmh`-only magnet;
  - the `.app` scenario.
- **029** restores existing data through the add path, never through a pending import.
- **Recheck** when the engine changes: finding 11 (`TestPinnedEngineAcceptsInfoBytesThatDoNotMatchTheInfohash`),
  31, 32 and 33 (their pinned tests), and the piece lookup on every access. If a release caches
  `PieceImpl`, `TestConfirmPutsTheFilesExactlyInTheChosenFolder` fails.
