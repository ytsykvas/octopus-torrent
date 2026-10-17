# The engine version, its lifecycle and who may release what

**Status:** `accepted`  
**Date:** 2026-10-17  
**Task:** [007](../../product/tasks/007-engine-api-lifecycle-spike.md)  
**Register item:** O2 (the anacrolix/torrent tag, [baseline](project-baseline.md#6-open-decision-register)); new: the lifecycle and ownership rules, and the v2-only answer  
**Requirements:** `AI-05`, `AI-07`, `STACK-03`

## Question

The engine adapter (018) and every engine task (009, 010, 012, 013) build on the engine's API. Until
007 they had only API documentation, which is not evidence. 006 had added anacrolix/torrent v1.61.0
for test fixtures only. Four things were open:

- Does that tag stay?
- Which calls create, run and end engine objects, and when is an operation really finished?
- Who may release a torrent that an import got back?
- What does the product do with a v2-only torrent, which the pinned engine panics on?

## Decision

- **The pin is anacrolix/torrent v1.61.0.** It is still the newest tag (2025-12-17); the previous one,
  v1.60.0, dates from 2025-09-03. `fixture.EngineModule` names it, and a test fails when the build
  uses another version. The signatures below are checked at compile time in
  `internal/torrentengine/lifecycle_test.go`.
- **Verified API map:**

  | Step | Call | Proven behavior |
  |---|---|---|
  | Client | `NewClient(*ClientConfig)` | Starts with the swarm's closed configuration (loopback only, every discovery path off). |
  | Spec | `TorrentSpecFromMetaInfoErr(*MetaInfo)` | Errors on an unreadable info dictionary. |
  | Add | `Client.AddTorrentSpec(*TorrentSpec) (t, new, err)` | `new=true` for a new infohash. For a known one it returns the same `*Torrent` with `new=false` and ignores the spec's `Storage`. A spec whose info bytes are not bencode gives an error: a new torrent is dropped, while an existing one is kept and keeps working. |
  | Metadata | `GotInfo()`, `Info()`, `Length()` | Closed at once for a torrent added with its metainfo. While the metadata is awaited: open, `nil`, `0`. |
  | Progress | `Stats()`, `BytesCompleted()` | Zero before any peer. The leecher's payload read and the seeder's payload written are each at least the length after a transfer. |
  | Remove | `Drop()`, then `Closed()` | `Closed()` closes within 1 s, also while metadata is awaited. The torrent leaves the client. Its files stay on disk. A second `Drop` is harmless. |
  | Shutdown | `Client.Close()`, then `Client.Closed()` | Always returns nil. It closes every torrent. A second `Close` is harmless. |

- **Completion contracts.** A method returning does not prove that the work finished:
  - `Drop` → wait for `Closed()`.
  - `Client.Close` → wait for `Client.Closed()`, then until every listen address refuses connections
    (`swarm.Refuses`; the engine closes its listeners in a goroutine), then until no engine goroutine
    is left (`swarm.WaitEngineStopped`).
  - `VerifyDataContext` → wait until no piece is queued, hashing or marking (`swarm.Settle`).
- **Ownership** (018 implements it):
  - The import that got `new=true` owns that `*Torrent`. It may `Drop` it when it is cancelled or
    fails.
  - An import that got `new=false` holds a shared handle. It must not `Drop` the torrent, must not
    assume its storage or spec options were applied, and must not change them. A `Drop` through the
    second handle ends the first owner's torrent.
  - Only the service that created the `*Client` closes it, once. Closing it is the only way to end its
    background work.
  - Never call `VerifyDataContext` on a dropped torrent (finding 9).
  - Set the upload and download switches with the `Torrent` methods, never with the `AddTorrentOpts`
    fields (finding 10). Never allow download right after disallowing it (finding 12), and expect
    the upload switch to stall now and then too (finding 14).
  - Before the engine sees a save folder, verify the full path plan against it (findings 8, 13).
  - Build a spec only from info bytes whose hash matches the infohash they are stored with
    (finding 11). A magnet's metadata arrives from peers and the engine consumes it itself, so no
    application code can check it first: fetch it into storage the application owns and wipes,
    verify it, then add the verified metainfo with the real folder (011).
- **v2-only torrents are rejected with a clear error before any engine call.** This covers a
  metainfo without a v1 infohash, and a magnet with `btmh` but no `btih`. The owner decided this on
  2026-10-16. It holds while the pinned engine panics on them (`panicif.Zero` in `AddTorrentOpt`).
  019 and 022 implement the rejection. The 006 tracking test turns red when a release fixes the
  panic, and the support is then decided again.
- **The early end-to-end path is a separate spike app, `EngineSpike.app`.** The owner decided this
  on 2026-10-16. It lives in `cmd/engine-spike`, `internal/enginespike` and `frontend/src/spike`,
  with its own bundle ID, page and bindings. The product never links it: the gate's product isolation
  step proves it. Task 034 deletes it. Feature doc: [engine spike](../tooling/engine-spike.md).
- **Two boundary exceptions, for the spike only, until 034.** `internal/enginespike` uses anacrolix
  types outside `internal/torrentengine`: it is a test instrument built on the swarm, which hands out
  engine torrents. `frontend/src/spike/bridge.ts` calls generated bindings outside
  `frontend/src/bridge`: they are the spike's own bindings, which the product's bridge never sees.
  The product isolation step is the control that keeps both out of the product binary.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Unreleased master of anacrolix/torrent | Fixes the v2-only panic | A pseudo-version with no release; against the dependency policy | Pre-releases and untagged code are forbidden |
| The spike inside the product app (like 003's runtime check) | Less gate work | The engine and test support would enter the product before 018; the task forbids its commands in the working build | The owner chose a separate app |
| Allowing v2-only torrents by patching around the panic | Full BitTorrent v2 | The next engine step (`checkPendingPiecesMatchesRequestOrder`) fails too; no release has the fix | Rejected with an error until a release fixes it |
| Pause and Resume of the spike on the leecher's download switch | Closer to a product pause | Stalls the transfer (finding 12) | The spike switches the seeder's upload |

## Verified grounds

The full record is in [report 007](../verification/007-engine-api-lifecycle-spike.md). The checks ran
on Apple M4, macOS 27.0.1, Go 1.27.2 and anacrolix/torrent v1.61.0, on the fixtures and the
loopback swarm of 006, with `-race`.

**Passed:**
- `go test -race -count=1 ./internal/torrentengine/...`: the lifecycle tests, twice in a row.
- `go test -race ./internal/enginespike` and the spike's TypeScript tests: 100%.
- `go tool devtool check`: `GATE PASSED`, with an instrumented run of both apps.
- The `.app` QA of the spike, six scenarios, recorded in the report.

Every lifecycle check ran in the swarm's closed configuration: loopback only, with DHT, trackers,
port forwarding and every other discovery path off. With those on, the shutdown checks are not
proven (018).

**Engine findings 9–14** (1–8 are in the [controlled-swarm ADR](controlled-swarm.md#verified-grounds)).
Findings 9–13 are each asserted by a test, so a release that fixes one turns that test red. Finding
14 cannot be caused on demand; it was measured with temporary scratch tests, and the spike's
`TestAStalledTransferIsWokenAndCounted` covers only the spike's remedy:

9. **`VerifyDataContext` on a dropped torrent returns while still holding the client lock.** Every
   later client call hangs. Proved in a child process.
10. **The `DisallowDataUpload` and `DisallowDataDownload` fields of `AddTorrentOpts` are never
    read.**
11. **Info bytes that do not match a v1-only infohash are accepted.** This happens from a spec and,
    over the wire, from a lying peer's metadata for a magnet. That peer's zero-length files are
    created as soon as the metadata arrives.
12. **Allowing download right after disallowing it stalls the transfer for good.** It happened in
    20 of 20 runs, and in 2 of 20 with a 1 s gap. The cause inside the engine is not established.
13. **When a piece fails its check, every existing file of that piece is renamed to
    `<name>.part`, and a download then overwrites it.** This includes a user's file that happens to
    sit at a torrent path. The engine writes the torrent's bytes into the renamed file and gives it
    its name back on completion, so the user's bytes are lost. With finding 8, an existing file at a
    zero-length path is first truncated and then renamed. This also reaches beside the torrent
    folder.
14. **The seeder's upload switch can stall too, rarely and only under CPU load.** With 14 busy
    processes, allowing upload 1.5 s after disallowing it left the transfer standing still in 1 of
    50 and 1 of 60 runs of the spike's sequence; without load, 0 of 52, and on the bare engine 0 of
    65. In the one stall caught with the engine's status, the
    seeder was not choking, the leecher was interested, and one request sat in the seeder's queue
    unserved. A second off and on served it. The cause inside the engine is not established. The
    spike wakes such a stall and counts it.

**Build facts of the spike.**
- `go list -deps` with cgo on finds cgo in `go-libutp` and crawshaw sqlite. `-tags
  nosqlite,disable_libutp` removes both. 009 decided: the product builds with `nosqlite,noboltdb`
  ([engine-storage](engine-storage.md)); `disable_libutp` stays with 018/026.
- `otool -L` lists only system frameworks, `libc++`, `libobjc`, `libresolv` and `libSystem`.
- DHT, uTP, the HTTP and UDP trackers, web seeds, WebTorrent with pion WebRTC, and UPnP are all
  compiled in. Only runtime switches disable them.

**Read in the source, not executed:**
- `NewDefaultClientConfig` without a `DataDir` stores data and its `.torrent.db` in the working
  directory.
- `Client.Close` does not wait for the DHT, tracker, port-forwarding and accept goroutines.
- `AddTorrentSpec` returns `new=false` also for a known v2 short hash.

## Consequences

- **018** builds the adapter on this API map, the completion contracts and the ownership rules. It
  reuses `fixture` and `swarm`, and replaces the "engine integration" `not_run` line with its own
  tests.
- **019/022** reject v2-only input. **011/022** hash received metadata against the requested
  infohash, and fetch metadata in storage that is not the user's folder.
- **020/023** check every torrent path for collisions before the add, not only the zero-length
  ones, because of finding 13.
- **010/013/025** own the product pause: they must not resume download right after disallowing it,
  must expect the upload switch to stall (finding 14), and must find a remedy that a test can
  measure.
- **029/030** check existing bytes with the application's own hashing before the engine sees a
  folder, and **031/032** include `.part` names in deletion lists (finding 13).
- **018** refuses to start with `GOPPROF` set, calls `classicio.Require` first in `main` (**009 replaced
  the rule "refuse `TORRENT_STORAGE_DEFAULT_FILE_IO`" with "require `classic`"**, see
  [engine-storage](engine-storage.md#file-io)), proves the shutdown with discovery on, and removes the
  anacrolix clause of the isolation step.
- **009** chose the storage and the build tags ([engine-storage](engine-storage.md)). `DataDir` is always
  set, and the default storage is `filestore.Refusing`.
- **034** deletes the spike app, its rows in the gate and its documents.
- An engine upgrade re-runs `internal/torrentengine` and `testutil/swarm`. Every finding test that
  turns red is decided again, and this ADR is updated.
