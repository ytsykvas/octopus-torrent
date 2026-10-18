# Report 011: magnet metadata without payload and the final folder

**Task:** [011](../../product/tasks/011-magnet-metadata-storage-spike.md). **Requirements:** `ADD-02`, `ADD-04`, `ADD-07`. **Register:** O7. **ADR:** [magnet-pending-storage](../decisions/magnet-pending-storage.md). **Branch:** `task/011-magnet-metadata`.

## Session architecture plan

Approved by the owner on 2026-10-18 after an independent adversarial review of the first draft (drop and re-add), which found two windows for a lying peer's metadata; this version confirms in place.

### Context

Task [011](../../product/tasks/011-magnet-metadata-storage-spike.md) is a P1 spike for `ADD-02`, `ADD-04` and `ADD-07`. It owns register item **O7** (temporary magnet storage and the move to the final folder). The ADR gets the register's name, `docs/decisions/magnet-pending-storage.md`; the task file's `pending-magnet.md` is corrected to it. Dependency 009 is `done`.

What the task must prove:
- A magnet's metadata can be fetched with no payload moving in either direction before the import is confirmed.
- After confirmation, the files land exactly in the chosen folder.
- Cancelling never touches an existing torrent or its data.
- The wait is bounded and cancellable, also when the cancel comes at the same moment as `GotInfo`.
- A repeated add, a double confirmation and shutdown are safe.

The product binary does not change: 018 links the engine, 022 builds the coordinator, 023 the durable record.

**Owner decision (2026-10-18):** no `.app` QA in 011, and EngineSpike is not extended. The `.app` row is `not_run`, owner 022.

### How this version improves the previous draft

The previous draft fetched the metadata, dropped the torrent, and re-added it with the final storage on confirmation. The independent review found that this opens two windows where a lying peer's metadata could reach the user's folder. Closing them needed a wrapper (`verifiedOnly`), a strict call order, a swarm hook and a production function split in two only for a test. Peers also had to be found again after every confirmation.

**New design: confirm in place.** The pending torrent's storage is a *deferred storage*. Until confirmation it holds nothing and refuses everything; on confirmation it is attached to the final folder's storage, without `Drop` and without a second add. This is possible because:
- The engine resolves a piece's storage on every access: `Piece.Storage()` calls `t.storage.PieceWithHash` → `TorrentImpl.Piece` each time (`piece.go:79-88`, `storage/wrappers.go:44-51`). It never keeps a `PieceImpl`.
- The guard's `TorrentImpl` from 009 has only `Piece` and `Close` (`filestore/guard.go:194-230`), so delegating is trivial.

What this removes:
- **Both liar windows.** The info is set once, and it is the info whose bytes we verified: `t.Metainfo().InfoBytes` are the bytes the engine parsed.
- **`verifiedOnly`, the swarm hook, the add/attach split, and the duplicate window between `Drop` and re-add.**
- **Re-discovering peers** after confirmation: the connections are kept.
- **Finding 15 on this path.** We call the final storage's `OpenTorrent` ourselves, so its error comes back to us directly. A refused folder (HFS+, a path that leaves the folder) leaves the import pending, and the user can choose again.

The drop-and-re-add design stays the documented fallback. Stage A decides between the two.

### Facts from the pinned source (v1.61.0)

Checked by the independent review; each is asserted by a test.

**Storage opens when the metadata arrives.** `setInfo` calls `OpenTorrent` (`torrent.go:553-575`). A refusal makes the engine discard the metadata and ask again, so `GotInfo` never fires (`1855-1872`).

**A storage that holds nothing is never read or written.**
- `onSetInfo` reads only `Completion()`. With `Ok=true`, no initial check is queued (`1801`, `2905-2922`).
- Without info, or with download disallowed, no request is made (`requesting.go:204-214`). An unrequested chunk is dropped before any write (`peerconn.go:1900-1903`).
- Nothing can be uploaded (`peerconn.go:677-681`, `1159`).
- Two conditions: `IgnoreUnverifiedPieceCompletion` stays false (`torrent.go:346`); `DisableInitialPieceCheck` is a third line of defence.

**The switches** are plain flags, valid without info and kept once info arrives (`torrent.go:3070-3120`). A torrent with no info still dials (`1890`).

**Finding 11.** A v1-only hash accepts any bytes (`torrent.go:617-640`).
- A btih that is a truncated v2 hash rewrites the infohash (`624-633`). We compare against the *requested* hash, never `t.InfoHash()`.
- Once info is set, `SetInfoBytes` only replaces the bytes and returns nil (`669-672`).

