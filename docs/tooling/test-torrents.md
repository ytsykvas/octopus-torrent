# Test torrents and the local swarm

**Status:** `implemented`  
**Requirements:** `AI-08`, `TR-02`  
**Related tasks:** [006](../../product/tasks/006-controlled-torrent-fixtures.md); used by [007](../../product/tasks/007-engine-api-lifecycle-spike.md) (engine contract tests, the spike app), 009, 010, 011, 018, 019  
**Last verified version:** task 006 ([report](../verification/006-controlled-torrent-fixtures.md))  
**Last updated:** 2026-10-17  
**See also:** [ADR controlled-swarm](../decisions/controlled-swarm.md), [testing](../development/testing.md)

## Goal and behavior

Engine tests get our own torrents and a swarm closed to the internet:

```
fixture.Named("hybrid-tree")                 deterministic payload + metainfo bytes
  └─ fx.WritePayload(seeder.Dir)             files exactly where the engine expects them
swarm.NewPeer(dir, Options{RateLimit})       anacrolix client: 127.0.0.1, TCP, no DHT/trackers/PEX/…
  ├─ seeder.Seed(ctx, mi)                    add + verify + Settle → missing pieces as IncompleteError
  ├─ leecher.Leech(mi); swarm.Connect(lt, seeder)
  ├─ swarm.WaitComplete(ctx, lt)             nil, or IncompleteError{Completed, Length, MissingPieces}
  ├─ swarm.Count(t) / swarm.Record(…)        payload vs all bytes; samples, marks, residual traffic
  └─ peer.Close(ctx)                         client + storage closed, the address refuses connections
swarm.WaitEngineStopped(ctx)                 no engine goroutine left
```

Nothing here touches the network beyond loopback, the owner's files, or the application binary.

## Architecture analysis before the change

These are test-support packages. No product layer, DTO or binding changes. They sit under
`internal/torrentengine/testutil`, next to the future adapter of 018, and they are the only code
that imports anacrolix/torrent today.

Every wait takes a context. Every peer is closed by the test that created it. Temporary directories
come from `t.TempDir()`.

## Contracts and data

### Fixture catalog

The catalog is in `internal/torrentengine/testutil/fixture/testdata/fixtures.json`.

| Name | Format | Pieces | Purpose |
|---|---|---|---|
| `v1-single` | v1 | 4 × 16 KiB | single file of 3.5 pieces |
| `v1-tree` | v1 | 3 × 32 KiB | nested folders, repeated names (`docs/readme.txt`, `media/readme.txt`), `empty.dat`, `Ünïcode/файл.bin`, pieces across files. **Does not transfer on the pinned engine** (empty-file chunk defect) |
| `v1-tree-no-empty` | v1 | 3 × 32 KiB | the same tree without the empty file: the control for that defect |
| `v1-large` | v1 | 64 × 16 KiB | 1 MiB for rate-limited transfers, measurement, cancellation |
| `v2-single`, `v2-tree` | v2 | — | parse-only. **Adding them panics on the pinned engine** |
| `hybrid-single`, `hybrid-tree` | hybrid | 4 × 16 KiB, 5 × 32 KiB | BEP 47 pad files, an empty file, a piece layer |

Invalid fixtures, with the first engine stage that rejects each:

| Name | Problem | Engine |
|---|---|---|
| `invalid-truncated` | file cut inside the info dictionary | `load` |
| `invalid-info-type` | `piece length` is a string | `info` |
| `invalid-piece-layer` | the piece layer does not hash to `pieces root` | `pieceLayers` |
| `invalid-pieces-length` | `pieces` is not a multiple of 20 | `add` |
| `invalid-negative-length` | a file of length −1 | `add` |
| `invalid-hybrid-mismatch` | the v1 list disagrees with the file tree | `accepted` (019 must reject it) |
| `invalid-path-traversal` | a `..` path component | `accepted` (019 must reject it) |
| `invalid-empty-traversal` | the empty file at `["..", "victim.txt"]` | `accepted`; **adding it truncates an existing `victim.txt` beside the torrent folder** |
| `invalid-embedded-separator` | the empty file's single component is `"a/../../x"` | `accepted`; adding it truncates `<save folder>/x` |
| `invalid-dot-components` | components `"."` and `""` | `accepted` (019 must reject it) |

