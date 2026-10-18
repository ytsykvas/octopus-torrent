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

**The gate.** The engine never assembles a peer's metadata (see "Hostile metadata" below).
`pendingmagnet.NewGate(cfg)` installs a callback in the client's configuration
(`Callbacks.PeerConnReadExtensionMessage`, which runs before the engine dispatches each extended
message). Each `ut_metadata` data message gives its piece to the assembly of the torrent `Fetch`
waits for, then is rewritten in place into a reject, which the engine ignores. One gate serves one
client; 018 builds the product client with it.

**Fetch.** `gate.Fetch(ctx, client, Request{InfoHash, Peers, Wait, Limits})` works in this order:

1. **Refuse a request that cannot work before touching the engine:** a wait that is not positive
   (`ErrInvalidWait`, so no wait is endless), a zero infohash (`ErrInvalidSource`; the engine panics
   on it), limits that are not positive (`infocheck.ErrInvalidLimits`), or a context that is already
   done.
2. **Add** with `AddTorrentOpts{InfoHash, Storage: deferred, DisableInitialPieceCheck: true}` and
   register the torrent with the gate.
   - `new=false` gives `ErrDuplicate`, with no `Drop` and no change, because the handle is borrowed.
   - A closed client gives `Drop` and then `ErrClientClosed`: the engine adds to a closed client and
     never closes that torrent (finding 32).
3. **Disallow both directions, then add the peers.**
4. **Wait in one `select`** on the assembly, the context, the wait limit, `t.Closed()` and
   `cl.Closed()`. Then classify in a fixed order:
   1. the caller's cancel;
   2. a closed client (`ErrClientClosed`);
   3. a torrent another holder dropped (`ErrTorrentClosed`);
   4. the limit (`ErrNoMetadata`, which wraps `DeadlineExceeded`).

   **A cancel always wins.** A cancelled fetch never yields metadata, even when the metadata arrived
   at the same moment.
5. **Take the assembly.** A size over `Limits.MetadataBytes` was refused before any buffer existed
   (`infocheck.ErrInfoTooLarge`); a piece that cannot belong to the metadata (out of range, the wrong
   length, a changed size) is `ErrInfoMismatch`.
6. **Vet the bytes before the engine sees them** (`vet`):
   - their SHA-1 must equal the *requested* infohash (`ErrInfoMismatch`). Never compare with
     `t.InfoHash()`: a btih that is a truncated v2 hash makes the engine rewrite it;
   - `infocheck.CheckBytes` must admit them (`ErrMalformed`, `ErrV2Only`, `ErrInconsistentInfo`,
     `ErrInfoTooLarge`).
7. **Apply:** `t.SetInfoBytes(bytes)`; the deferred storage admits the info again with
   `infocheck.Check`, the one guard no add path can skip. The applied info must equal the vetted one
   (`t.Info()`, `ErrInfoReplaced`): once an info is set, a later `SetInfoBytes` on the same torrent
   replaces only the bytes (finding 31).
8. **Drop on every error after the add.** On success, return a `*Pending` that owns the torrent.

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

**Hostile metadata never reaches the engine (findings 33–43).** A peer can choose an info and then
hand out a magnet whose btih is its SHA-1, so every hash check passes. The pinned engine parses and
applies a peer's metadata as soon as its pieces are in, before any caller can look at it, and these
shapes crash or stall it (`TestPinnedEnginePanicsOnHostileInfo`, a child process per shape; over the
wire the crash is in an engine goroutine with no recover and ends the application):
- **33:** v2-only info under a v1 infohash: a sampled assert (`piece request order has {} and
  pending pieces has …`; `amortize.Try` runs the 1st, 2nd, 4th… check in a process).
- **34:** `meta version 2` info whose file tree needs more pieces than `pieces` holds: `makePieces`
  slices past its end (`slice bounds out of range`), every time. `Info.HasV1()` counts any `length`
  or `pieces`, `HasV2()` is just `meta version == 2`, and with it the engine takes the files and the
  piece count from the file tree but the piece hashes from `pieces`; `validateInfo` skips the piece
  count whenever `HasV2()`.
- **35:** a file tree node that the engine reads without checking: a pieces root that is not 32
  bytes (`FileTree.PiecesRootAsByteArray`, inside `Info.UpvertedFilesIter`), or file properties on
  a folder (`Info.NumPieces` counts every node's length, folders included, while the files skip a
  folder's properties: a pad and one more piece make the counts agree and `onSetInfo` then finds a
  piece in no file, `mustGetOnlyFile`).