**Drop and close.**
- `Drop` is synchronous and frees every short hash (`t.go:102-114`, `torrent.go:1101-1137`).
- Connections and dials can outlive it (`client.go:855`).
- `GotInfo` closes only in `onSetInfo` (`torrent.go:605`). `Client.Close` drops every torrent (`client.go:539-545`).
- `AddTorrentOpt` does not check for a closed client (`1512-1540`).

**Counters.** `Client.Stats()` is lifetime, client-wide and payload-only, including dropped torrents and rejected chunks (`peer.go:293-300`, `conn-stats.go:44-56`, `peerconn.go:977`).

**DataDir.** The engine writes to `DataDir` only when `DefaultStorage` is nil (`client.go:306`).

### Design (goes into the ADR)

**`filestore.Deferred`** sits beside `Refusing()`.
- `OpenTorrent` succeeds with no file and keeps the info and hash.
- **Before `Attach`:** `Piece` returns pieces that report `Completion{Ok: true}` and refuse `ReadAt`, `WriteAt`, `MarkComplete` and `MarkNotComplete` with `ErrNotAttached`. Each refusal is counted (`Touched()`). The count is the committed proof that the engine never touched it; the engine itself only logs such an error.
- **`Attach(final storage.ClientImpl)`:**
  1. Calls `final.OpenTorrent(ctx, info, hash)` itself and returns its error.
  2. Swaps the delegate atomically.
  3. A second `Attach` → `ErrAttached`.
- **`Close`** closes the delegate if there is one.

**`pendingmagnet.Fetch(ctx, cl, Request{InfoHash, Peers, Wait}) (*Pending, error)`:**
1. **Validate without touching the engine:**
   - `Wait <= 0` → `ErrInvalidWait`;
   - zero hash → `ErrInvalidSource`;
   - a context already done → its error.
2. `AddTorrentOpt{InfoHash, Storage: deferred, DisableInitialPieceCheck: true}`.
   - `new=false` → `ErrDuplicate`, with no `Drop` and no change: the handle is borrowed.
   - Client closed → `Drop`, then `ErrClientClosed`.
3. Disallow download and upload, then add the peers.
4. **One `select`** over `GotInfo`, `ctx`, the limit, `t.Closed()` and `cl.Closed()`. Classify the outcome in a fixed order:
   1. the caller's context → its error;
   2. client closed → `ErrClientClosed`;
   3. torrent closed → `ErrTorrentClosed`;
   4. limit → `ErrNoMetadata` (wraps `DeadlineExceeded`);
   5. otherwise the metadata arrived.

   A cancel always wins, so a cancelled fetch never yields metadata.
5. **`verify(requested, bytes)`**, a pure function, checks:
   - the SHA-1 equals the requested hash (else `ErrInfoMismatch`);
   - the bytes parse;
   - `HasV1()` (else `ErrV2Only`, 007 decision).
6. Every error after the add → `Drop`, then return. On success, return a `*Pending` that owns the torrent.

**`Pending`**: a mutex and a state (`pending` → `confirmed` | `canceled`).
- `Info()` comes from the verified bytes; `InfoBytes()` returns a copy.
- **`Confirm(ctx, Final{Storage, Download, Upload}) (*torrent.Torrent, error)`:**
  - **Refusals, with no engine call:**
    - confirmed already → `ErrConfirmed`;
    - canceled → `ErrCanceled`;
    - client or torrent closed → `ErrClientClosed` or `ErrTorrentClosed`.
  - **Then:**
    1. `deferred.Attach(final)`. An error is returned and **the import stays pending**, so another folder can be chosen.
    2. Allow upload if `Final.Upload`.
    3. Allow download if `Final.Download`. There are no cancelled requests (download was never allowed with info), so finding 25 cannot apply.
    4. State `confirmed`; ownership of the torrent passes to the caller.
- **`Cancel() error`:**
  - after `Confirm` → `ErrConfirmed`, and the torrent keeps running;
  - the first call → `Drop`;
  - later calls → nil.
- **Calls after cancellation** are refused by state. Late engine events after `Drop` touch nothing (`Touched()` stays 0).

**Cached completion.** At `onSetInfo` the engine cached "not complete" for every piece.
- That is right for a new import into a folder without the torrent's files. 020 refuses a folder that already holds any of its paths (findings 8, 13).
- Re-adding data that is already on disk is the restore path (029), not a pending import. The ADR states this precondition.

**Ownership across add paths (ADR; 018/022).** While a fetch or a dialog is pending, any other add of the same hash gets `new=false`: a `.torrent` import, a restore, a second magnet. That add gets the deferred torrent with its own storage ignored. Per-infohash serialization must therefore cover every add path.

