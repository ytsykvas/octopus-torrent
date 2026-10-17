# Controlled test torrents and a closed local swarm

**Status:** `accepted`  
**Date:** 2026-10-16  
**Task:** [006](../../product/tasks/006-controlled-torrent-fixtures.md)  
**Register item:** new; touches O2 (the anacrolix/torrent tag, [baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `AI-08`, `TR-02`

## Question

Engine tasks 007, 009, 010, 011, 018 and 019 need torrents whose every byte and hash is known, and a
swarm that cannot reach the internet. Neither existed, and nothing in the repository depended on
anacrolix/torrent. The open questions were:

- how to produce v1, v2 and hybrid metainfo, including invalid variants;
- which engine version to test against;
- how to keep the swarm closed and bounded in time;
- where generators may live under the quality gate's inventory rules.

## Decision

- **anacrolix/torrent v1.61.0 is added in 006.**
  - It is the latest tag (2025-12-17), past the 14-day cooldown. `golang.org/x/time` v0.16.0
    (2026-08-19) comes with it, for the rate limiters.
  - The engine is used only by the two test-support packages below, so the `.app` does not link it.
  - 007 still owns O2: it re-checks the API and the lifecycle on this pin, or moves it.
- **Fixtures** live in `internal/torrentengine/testutil/fixture`.
  - The package writes its own bencode, so every byte, of invalid variants too, is decided by us.
  - Payloads are built from 32-byte SHA-256 blocks of `"<set>/<path>:<i>"`.
  - Pieces: 16 KiB for single files, 32 KiB for the tree. With 32 KiB a v2 piece spans two merkle
    leaves, so the piece-layer padding is exercised.
  - v1 pieces are hashed over the stream with pads. Hybrid files are aligned with BEP 47 `.pad/<n>`
    files.
  - The BEP 52 file tree, merkle roots and piece layers are built with the engine's `merkle`
    package. An empty file has no `pieces root`.
  - Files are ordered bytewise by path components, as in the file tree.
  - The generated `.torrent` files and `fixtures.json` are committed in `testdata/`, and a test
    rebuilds and compares them. Payloads are never committed.
  - `fixtures.json` records each fixture's purpose, the full infohashes, every file's length and
    SHA-256, and the first engine stage that rejects it: `load`, `info`, `pieceLayers`, `add` or
    `accepted`.
- **The swarm** lives in `internal/torrentengine/testutil/swarm`. Each peer is a client with:
  - `127.0.0.1:0` over TCP only;
  - DHT, trackers, PEX, port forwarding, web seeds, WebTorrent, uTP and IPv6 off (the engine has no
    LSD);
  - file storage in a temporary directory and in-memory piece completion, so no database file;
  - the engine log captured;
  - every peer address recorded through `Callbacks.NewPeer`.

  Tripwires stand behind the switches:
  - an IP block list lets only loopback addresses be added or accepted;
  - the HTTP transport, the HTTP and tracker dialers, the tracker socket and the tracker DNS lookup
    refuse and record every call (`Peer.Tripped`);
  - `Peer.Add` refuses a metainfo that names a tracker, a web seed or a DHT node.

  Peers are connected only with `Connect`, which marks them trusted. Every local peer shares
  127.0.0.1 and the engine bans a peer that sent bad data by its IP, so one ban would cut off every
  local peer. A trusted peer is never banned, and its bad piece is requested again until the wait
  ends. Every wait takes a context and reports what is missing
  (`IncompleteError`). `Close` checks that the listen address refuses connections.
  `WaitEngineStopped` proves no engine goroutine is left. A `Recorder` samples payload counters for
  start, stop and residual-traffic measurements.
- **Formats.** Transfers are verified for v1 and hybrid. v2-only torrents are parse-only on the pin:
  adding one panics.
- **Engine limitations.** Limitations 1–4, 7 and 8 below are asserted by tests, so a version that fixes
  one turns its test red. Limitations 5 and 6 are timing races: the harness waits them out with
  `Refuses` and `Settle`, and no test can assert them deterministically.
- **Swarm tests run in the ordinary `go test -race ./...` step of every gate mode.** They use
  loopback only, not the network. The "engine integration" `not_run` tier stays for 007/018, which
  test our adapter.
- **Gate inventory rules:**
  - Go files under `testdata/` fail the gate, because `./...` never builds them.
  - Shell scripts may only hand over to `devtool` (the [quality-gate ADR](quality-gate.md#consequences)).

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| The engine's `Info.BuildFromFilePath` / `GeneratePieces` | Less code | v1 only. No pad files, no v2 tree. Its `FileTreeFile` always writes `pieces root`, even for an empty file | Cannot produce v2 or hybrid fixtures, or exact invalid ones |
| Third-party or downloaded test torrents | Real-world shapes | Unknown content, licences and trackers; not reproducible | Forbidden by AI-08 and the file rules |
| Clients in separate processes | Real process isolation | Slower; process lifecycle to manage; no in-process counters | In-process clients on separate ports give the same wire behavior with direct counters and a goroutine check |
| A build tag or `-short` for swarm tests | Faster TDD loop | Files invisible to the inventory; skipped tests are forbidden | The whole package takes about 12 s under `-race` |
| Setting `TORRENT_STORAGE_DEFAULT_FILE_IO=classic` for the tests | `v1-tree` would transfer | The variable is read in the engine's `init`, so it must be set before the process starts; it would hide a defect the product inherits | The tests use the engine's default and track the defect; 009 decides the product's storage |

## Verified grounds

The fixtures and swarm run on Apple M4, macOS 27.0.1, Go 1.27.2, anacrolix/torrent v1.61.0. Details
and numbers are in [report 006](../verification/006-controlled-torrent-fixtures.md).

**Passed:**

- **Determinism:** two builds are equal, and the committed `testdata/` equals a fresh build.
- **Hash identity:** the infohashes are SHA-1/SHA-256 of the info bytes.
- **Engine validation:** the hybrid and v2 piece layers pass `ValidatePieceLayers`, and the
  engine's piece counts match the manifest.
- **Byte-exact transfers:** `v1-single`, `hybrid-single`, `v1-tree-no-empty` and `hybrid-tree`
  arrive byte for byte.
- **Closed swarm:** only loopback addresses appear, in the peers and in the engine logs.
- **Bounded waits:**
  - with no peers, the wait ends within 3 s;
  - with a corrupted piece, exactly that piece is missing, both when it is corrupted on the seeder's
    disk before verification and when bad bytes arrive over the wire (the leecher logs
    "piece 1 failed hash" and never accepts them);
  - a cancelled wait returns within 1 s.
- **Clean runs:** two consecutive runs leave no listener or engine goroutine, and a `TestMain` check
  finds none after the whole package. Payload files stay mapped (limitation 7). No engine log names
  the home folder or the user.
- **Measured:** after `DisallowDataUpload` on the seeder of a 256 KiB/s transfer, 0 payload bytes
  arrived in the next second (50 ms sampling).

Engine limitations found and tracked by tests or handled by the harness:

1. **v2-only torrents cannot be added.** `Client.AddTorrentOpt` panics on a zero v1 infohash
   (`panicif.Zero`). The fix exists only on unreleased master.
2. **The default mmap file IO fails on a chunk that spans a zero-length file.** The engine logs
   "writing received chunk 0: invalid argument" and then disables the download, so `v1-tree` never
   completes piece 0. With classic IO the same transfer completes.
3. **A BEP 52 single-file torrent is stored inside a folder of its name.** The engine treats the
   file tree as a directory even with one entry.
4. **The engine accepts a hybrid whose v1 list disagrees with its file tree, and a `..` path
   component.**
5. **`Client.Close` closes its listeners in a goroutine,** after it returns.
6. **`VerifyDataContext` returns before the piece completion is stored** (`Marking`), so a state
   read right after it can be stale.
7. **The default mmap file IO never unmaps payload files.** After `Close`, and after every engine
   goroutine has ended, `lsof` still lists the seeder's payload as mapped in the process. The
   torrent's `Close` is a no-op for this IO. For the product this means the disk space of a file
   moved to the Trash is not freed, and an external volume cannot be ejected while the app runs.
8. **Adding a torrent truncates existing files at its zero-length paths.** 007 found that a failed
   piece check then also renames them, and any other existing file of that piece, to `<name>.part`
   (finding 13 in [engine-lifecycle](engine-lifecycle.md#verified-grounds)).
   - On `AddTorrent` (or when magnet metadata arrives), the engine creates every zero-length file
     with `O_TRUNC`, before any download or confirmation.
   - It checks only that the path stays inside the save folder. One `..` level, or a component with
     embedded separators (`"a/../../x"`), therefore reaches files beside the torrent folder.
   - Proven on synthetic files: a name collision inside the torrent folder (`v1-tree`'s
     `empty.dat`), `invalid-empty-traversal` and `invalid-embedded-separator` each emptied an
     existing file.
   - Two levels up is refused ("is not sub path of").
   - The finding comes from the security review of 006.

Findings 9–14 of 007 continue this list in the [engine-lifecycle ADR](engine-lifecycle.md#verified-grounds).

**Assumption not proven here:** behavior with a real remote peer. The swarm is loopback by design,
and real-network checks are not part of AI-08.

## Consequences

- Engine tests use `fixture` and `swarm` and do not write another harness. 018 reuses them for
  adapter tests.
- **007:**
  - re-checks the pin and decides the product answer to v2-only torrents (reject on import, or
    wait for a release with the fix);
  - must not read piece state right after `VerifyDataContext` without the `Settle` wait.
- **009** decides the storage:
  - the default mmap IO loses torrents with an empty file inside a chunk;
  - the default piece completion is SQLite through cgo when cgo is on, which is the case for Wails;
  - the `nosqlite,disable_libutp` tags avoid both C dependencies;
  - BEP 52 single files get an extra folder.
- **010** measures the pause and upload policy with `Recorder`.
- **019** must reject what the engine accepts: the hybrid mismatch, any `..`, `.` or empty
  component, and any component with a separator.
- **019/020/023** must validate the metainfo and the path plan **before** the engine sees the
  torrent, because adding it already truncates zero-length targets (limitation 8). The collision
  check (DEL-02) must include zero-length files, and paths are compared after cleaning the joined
  path, not only by component.
- **028** must wait for listeners to close on quit, and must not report files released while they
  are still mapped (limitation 7).
- **Deletion (031/032, DEL-01–05) and 009:** with the default IO, a removed or trashed payload
  stays mapped until the process exits.
- **010/018:** `Connect` trusts peers. A test that needs the engine to ban a bad peer must use peers
  on different loopback addresses, and should expect the bad piece to be re-requested in a hot
  loop until then.
- Upgrading the engine changes `fixture.Engine`. Its test then fails until the recorded stages and
  the tracked limitations are re-checked.
- The release-age and govulncheck checks now cover the engine's build list (358 modules at the end
  of 006).
  - Vulnerabilities in the engine's WebRTC dependencies became reachable from our code, so the
    transitive `github.com/pion/dtls/v3` (v3.0.3 → v3.1.10, 2026-09-26; GO-2026-6165) and
    `github.com/pion/stun/v3` (v3.0.0 → v3.1.7, 2026-08-18; GO-2026-6163) are force-upgraded to
    the latest versions out of the cooldown. MVS raised `pion/logging` to v0.2.4 and
    `wlynxg/anet` to v0.0.5, and added `pion/transport` v4.1.0 and v5.0.0.
  - Linking the engine also makes five HTTP/2 vulnerabilities in `golang.org/x/net` v0.59.0
    reachable (GO-2026-6603, -6610, -6611, -6612, -6617). The trace goes through the standard
    `net/http` client used by `internal/devtool`, because the engine brings `golang.org/x/net/http2`
    into the program. At 5bf2d40, before 006, govulncheck finds nothing. The fix is v0.60.0
    (2026-10-08). On 2026-10-16 the owner decided to take it before the 14-day cooldown ends. It is
    the first entry in the gate's `releaseAgeExceptions`, which expires on 2026-10-22.
  - `go.opentelemetry.io/otel` v1.38.0 → v1.47.0 (2026-10-02) for GO-2026-5506 (imported by the
    engine, not called).
  - A module-level finding in `golang.org/x/crypto/openpgp` (GO-2026-5932, no fix) is not
    imported.