- **36:** `meta version 2` with no piece length: `validateInfo` divides by it (`integer divide by
  zero`) *before* the storage is opened.
- **37:** an unknown key nested 4M levels (8 MB, under the engine's 16 MiB metadata cap): the
  recursive decoder overflows the stack (`fatal error: stack overflow`, no recover possible) before
  anything else runs.
- **38:** a deeply nested file tree: `FileTree.UnmarshalBencode` parses every folder again for each
  folder above it, under the client lock. The security review measured 100 KB at 18–30 s and
  17 GiB allocated, freezing every torrent; `TestPinnedEngineDecodesAFileTreeInQuadraticWork` pins
  the growth.
- **39:** a piece length of 2^32 or more: applied without complaint, then the request planner
  divides by a chunk count its uint32 index truncated to zero once a peer has pieces
  (`TestPinnedEnginePanicsPlanningPastItsRequestIndex`), after Confirm and again on every restart.
- **40:** a `meta version 2` info with many small files: the engine walks the whole file tree once
  per piece per connected peer when it applies the info, under the client lock (`NumPieces` in
  `selectivePieceAvailabilityFromPeers`); the review measured 50 000 files at 43 s and 77 GiB
  allocated, and that scales with the peer count, so a swarm the attacker controls makes it worse.
  Cost is pieces × tree nodes × peers, not metadata size.
- **41:** a `ut_metadata` request for a negative or out-of-range piece, once the engine has a
  metadata buffer (after a size or the info): it indexes or slices the buffer out of range
  (`index out of range`, `slice bounds out of range`).
- **42:** the extended handshake's `metadata_size` passes the engine's 32-bit cap check (`uint32(size)
  > maxMetadataSize`) for a 64-bit value, then the engine allocates it (4.9 GiB at 2^32, OOM at 2^47,
  `makeslice` panic at 2^62), under the client lock.
- **43:** a v2 `Hashes` or `HashRequest` message dereferences the torrent's files or info, nil before
  the metadata (`nil pointer dereference`), from any peer. Separately, the engine panics answering a
  `ut_holepunch` message from a peer that did not advertise the extension, and keeps an unbounded
  per-name counter of handshake extensions and a per-length counter of requests, which a hostile
  peer grows without bound (the review's N1/M2).

36–43 crash or stall the engine when it parses a message or applies an info, several before any
storage hook, so a check in `OpenTorrent` (this branch's first attempt) is too late. **The decision:
the engine parses and applies only what we have vetted.** The gate sees every message first, through
the engine's own callbacks (`PeerConnReadExtensionMessage`, `ReadExtendedHandshake`, `ReadMessage`),
which run under the client lock.
- **The gate** (`pendingmagnet.Gate`) wraps three engine callbacks:
  - a `ut_holepunch` message (binary, not bencode) is cleared, so the engine refuses it and drops
    the peer; it moves no payload while the magnet is pending, so it needs none. Scanning it as
    bencode would wrongly clear every legitimate one;
  - every other extended message must pass `infocheck.CheckMessage` (the same scan as an info's
    bytes), or the gate clears it and the engine drops the peer (37, and the review's big-integer
    and typed-string shapes, M4/N2);
  - each `ut_metadata` data piece goes into the fetched torrent's assembly, and the message becomes
    `d8:msg_typei2ee` plus its old bytes. The engine reads a dictionary with trailing bytes, sees a
    reject and ignores it (`client.go:1320-1361`); it never builds an info from peers, so `GotInfo`
    fires only after our `SetInfoBytes`. A rewritten piece counts as rejected, so the engine does not
    ask that peer again; new peers are still asked for every piece;
  - a `ut_metadata` request for an impossible piece becomes a reject too (41); other requests are
    left alone, so metadata we have is still served;
  - in the extended handshake, a `metadata_size` the engine cannot hold is cleared (42), and every
    extension but ut_metadata and ut_pex is dropped, so a handshake of many names cannot grow the
    engine's per-name counter (N1);
  - `Hashes`, `HashRequest` and `HashReject` become keep-alives (43; the product verifies pieces
    with v1 hashes), and a `Request` for an outsized chunk length too, before the engine records it
    (M2).
  A refused assembly is a `PeerError` naming the peers' IPs that sent its pieces, so the coordinator
  can retry without them (022). `NewGate` wraps the callbacks already in the configuration, so it
  must be the last to set them.
- **`infocheck.CheckBytes(bytes, Limits)`** is the one entry point for raw info bytes (magnets now,
  `.torrent` files from 019):
  - at most `Limits.MetadataBytes`;
  - the info must be a dictionary, and the scan refuses an integer of more than 19 digits (the
    engine parses a longer one under an unknown key as a big integer, quadratically, N2);
  - one iterative scan, with no recursion, that checks the syntax, refuses nesting deeper than 64
    (37) and refuses a `file tree` whose dictionaries add up to more than 16 times its length: that
    sum is what the engine's tree decoder parses (38). The tree must be a dictionary, because the
    engine's decoder also takes a one-element list there, which would hide it from the count (M2);
  - `bencode.Unmarshal`, then `Check`.
- **`infocheck.Check(info, Limits)`**:
  - `meta version` 0, 1 or 2;
  - a complete v1 part: a piece length from 1 to 2^30 (36, 39), `pieces` non-empty and a multiple
    of 20, exactly one of `length` (> 0) and `files`, no negative length, and exactly the pieces the
    v1 total needs (pad files included); none with `meta version 2` is `ErrV2Only` (the 007
    decision);
  - pieces × ⌈piece length / 16 KiB⌉ within the engine's uint32 request index (39), else
    `ErrInfoTooLarge`;
  - with `meta version 2`, the file tree must be the same torrent. `infocheck` walks the tree
    itself, in the engine's order, and calls no engine method on it (35): a folder holds no file
    properties; a file has a non-negative length and a 32-byte pieces root exactly when it is not
    empty; its files equal the v1 files without BEP 47 pad files, in order, path and length, each at
    the offset the v2 view aligns it to, and they need the same piece count. The walk stops at the
    first file that differs, so its cost is bounded by the v1 part (a path is a chain to its
    parent, never a copy per node). Otherwise `ErrInconsistentInfo`;
  - no file tree unless `meta version` is 2;
  - for `meta version 2`, pieces × file tree nodes within a bound that allows for the engine's full
    set of peers (40), else `ErrInfoTooLarge`; this refuses a hybrid of more than about a thousand
    files, which 022's product limits may lower;
  - at most `Limits.Files` v1 files and `Limits.Pieces` pieces, and `Limits.MetadataBytes` at most
    the engine's 16 MiB (the last by `Limits.Validate`, `ErrInvalidLimits`). The numbers are the
    caller's (019/022 set the product's).

**What it covers.** Every shape measured to crash or stall the pinned engine (`fixture.HostileInfos`
and the wire test's extra shapes) and every shape measured to leave it two disagreeing views; plus
`FuzzCheckBytes`: for any bytes, `CheckBytes` never panics and admitted bytes never panic the engine
when applied through a `Deferred` and read back (files, piece states, `DownloadAll`, stats). The
fuzz budget is in report 011. The engine's sampled assert (33) is checked only on the inputs a run
samples, and no fuzzed torrent has a peer with pieces, so request planning (39) is covered by the
`Check` rule and its pin, not by fuzzing. An unmeasured engine bug past the rule stays possible,
which is why the rule admits only info whose two views are identical, and why each pin fails when a
release changes the engine.

**Remaining cost, measured** (M4, in-process, `runtime.MemStats`). The engine never parses a hostile
info; our `CheckBytes` does, outside the client lock. Its cost grows with the node count, not only
the size:
- a 16 MiB v1 info of ~550k files: ~1.2 s, ~3 GiB allocated, ~0.5 GiB in use;
- the worst file tree under the bounds, 16 MiB of tiny folders 29 deep (work 15×): 4.4 s, 8.3 GiB
  allocated, 2.2 GiB in use; the same at 4 MiB: 1.1 s, 2.1 GiB, 0.5 GiB. Lowering the depth or work
  bound does not help (the node count drives it: at work 4.8× it still costs 5.1 GiB at 16 MiB).
- An admitted v1 info is parsed once more by the engine (`SetInfoBytes`), at about the same cost. An
  admitted `meta version 2` info costs the engine pieces × tree nodes per connected peer when it
  applies it; the `maxTreeWalks` bound (40, `1<<20`) caps that across a full set of peers.
`Limits.MetadataBytes` is the main knob, and `Files`×`Pieces` and the tree-walk bound cap the rest;
022 sets them.

**A refusal does not prove the magnet is bad.** A peer serves the metadata, and any peer can serve
info whose SHA-1 is not the btih (finding 11), or a piece that does not fit: `infocheck`'s refusal
may come from one liar on a legitimate magnet, exactly as `ErrInfoMismatch` does. 022 shows it as
"metadata could not be used" and retries with a new `Fetch` without that peer, never as "this
torrent is unsupported". The first copy of a piece wins, so one liar's piece spoils an assembly, as
before.

**Not done, deliberately.**
- The gate screens the message kinds the review found could crash or exhaust the engine (metadata,
  the extended handshake, the hashes messages, an oversized request, the ut_holepunch panic) and
  keeps only ut_metadata and ut_pex from a handshake. Other message types the engine decodes from a
  peer are left to it; the product is v1-only. 018 revisits the full peer-message surface when it
  builds the product client, including whether to re-enable hole punching outside the pending
  phase.
- No `recover` around the admission: `CheckBytes` runs first, outside the lock, where a recover
  would not help either; fuzzing is the defence.
- `Fetch` cannot verify that its client was built with this gate (the engine does not expose the
  configuration); 018 owns building the one product client with `NewGate` and is where this is
  guaranteed.

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
    info whose SHA-1 *is* the btih is the hostile case, which the gate and `infocheck` keep from the engine).
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
  - a hostile peer (`swarm.MetadataPeer`) serving each info shape of findings 33–39 under its own
    SHA-1: `Fetch` ends at once with the refusal, the engine survives and nothing is left; an info
    over the file limit and metadata over the size limit (`ErrInfoTooLarge`). With the gate's
    rewrite disabled, the finding-36 shape kills the test binary;
  - the gate and the assembly as units: pieces in any order, the first copy kept, every data message
    rewritten (registered torrent or not), requests and other extensions left alone, and each piece
    that does not fit;
  - parallel `Confirm`/`Confirm` and `Confirm`/`Cancel`;
  - both switches;
  - calls after the end.
- **Pinned engine findings in `internal/torrentengine/lifecycle_test.go`:**
  - 31: `SetInfoBytes` after a peer's metadata returns nil and keeps the peer's info.
  - 32: `AddTorrentOpt` on a closed client returns a torrent that is never closed.
  - 33–37: each hostile shape crashes the engine (`TestPinnedEnginePanicsOnHostileInfo`, a child
    process per shape).
  - 38: decoding a file tree twice as deep allocates more than three times as much
    (`TestPinnedEngineDecodesAFileTreeInQuadraticWork`).
  - 39: a piece length of 2^32 panics the request planner once a peer has every piece
    (`TestPinnedEnginePanicsPlanningPastItsRequestIndex`, a child process).
- **`infocheck` (unit and fuzz):** every fixture the engine handles is admitted, from its bytes;
  every shape measured to crash or stall the engine, or to leave it two disagreeing views, is
  refused; each file tree node the engine would trust is refused by the walk; the scan's syntax,
  depth and tree-work bounds at and past each limit; the piece length and chunk index bounds; a sum
  overflow where it happens; the limits at and over; `FuzzCheckBytes` (seeds: the catalog and the
  hostile shapes under 1 MiB).
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
  every add path, builds the product client with `pendingmagnet.NewGate`, and decides whether other
  extended messages need the same depth bound.
- **019** runs `infocheck.CheckBytes` on every `.torrent`'s info bytes before any add, and caps and
  scans the whole file the same way before `metainfo.Load` (a hostile `.torrent` reaches findings
  33–43 too), and sets the product limits.
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
  - the product limits for `Request.Limits` (with 019), `MetadataBytes` above all: the cost of a
    hostile info grows with it (about 0.5 GiB in use per 4 MiB);
  - shows an `infocheck` refusal as unusable metadata and retries with a new `Fetch` without the
    peer;
  - tracker responses are bencode too: scan them before the engine parses them (with 026);
  - the rejection of a `btmh`-only magnet;
  - the `.app` scenario.
- **029** restores existing data through the add path, never through a pending import.
- **Recheck** when the engine changes: finding 11 (`TestPinnedEngineAcceptsInfoBytesThatDoNotMatchTheInfohash`),
  31, 32 and 33–43 (their pinned tests), the gate's callbacks (the wire tests), and the piece lookup on every access. If a release caches
  `PieceImpl`, `TestConfirmPutsTheFilesExactlyInTheChosenFolder` fails.