**Limits.**
- The fetch limit is required. The product default, an expiry for a dialog left open, and the waiting UI belong to 022.
- Metadata is ≤ 16 MiB (engine).
- File count and depth limits are 019.
- Paths are checked by the guard at `Attach`.

### Boundaries and files

Not changed: product `main.go`, the product UI, bridge, facade and bindings, `go.mod`, `.npmrc`, EngineSpike and the isolation step.

| File | Change |
|---|---|
| `internal/torrentengine/filestore/deferred.go` + `deferred_test.go` | `NewDeferred() *Deferred`, `OpenTorrent`, `Attach`, `Touched`, `ErrNotAttached`, `ErrAttached` |
| `internal/torrentengine/pendingmagnet/{doc.go,fetch.go,pending.go,verify.go,*_test.go,main_test.go}` | new prototype for 018/022 |
| `internal/torrentengine/testutil/swarm/peer.go` (+ test) | `PeerInfos(peers...) []torrent.PeerInfo` (trusted); `Connect` uses it, so the loop exists once |
| `internal/torrentengine/lifecycle_test.go` | two pinned findings (below) |
| docs, task notes | see Documents |

### Stages, tests first

One checkpoint, gated by `go tool devtool check`.

#### Stage A: measurements (scratch, not committed)

1. A fetch with a counting deferred storage, for `v1-single`, `v1-tree` and `hybrid-tree`. Expect 0 touches and 0 payload on both sides. Record:
   - protocol bytes;
   - time to metadata;
   - whether the seeder connection survives a 3 s "dialog" with no wanted piece.
2. `Attach`, then allow download, then `DownloadAll`: the download completes into the final folder with no new `Connect`. If the connection was dropped, record whether the engine dials the known address again.
3. A liar's metadata: `ErrInfoMismatch`, and nothing on disk.

**Decision rule:**
- If (1) shows a touch, or (2) cannot complete even with the peers given again, attach-in-place is refuted. Work stops and the plan falls back to drop-and-re-add (previous draft), amended with the owner.
- If (2) needs the peers again, `Final` gets `Peers`, added after `Attach`.

#### Stage B: units

- **RED:** `go vet` fails with `undefined: NewDeferred` / `verify` / `PeerInfos`.
- **`Deferred`:**
  - before `Attach`, each piece method refuses and is counted, `Completion` is not counted, `Close` is nil;
  - `Attach` with a refusing storage returns its error and stays detached;
  - `Attach` then delegates `Piece` and `Close` (inner calls observed);
  - a second `Attach` → `ErrAttached`.
- **`verify`:** a match; a mismatch; a truncated v2 btih (a mismatch); bytes that hash right but do not parse; v2-only info built in the test.
- **Request validation:** no engine call.
- **`swarm.PeerInfos`.**

#### Stage C: real engine on the loopback swarm

Setup:
- `TestMain` is `swarm.MainClassicIO`.
- Leechers use `DefaultStorage: filestore.Refusing()` and `KeepAliveTimeout: transferswitch.WakeInterval`.
- Every test snapshots the leecher client's payload counters and the folder trees, directories included.
- While a fetch or a dialog is pending, these hold: 0 touches, no payload, no new file anywhere.
- **RED:** `undefined: Fetch`.