Each entry records the name, purpose, format, piece length and count, the full `infohashV1` and
`infohashV2`, every file's path, length and SHA-256, and `engine`. The bytes of every payload file
are SHA-256 blocks of `"<set>/<path>:<i>"`, so the same set (`single`, `tree`, `large`) has the same
bytes in every format.

`fixture.FilePath(dir, file)` follows the engine's layout:

- a v1 single file sits at `<dir>/<name>`;
- a BEP 52 single file sits at `<dir>/<name>/<name>`;
- a tree sits under `<dir>/<root>/`.

### Swarm API

| Function | Behavior |
|---|---|
| `NewPeer(dir, Options{RateLimit})` | A client on `127.0.0.1:0`. It uses TCP only, has every discovery path off, keeps its storage in `dir` with in-memory piece completion, captures its log and records every peer address. Tripwires back the switches: only loopback addresses pass the IP block list, and HTTP, tracker and DNS paths are refused and recorded in `Tripped()`. |
| `Peer.Seed(ctx, mi)` | Adds the torrent, verifies the data and waits until no piece is still being checked. Missing pieces come back as `*IncompleteError` together with the torrent. |
| `Peer.Leech(mi)`, `Peer.AddInfohash(h)` | Adds the torrent and downloads all of it; the infohash form waits for metainfo, as a magnet does. |
| `Peer.Add(mi)` | Adds the torrent; an engine panic is returned as an error. A metainfo that names a tracker, web seed or DHT node is refused. |
| `Connect(t, peers...)` | The only peer source. Peers are trusted: they share 127.0.0.1 and the engine bans by IP, so a peer that sends bad data has its piece re-requested instead of being banned. |
| `WaitInfo`, `WaitComplete` | Bounded by `ctx`. On expiry: `"no metainfo: …"`, or an `*IncompleteError` wrapping `ctx.Err()`. |
| `Settle(ctx, t)` | Waits until no piece is queued, hashing or storing its verdict; bounded by `ctx`. Read piece state only after it: `VerifyDataContext` returns before the verdict is stored. `Peer.Seed` uses it, and so do the 009 storage tests. |
| `Count(t)` | `PayloadRead`, `PayloadWritten`, `Read`, `Written`. Payload is piece data; the other two include protocol messages. |
| `Record(interval, torrents)`, `Mark`, `Stop` | Sample counters. `Log.FirstPayload`, `LastPayload`, `MarkAt` and `PayloadAfter(mark, name)` give the start, the stop and the residual traffic. |
| `Peer.Settings`, `ListenAddrs`, `SeenAddrs`, `Logs` | What the configuration allows, where the peer listens, the address of every peer it saw, and its engine log. |
| `Peer.Client()` | The raw engine client, for tests of the engine's own API (007). The peer still owns it: close it only through `Peer.Close`. |
| `Main(m)` | The `TestMain` body of every package that starts engine clients (see below). |
| `MainClassicIO(m)` | `Main` for a package whose tests need classic file IO: it restarts the test binary with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic` when it is not set (009). |
| `Options.DefaultStorage`, `Options.DataDir` | A peer can take the engine client's default storage and its data folder from the caller, who closes the storage (009). |
| `Peer.Close(ctx)`, `Refuses(ctx, addr)` | Stop the client and storage; wait until the address refuses connections. |
| `NonLoopback(text)`, `WaitEngineStopped(ctx)` | Find non-loopback IP addresses; prove no engine goroutine is left. |

## Scenarios and TDD

The RED and GREEN record for each stage is in the [report](../verification/006-controlled-torrent-fixtures.md).
The scenarios are in `swarm_test.go`:

- single-file and multi-file transfers, byte for byte;
- local peers only;
- no peers;
- a corrupted piece;
- cancellation;
- measurement;
- two runs in a row;
- the add stage of every fixture;
- the empty-file defect;
- error branches;
- a corrupted piece arriving over the wire;
- payload files still mapped after `Close`;
- zero-length paths truncating existing files on add, and a failed piece check renaming every
  existing file of that piece to `<name>.part` (the test waits for the checks, so the outcome is
  deterministic);
- the tripwires.

`swarm.Main(m)` is the `TestMain` body of every package that starts engine clients: the swarm, the
engine contract tests of 007 (`internal/torrentengine`), the engine spike and its app. It refuses
`TORRENT_STORAGE_DEFAULT_FILE_IO` and `GOPPROF`, which change what the engine does, and after all
tests it fails the package if any engine goroutine is left.

Each wait has its own limit:
- a transfer: 20 s;
- no peers: 2 s;
- close: 5 s.

## Implementation and reuse

**`fixture`:**
- `bencode.go`: our own encoder;
- `payload.go`: content, `WritePayload`, `CorruptPiece`;
- `generate.go`: v1, v2 and hybrid;
- `catalog.go`: `Fixture`, `All`, `Named`, `ParseStage`, `WriteCatalog`;
- `specs.go`: the catalog.

**`swarm`:**
- `peer.go`;
- `wait.go`;
- `counters.go`;
- `diagnostics.go`;
- `testmain.go` (`Main`).

The test seams are unexported package variables: `newClient`, `pieceBusy`, and `mainOutput` and
`mainLimit` for `Main`.

## Checks and 100% coverage

```sh
go test -race -count=1 ./internal/torrentengine/...      # both packages; the swarm takes about 12 s
go test ./internal/torrentengine/testutil/fixture -args -update   # rewrite testdata/ after changing the catalog
```

Both packages are part of the gate's Go inventory and reach 100.0% of statements in the unit run.
They need no `.app` run, because the application does not import them.

## Refactoring and debt check

Engine timing is handled in the harness, not in the tests:
- `Settle` waits until piece checks finish;
- `Refuses` polls until the address refuses connections.

Each engine limitation is a named test, not a skipped one. The tracked items are listed in the
[ADR](../decisions/controlled-swarm.md#verified-grounds).

## Usage and recovery

- **Adding a fixture:**
  1. Add a `spec` in `specs.go`.
  2. Run the `-update` command above.
  3. Check that the `fixtures.json` diff contains only the new fixture.
  4. Commit `testdata/`.
- **A test fails with "testdata … differs from the generator":** the generator changed. Re-run
  `-update` only if the change is intended.
- **A swarm test fails:** the failing test prints both peers' engine logs (`engine log of the peer
  in …`).
- **The engine is upgraded:** `TestEngineNamesTheModuleThisBuildUses` fails. Re-check the recorded
  `engine` stages and the tracked limitations, then update `fixture.Engine`.

## Change history and evidence

- 2026-10-17, task 009: `swarm.MainClassicIO`, `Options.DefaultStorage` and `DataDir`; the engine's file
  IO is classic in `internal/torrentengine/filestore`. Disk images for volume scenarios are
  `internal/testutil/volume` ([report](../verification/009-storage-and-completion-spike.md)).
- 2026-10-17, task 007: `Peer.Client`, `swarm.Main`; the truncation test waits for the piece checks
  and asserts the rename to `.part` (finding 13). The engine contract tests and the spike app use the
  package ([report](../verification/007-engine-api-lifecycle-spike.md)).
- 2026-10-16, task 006: the fixture catalog, the swarm and eight engine findings
  ([report](../verification/006-controlled-torrent-fixtures.md), [ADR](../decisions/controlled-swarm.md)).