| # | Test | What it proves |
|---|---|---|
| 1 | `TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload` | `v1-single`, `v1-tree` (zero-length file), `hybrid-tree` by v1 hash. The metadata is verified; the import is pending for a 2 s "dialog" with the seeder connected; payload is 0 on both sides; protocol bytes > 0; 0 touches; no file. |
| 2 | `TestFetchWithNoPeerEndsAtItsLimit` | `ErrNoMetadata` wraps `DeadlineExceeded`, within limit + 1 s; client empty |
| 3 | `TestACanceledFetchReturnsTheContextErrorAndLeavesNothing` | a cancel while waiting; a context already done at entry adds no torrent |
| 4 | `TestACancelAsTheMetadataArrivesLeavesNothing` | 20 runs. A watcher cancels the moment the pending torrent's `GotInfo` closes. Invariants: either the context error with an empty client, or a `Pending` whose `Cancel` empties the client. Always 0 touches, no payload, no file, and both goroutines end. |
| 5 | `TestFetchRefusesMetadataThatDoesNotMatchTheInfohash` | the liar setup from `lifecycle_test.go:248-273`: `ErrInfoMismatch`, the client is empty, and the liar's zero-length file exists nowhere |
| 6 | `TestFetchOfAKnownInfohashLeavesTheExistingTorrentAlone` | an existing torrent still downloading into its own folder (`filestore` with marks): `Fetch` → `ErrDuplicate`; the torrent stays open with its switches unchanged and completes with the fixture's bytes (`files` fingerprints). A cancelled fetch of another hash leaves it unchanged too. |
| 7 | `TestShutdownEndsAFetchAndRefusesLaterCalls` | `Client.Close` during the wait → `ErrClientClosed` within 1 s. `Fetch` on a closed client → `ErrClientClosed` with no torrent left. `Confirm` after shutdown → `ErrClientClosed` with no file. |
| 8 | `TestConfirmPutsTheFilesExactlyInTheChosenFolder` | `v1-tree`, `hybrid-tree`. The first `Confirm` targets folder A with a storage that refuses → error, and the import is still pending. The second targets folder B → `DownloadAll` completes with no new `Connect`. Files equal `filestore.Layout(B)` with the fixture's bytes; A, the data folder and the rest of the tree are unchanged; after `Client.Close`, still only B. |
| 9 | `TestConfirmAndCancelRaceToExactlyOneOutcome` | parallel `Confirm`/`Confirm` and `Confirm`/`Cancel` under `-race`: exactly one wins; the loser gets `ErrConfirmed` or `ErrCanceled`; the client state matches the winner |
| 10 | `TestConfirmAppliesTheSwitches` | `Download=false` with a seeder: 0 payload for 2 s. `Upload=false` while downloading: a second leecher connected to it gets 0 payload. |
| 11 | `TestCallsAfterTheEndAreRefused` | `Cancel` after `Confirm` → `ErrConfirmed`, and the torrent completes. `Confirm` after `Cancel` → `ErrCanceled`. `Cancel` twice → nil. |

Pinned findings in `lifecycle_test.go` (each failure message names what to re-check):
- **31:** after a peer's metadata set the info, `SetInfoBytes` with other bytes returns nil and keeps the peer's info. Consequence for 019/023: add a `.torrent` with no peers before its info is set.
- **32:** `AddTorrentOpt` on a closed client returns a torrent that is never closed.

#### Stage D

Refactor, documents, `/branch-review`, gate, `done`.

### Coverage

`go tool devtool check`: `pendingmagnet`, `filestore.Deferred` and `swarm.PeerInfos` at 100%.
- Branches the engine cannot reach live in pure, unit-tested code: `verify`'s parse and v2-only checks, `Deferred`'s refusals and second attach, request validation.
- Every engine-side branch has a scenario: duplicate, closed before the add and during the wait, torrent closed, limit, cancel, mismatch, refused `Attach`, every state refusal.
- No product code changes.

### Real vs doubles

Real: the pinned engine, the loopback swarm, classic IO, `filestore` with marks, `files` fingerprints. No test double. The unit tests call `Deferred` and `verify` directly; the inner storage observed in the `Deferred` unit test is `filestore.Refusing()` or a real guard on a temp folder.

`not_run`:
- existing library *records* (no store before 014/015; owner 023);
- the `.app` (owner decision; owner 022);
- magnet trackers and DHT, and a dialog left open for minutes on a real network (owners 022 and 026).

### Documents

- **New:**
  - **ADR `magnet-pending-storage.md`:**
    - O7 accepted for the engine part;
    - the facts and the cached-completion precondition;
    - the alternatives: a temporary folder; drop-and-re-add (the window, finding 31); a separate metadata client; `AddTorrentOpt` with `InfoBytes` (finding 15);
    - ownership across add paths.
  - **Report `docs/verification/011-magnet-metadata-storage-spike.md`:** the session plan, RED/GREEN, stage A numbers, findings 31–32.
  - **Feature doc `docs/app/magnet-metadata.md`**, plus index rows.
- **Marked as superseded:**
  - the 007 bullet "storage the application owns and wipes" in `engine-lifecycle.md`;
  - 009's "bound what the isolated storage may create";
  - architecture §7 ("temporary folder belongs to a pendingImportID");
  - research §5 ("own temporary storage", "we do not promise the absence of empty files");
  - task 011's "From 007/009".
- **Edited:**
  - `project-baseline.md` O7;
  - `engine-storage.md` (`Deferred`);
  - `controlled-swarm.md` (`PeerInfos`);
  - architecture §3;
  - "From 011" notes in 018, 019, 020, 021, 022, 023 and 029;
  - task 011 status, then `plan-nav`;
  - `CLAUDE.md`, `README.md`, `docs/development/testing.md`.

### Out of scope

The coordinator, pendingImportID, queue and dialog expiry (022); validation limits (019); folder collisions (020); v1/v2 identity (021); the durable record (023); magnet URIs, trackers and DHT (022/026); the UI (039).

### Verification

- `go test -race -count=1 ./internal/torrentengine/...`, twice.
- `go test -count=1 ./internal/torrentengine/...`, once.
- `go tool devtool check` → `GATE PASSED`.
- `/branch-review`.
- The `not_run` rows above.

### Self-assessment: 8.5/10

**Strengths:**
- One add, one info and one owner per import. No liar window, by construction rather than by a guard.
- The user's folder is opened only by an explicit `Attach`, and a refusal there is recoverable.
- Every engine fact has a source line and a test.
- Stage A can refute the design before code is written, and the fallback is already designed.
- Less code than the previous draft: no `verifiedOnly`, no swarm hook, no split function.

**Remaining risks:**
- **Attach-in-place relies on the engine not caching `PieceImpl`.** That holds in v1.61.0 and is asserted by test 8. A future engine that caches them would fail that test.
- **The pending torrent keeps connections open while the dialog is open.** That costs sockets, not payload. An expiry is 022's.
- **The cached "not complete" completion assumes the target folder does not already hold the torrent's files.** That is 020's guarantee, stated in the ADR.
- **Test 4 is probabilistic about the interleaving.** Coverage is not: the classification order makes the "cancel and metadata both ready" case take the cancel branch, which test 3 already covers.
- **"Existing library records" are proven at the engine and file level only**, until 014/015.

## Stage log

| Stage | Result |
|---|---|
| A — measurements | done; attach-in-place confirmed, no plan amendment |
| B — units (`Deferred`, `verify`, request validation, swarm helpers) | `pass` |
| C — real-engine scenarios and pinned findings | `pass` |
| D — refactor, documents, review, gate | done: review fixed, `GATE PASSED` (below) |

## Stage A — measurements

**Environment:** Apple M4, macOS 27.0.1 arm64, Go 1.27.2, anacrolix/torrent v1.61.0, loopback swarm, classic IO, 18 October 2026. A temporary scratch test (`internal/torrentengine/filestore/zz_scratch_test.go`, not committed) with a counting deferred storage: a leecher client whose default storage refuses adds the infohash with the deferred storage and `DisableInitialPieceCheck`, disallows both directions, connects the seeder, waits for `GotInfo`, holds the pending import for 3 s, then attaches a `filestore.NewTorrentStorage` in a new folder, allows both directions and calls `DownloadAll` with no new connect.

| Measurement | `-race` (5 runs per fixture) | no `-race` (3 runs) |
|---|---|---|
| Time to metadata, `v1-single` / `v1-tree` / `hybrid-tree` | 2.4–5.6 / 3.3–3.9 / 4.4–5.7 ms | 1.0–2.2 / 1.9–2.1 / 1.6–1.8 ms |
| Storage opened by the engine | once per fetch | once |
| Deferred storage touched (read, write, mark) before the attach | 0 in every run | 0 |
| Payload read / written by the leecher client, payload written by the seeder, during fetch and the 3 s hold | 0 / 0 / 0 | 0 / 0 / 0 |
| Protocol bytes read / written by the leecher client | 795–2044 / 527–1339 | 774–1812 / 1022–1296 |
| Seeder connection after the 3 s hold | still active (1 peer) in every run | same |
| Files in the leecher's data folder before the attach | none | none |
| After the attach: complete with no new connect | every run, 5.6–15.2 ms | every run, 3.8–6.3 ms |
| Files after the attach | exactly `filestore.Layout` of the new folder (zero-length `empty.dat` included) | same |
| Liar (`v1-tree` info under `v1-single`'s hash) | hash check fails 5 of 5; the engine set the liar's info (name `tree`), deferred storage touched 0, no file, payload 0 | 3 of 3 |

Decision rule: no touch, and the download completes after the attach without new peers, so attach-in-place stands and `Final` needs no peers.

## Stage B — units

| Command | Result |
|---|---|
| `go vet ./internal/torrentengine/filestore` with `deferred_test.go` only | `fail` as expected: `undefined: Deferred` |
| `go test -race -count=1 ./internal/torrentengine/filestore -run 'Deferred\|Attach' -coverprofile=…` | `pass`; every function of `deferred.go` at 100% |
| same with `TestAClosedDeferredStorageOpensNoFinalStorage` added (designing `Confirm` showed that a shutdown racing an attach could open the user's folder for a closed torrent) | `fail` as expected: `undefined: ErrDeferredClosed` |
| same after the closed flag | first `fail`: `TestAttachOpensTheFinalStorageAndThenDelegates` tried its second attach after closing (`second Attach = the torrent was closed…, want ErrAttached`); the test now attaches twice before the close; then `pass`, 100% |
| `go vet ./internal/torrentengine/testutil/swarm` with `TestPeerInfosTrustEveryListenAddressOfEveryPeer` | `fail` as expected: `undefined: PeerInfos` |
| `go vet ./internal/torrentengine/pendingmagnet` with `verify_test.go` | `fail` as expected: `undefined: verify` |

## Stage C — real engine

| Command | Result |
|---|---|
| `go vet ./internal/torrentengine/pendingmagnet` with `fetch_test.go` | `fail` as expected: `undefined: Fetch` |
| `go test -race -count=1 ./internal/torrentengine/pendingmagnet -coverprofile=…` | `pass` at the first run, 100.0% of statements, 18.7 s |
| `go test -race -count=3 ./internal/torrentengine/pendingmagnet -v` | `pass`, every test 3 of 3; 51 s |
| `go test -count=2 ./internal/torrentengine/pendingmagnet` (no `-race`) | `pass`; 33 s |
| the cancel race, `-race`, 3 × 20 runs | both outcomes reached in every batch: 15/5, 10/10, 14/6 (metadata returned before the cancel / the cancel returned); nothing left in any run |
| `go test -race -count=1 ./internal/torrentengine -run 'KeepsThePeersInfo\|AddsATorrentToAClosed' -v` (findings 31, 32) | `pass`: the defects exist on the pin; each test fails, naming 011/018/019/023, when a release fixes it |

The pinned-finding tests have no RED of their own: their RED is the engine's behavior, and their failure messages say what to re-check when a release changes it.

## Stage D — refactor

| Change | Check |
|---|---|
| The liar setup existed three times (two 007 tests, finding 31, 011): `swarm.Peer.Lie` replaces them; its RED was `liar.Lie undefined`, its test covers the success and the refused-bytes branch | `go test -race ./internal/torrentengine/testutil/swarm -run 'Liar\|PeerInfos'` `pass`; the three users `pass` |
| `swarm.Connect` builds on `PeerInfos` (one loop) | the swarm transfer tests `pass` |
| Test helpers (`named`, `deadline`, `newPeer`, `seeder`) follow the per-package pattern of `filestore` and `internal/torrentengine`; no shared helper is introduced for three lines each | — |
| `go test -race -count=1 ./internal/torrentengine/...` after the refactor | `pass`: `internal/torrentengine` 96.3 s, `filestore` 44.9 s, `pendingmagnet` 18.3 s, `swarm` 12.9 s, `transferswitch` 1.9 s, `fixture` 3.3 s, `classicio` 1.2 s |

## Stage D — review and fixes

### Branch review (`/branch-review`, three agents, read-only)

| Lens | Finding | Resolution |
|---|---|---|
| tech-lead A | `verify` checked the bytes, but a later `SetInfoBytes`/merge on the same handle replaces only the bytes and keeps the applied info (finding 31): the dialog could show one file list and the folder get another | `verify(requested, bytes, t.Info())` refuses bytes that are not the applied info (`ErrInfoReplaced`, `reflect.DeepEqual`); unit case; finding 31 pinned |
| tech-lead B | `Attach` passed on any final `TorrentImpl`; the engine reads optional fields from the `Deferred` and would never see them | `Attach` reuses the guard's `unsupported`, requires `Piece`, closes what it refused; `TestAttachRefusesAStorageItCannotDelegateTo` |
| tech-lead C, code-reviewer warning 1 | `Final{Storage: nil}` panicked | `ErrNoFinalStorage` in `Attach` and, for a typed nil, in `Confirm`; asserted in `TestAttachWithoutAStorageIsRefused` and test 8 |
| tech-lead, wording | "the engine never keeps a `PieceImpl`" was too broad | ADR names the two holders (a `Reader`'s cached piece reader, the hasher) and why neither exists before the attach |
| tech-lead D | the attach creates the zero-length files at once, even with `Download=false`; a refused folder can keep some | feature doc "Usage and recovery", ADR, 023 note |
| tech-lead E, code-reviewer warning 2 | `Info()` shares its slices | documented read-only; `InfoBytes()` clones |
| tech-lead, test 6 | the duplicate test did not assert one torrent and unchanged switches | asserts the client holds only the existing torrent; its completion shows the download switch unchanged; the upload switch has no getter in the engine and `Fetch` never touches a `new=false` handle (the code returns before any switch) |
| code-reviewer 1, 2 | gofmt; a dead `waitClosed` helper | `gofmt -w`; helper removed |
| code-reviewer warning 3 | the limit and `GotInfo` ready in the same instant discarded the metadata | metadata that arrived is used (`!isClosed(t.GotInfo())`) |
| code-reviewer warning 6 | `Peer.Lie` ignored `new=false` and could drop a torrent it did not create | refuses an existing infohash; swarm test covers it |
| security M3 | `Layout` accepted a path that leaves the torrent's folder but stays in the save folder (name `x`, path `../f`), or equals the folder | a folder torrent's paths must stay strictly inside `<saveRoot>/<name>`; RED: three cases accepted (`[/save/f]`, `[/save/f]`, `[/save/x]`); GREEN `TestLayoutIsTheEnginesPathRuleAndRefusesPathsOutsideTheSaveFolder` |
| security M2 | `Confirm` overwrote a user's non-empty file at a torrent path; the test's "user's file" sat on the empty file, which the guard does refuse | `TorrentStorage.CheckNew` (`ErrPathExists`) before the attach; `Final.Storage` is a `*filestore.TorrentStorage` (L4); RED: test 8 with the file at `tree/media/readme.txt` got `Confirm = "tree", <nil>`; GREEN with the user's bytes intact and nothing created; `TestCheckNewRefusesAFolderThatAlreadyHoldsAPathOfTheTorrent` |
| security L1 | `Attach` opened the final storage under the `Deferred`'s lock, which the engine's close waits for under the client lock | open outside the lock, publish under it, `ErrAttaching` for a second attach, a torrent closed meanwhile gets nothing and its storage is closed; RED: `TestAnAttachInProgressBlocksNeitherCloseNorAnotherAttach` hung (the second `Attach` waited for the lock; stopped after 120 s); GREEN in 2 s |
| security M1 | hostile metadata reaches the engine before `verify`; v2-only info under a v1 infohash panics it; a 16 MiB info costs ~600 MiB | finding 33, reproduced and pinned: `TestPinnedEnginePanicsOnV2OnlyInfoUnderAV1Infohash` runs a child process (20 of 20 without `-race`, 5 of 5 with); recorded as a known residual in the ADR, the feature doc, the research doc, CLAUDE.md, and the 018/022 notes; the overclaim that the hash check is the defence is corrected. The product links none of it (`product isolation`) |
| security L2 | a holder dropping the torrent at the moment of the attach can leave `Confirm` successful for a closed torrent | after L1 a close during the open is `ErrDeferredClosed`; the rest is documented: the caller checks `Closed()` before recording, and per-infohash serialization (018/022) removes the other holder |
| security L3 | one lying peer can block every fetch it joins | 022 note: magnet peers never `Trusted`, retry without the liar |
| security L4 | the dialog's info is unbounded; a failed attach can leave debris; the IP is visible while the dialog is open | 019 caps the info (its note); debris in the feature doc; the expiry is 022's |
| code-reviewer 4, warning 4 | Stage A cannot be re-run; `innerStorage` is a double the plan did not name | see below |
| first full run after the fixes | `TestPinnedEngineKeepsThePeersInfoWhenSetInfoBytesComesLate` failed once (`no metainfo: context deadline exceeded`): the liar's log shows `rejecting accepted conn: don't want conns right now` while it was still checking its pieces. A piece being checked is not pending, so the engine's `needData` is false and every incoming connection is refused; the leecher does not dial again | `Peer.Lie(ctx, …)` returns only after `swarm.Settle`; a failed settle drops the lie (`a lie whose pieces stay busy`); the four liar users then passed 30 of 30 runs with `-race` |

**Real vs doubles, corrected.** One test double exists: `innerStorage` in `filestore/deferred_test.go`,
a recording `storage.ClientImpl` under the `Deferred` unit tests (attach, refusal, close, an attach
in progress). Every attach to a real folder is covered by the real engine in `pendingmagnet`. The
close racing an attach is proven at unit level only; the engine scenarios close sequentially.

**Stage A is not reproducible.** Its scratch test is not committed, so the timings and byte counts
cannot be re-run. The committed `TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload` and
`TestConfirmPutsTheFilesExactlyInTheChosenFolder` are the reproducible form of its claims.

**Child-process rule once.** `childDir` in `lifecycle_test.go` now holds the folder check for both
child tests (findings 9 and 33).

### Final runs (after the review fixes)

**Environment:** Apple M4, macOS 27.0.1 arm64, Go 1.27.2, anacrolix/torrent v1.61.0, 18 October 2026.

| Command | Result |
|---|---|
| `go test -race -count=1 ./internal/torrentengine/...` (before the `Lie` fix) | `fail` once: the liar flake above; every other package `pass` |
| `go test -race -count=1 ./internal/torrentengine/...`, twice | `pass`, `pass`: `internal/torrentengine` 96.0 / 96.0 s, `filestore` 43.6 / 45.2 s, `pendingmagnet` 18.2 / 18.1 s, `swarm` 13.3 / 13.1 s, `transferswitch` 2.0 / 1.8 s, `fixture` 3.4 / 3.4 s, `classicio` 1.3 / 1.1 s |
| `go test -count=1 ./internal/torrentengine/...` (no `-race`) | `pass`: `internal/torrentengine` 93.6 s, `filestore` 40.8 s, `pendingmagnet` 16.4 s, `swarm` 11.6 s |
| `go test -count=20 ./internal/torrentengine -run TestPinnedEnginePanicsOnV2Only`; `-race -count=5` | `pass` 20 of 20 and 5 of 5: the child panics with `piece request order has` every time |
| `go tool devtool check` | `GATE PASSED`: frontend 23 own files at 100% on four metrics; every block of every own Go file covered (unit tests + the `.app` runs of OctopusTorrent and EngineSpike); bindings of both apps match; product isolation holds (314 packages: no engine, `pendingmagnet` or `filestore` in the product); `single code version` pass. `not_run`: engine integration (018), test-bridge UI tests (035). `updates`: `jsdom` 30.1.1 → 30.1.2 and `github.com/anacrolix/chansync` v0.7.0 → v0.8.0 are out of quarantine; neither is part of 011 (chansync is deferred by the owner, `toolchain-versions.md`) |

### Acceptance criteria

| Criterion | Evidence | Result |
|---|---|---|
| Before explicit import confirmation, file data is neither downloaded nor uploaded | test 1 (three fixtures, a 2 s dialog: client and seeder payload 0, `Touched` 0, trees unchanged); tests 4, 5, 10; stage A | `pass` |
| After confirmation, physical files are created exactly in the selected directory | test 8 (`filestore.Layout(B)` with the fixture's bytes after a missing storage and two refused folders, which stay unchanged with the user's file intact; the data folder unchanged; still only B after `Client.Close`); test 11; `Layout` keeps a folder torrent inside its folder | `pass` |
| Cancelling the import does not change existing library records or their data | data: test 6 (an existing torrent still downloading completes with the fixture's bytes after `ErrDuplicate` and a cancelled fetch of another hash; the client holds only it); records: no store before 014/015 | `pass` at the engine boundary; records handed to 023 |
| All checks performed at the right level; nothing `not_run` or failed shown as passed | the tables above; `not_run` below; the one failed run is recorded with its cause | `pass` |
| Check: no other client, wait for the limit | `TestFetchWithNoPeerEndsAtItsLimit` | `pass` |
| Check: cancel at the same moment as `GotInfo` | test 4, 3 × 20 runs, both outcomes reached, nothing left | `pass` |
| Check: repeated add with `new=false` | test 6 (`ErrDuplicate`, no drop; the torrent stays open, the only one in the client, and keeps downloading to completion, so its download switch is unchanged) | `pass` |
| Check: double confirmation | tests 9, 11 | `pass` |
| Check: program shutdown | test 7 (during the wait, a fetch after, a confirm after) | `pass` |
| Run system scenarios in the built `.app` | owner decision: EngineSpike is not extended | `not_run`, owner 022 |

### Not run

| Check | Why | Owner |
|---|---|---|
| The magnet scenario in the `.app` | owner decision; the product links no engine before 018 | 022 |
| Existing library *records* left unchanged by a cancel | no store before 014/015 | 023 |
| Magnet trackers and DHT; a dialog left open for minutes on a real network | the loopback swarm only, every discovery path off | 022, 026 |
| The engine's close racing a confirm with the real engine | reproduced at unit level only (`TestAnAttachInProgressBlocksNeitherCloseNorAnotherAttach`) | 018 |

### Known residual

Finding 33: the pinned engine applies a peer's metadata before `Fetch` can verify it. v2-only info
under a v1 infohash panics an engine goroutine and ends the application; a 16 MiB info of tiny
files costs about 600 MiB and 0.6 s under the client lock. A peer can make such bytes match the
btih, so the hash check does not defend against it. The product links none of this code
(`product isolation`); 018/022 must close it before magnets ship (own BEP 9 handling or an engine
release), as their notes say.

### Refactoring and debt

- Reused, not duplicated: the guard's `unsupported` rule in `Attach`, `Layout` for `CheckNew`, the
  liar setup in `swarm.Peer.Lie` (four users), `swarm.Connect` on `PeerInfos`, the child-process
  folder rule in `childDir` (findings 9 and 33).
- `pendingmagnet` and `Deferred` are prototypes that 018 moves into the adapter; the product does not
  link them, which the isolation step proves.
- No `TODO`, skipped test, ignore directive or lowered threshold was added (gate: `escape hatches`
  pass).
- Open for later tasks, not debt: finding 33 (018/022), the coordinator, its expiry and peer retry
  (022), the durable record (023), the folder dialog's collision report (020), info limits (019),
  trackers and DHT (022/026).
