# Report 006: controlled torrents and a local swarm

**Task:** [006](../../product/tasks/006-controlled-torrent-fixtures.md)  
**Date:** 16 October 2026  
**Change version:** in progress

## Session architecture plan

Approved by the owner on 16 October 2026 after one self-review pass (7.5 → 9 of 10).

- **Goal:** own reproducible test torrents (v1, v2-only, hybrid; valid and invalid) and a loopback
  swarm of anacrolix clients with time limits, cancellation, counters and guaranteed cleanup, for
  007, 009, 010, 011, 018 and 019.
- **Facts before coding:** anacrolix/torrent v1.61.0 (2025-12-17) is the latest stable tag; it builds
  only v1 metainfo, so v2 and hybrid are generated from its `merkle`, `FileTree` and `PieceLayers`
  primitives with our own BEP 47 pad files; v1 and hybrid transfer over loopback, a v2-only torrent
  panics on add (`panicif.Zero(infoHash)`, `client.go:1514`); every discovery path can be switched
  off and the library has no LSD.
- **Boundaries:** new packages `internal/torrentengine/testutil/fixture` (generator, catalog,
  invalid variants, committed `.torrent` files and `fixtures.json`) and
  `internal/torrentengine/testutil/swarm` (peers, waits, counters, recorder, diagnostics, leak
  check). The application binary does not import them. The gate gains two inventory rules: no Go
  under `testdata/`, and shell scripts only as hand-overs to `devtool`.
- **Decisions:** 006 adds anacrolix/torrent v1.61.0; 007 keeps the pin decision (O2). Transfers are
  verified for v1 and hybrid; v2-only is parse-only on the pinned engine, and a test tracks the
  panic.
- **Stages:** A — gate inventory rules; B — module and fixture generator; C — swarm and scenarios;
  D — documents, review, `done`.
- **Tests first:** listed per stage below; RED is recorded before the implementation.
- **Coverage:** `go tool devtool check` (Go unit + `.app` profiles; TypeScript unchanged).

## Stage log

| Stage | State |
|---|---|
| A — gate inventory rules | done |
| B — module and fixture generator | done |
| C — swarm and scenarios | done |
| D — documents, review, acceptance | in progress |

## Stage A — gate inventory rules

The quality-gate ADR left two inventory rules to this task.

- **RED** `go test ./internal/devtool/`:
  - `scripts_test.go` failed to build: `undefined: CheckScripts`.
  - The new `Go under testdata` case failed with the old message, "internal/x/testdata/gen.go has
    functions but is not in the coverage profile". The `Go test under testdata` case found no
    problem at all, because test files were skipped. That is a silent hole.
  - The step list in `TestCheckPassesTheGateWhenEveryStepPasses` lacked `scripts`.
- **GREEN:**
  - `goInventory` (`internal/devtool/gocov.go`) reports every `.go` file under a `testdata/`
    directory, test files included: "Go source under testdata/ is never built or measured; put it in
    a package".
  - `CheckScripts` (`internal/devtool/scripts.go`) accepts a `*.sh` only as a hand-over: `#!/bin/sh`,
    comments, `set -eu`, `go build -o build/bin/devtool ./cmd/devtool`, and
    `exec build/bin/devtool <words>`. Every other line is reported with its line number, and the
    missing pieces are named.
  - The new `scripts` step runs after `escape hatches`.
  - Both `.octopus/scripts/*.sh` files pass.
  - Result: `go test -race -count=1 ./internal/devtool/` → `ok`, coverage 100.0% of statements.
- **Refactoring:** none needed. `CheckScripts` follows the shape of `EscapeHatches` (sources map in,
  `file:line` problems out).
- **Fast gate:** `go tool devtool check fast` → every step `pass` except the expected `not_run` lines
  (the `.app` run, `main()` coverage, the network steps, the unbuilt tiers); `scripts` → pass.

## Stage B — module and fixture generator

- **Dependency:**
  - `go get github.com/anacrolix/torrent@v1.61.0 golang.org/x/time@v0.16.0`, then `go mod tidy`.
  - Release dates from the proxy: anacrolix/torrent v1.61.0, 2025-12-17 (the latest tag);
    golang.org/x/time v0.16.0, 2026-08-19 (the latest tag, up from the transitive v0.8.0).
  - No version already in `go.mod` changed.
  - `go tool devtool check deps` → `DEPENDENCIES PASSED`: "327 Go modules are at least 14 days old",
    `npm audit` pass, `govulncheck` pass.
  - `govulncheck -show verbose` lists one module-level finding, GO-2026-5932
    (`golang.org/x/crypto/openpgp` is unmaintained; no fix exists). It is not imported or called by
    our code. The module was in the build list before this task.
- **RED** `go test ./internal/torrentengine/...` failed to build: `undefined: WriteCatalog`,
  `build`, `All`, … (the package did not exist).
- **GREEN:** package `internal/torrentengine/testutil/fixture`.
  - `bencode.go` holds our own bencode encoder, so every byte of a fixture is decided by us.
  - `payload.go`: deterministic content (SHA-256 counter blocks), `WritePayload`, `FilePath`,
    `CorruptPiece`.
  - `generate.go`: v1 pieces over the padded stream, BEP 47 pad files for hybrids, BEP 52 file tree,
    merkle roots and piece layers built from the engine's `merkle` package.
  - `catalog.go`: `Fixture`, `All`, `Named`, `ParseStage`, `WriteCatalog`.
  - `specs.go`: 7 valid fixtures (`v1-single`, `v1-tree`, `v1-large`, `v2-single`, `v2-tree`,
    `hybrid-single`, `hybrid-tree`) and 7 invalid ones (truncated, wrong info type, short
    `pieces`, negative length, path traversal, hybrid mismatch, corrupted piece layer).
  - `testdata/` holds the 14 `.torrent` files and `fixtures.json`, generated with `-args -update`.
  - Tests:
    - two builds are equal;
    - the committed testdata equals a fresh build;
    - `Engine` equals the module in the build info;
    - infohashes are SHA-1/SHA-256 of the info bytes;
    - file checksums match;
    - the same payload set has the same bytes in every format;
    - the engine's piece count and length match;
    - `v1-tree` has a piece spanning two files;
    - hybrid files start on piece boundaries;
    - the empty file has no pieces root, and `ValidatePieceLayers` passes;
    - no tracker, web seed or creation data;
    - payload writing;
    - `CorruptPiece` changes exactly one byte, inside the named piece;
    - every error branch.
  - `go test -race -count=1 -cover ./internal/torrentengine/testutil/fixture` → `ok`, 100.0% of
    statements.
- **Deviation from the plan (recorded):** the tree set uses 32 KiB pieces instead of 16 KiB. With 16
  KiB pieces a v2 piece is a single merkle leaf, so the piece-layer padding (`SumMinLength`,
  `HashForPiecePad`) would never be exercised. The single and large sets keep 16 KiB.
- **Found during GREEN:** no fixture reached the `info` parse stage, and coverage showed it. I
  added `invalid-info-type` (`piece length` as a string), which `UnmarshalInfo` rejects. The
  `add`/`accepted` results of the invalid fixtures are confirmed by the swarm tests in stage C.
- **Refactoring:** none beyond the above. The generator writes its own bencode instead of using the
  engine's `Info` struct, because that struct cannot leave out `pieces root` for an empty file.

## Stage C — swarm and scenarios

- **RED** `go test ./internal/torrentengine/testutil/swarm` failed to build: `undefined: Options`,
  `WaitComplete`, `Count`, `Peer`, `NonLoopback`, … (the package did not exist).
- **GREEN:** package `internal/torrentengine/testutil/swarm`.
  - `peer.go`:
    - `NewPeer` builds a client listening on 127.0.0.1 only, TCP only (`DisableUTP`), no IPv6. DHT,
      trackers, PEX, port forwarding, web seeds and WebTorrent are off; the engine has no LSD.
    - Storage is `storage.NewFileOpts` in `Dir` with in-memory piece completion, so no database file
      is written.
    - Logs go to a buffer, and `Callbacks.NewPeer` records every peer address.
    - `Settings`, `ListenAddrs`, `SeenAddrs`, `Logs`.
    - `Add` returns an engine panic as an error.
    - `Seed` returns `*IncompleteError` for missing pieces; `Leech`, `AddInfohash`, `Connect`.
    - `Close` stops the client and storage, then checks the address with `Refuses`.
  - `wait.go`: `WaitInfo`, `WaitComplete`, and `IncompleteError` (completed, length, missing pieces,
    wrapped reason).
  - `counters.go`: `Count` (payload separately from all bytes); `Recorder` with `Mark` and `Stop`;
    `Log.FirstPayload`, `LastPayload`, `MarkAt`, `PayloadAfter`.
  - `diagnostics.go`: `NonLoopback` and `WaitEngineStopped` (polls the goroutine stacks for engine
    frames).
  - `newClient` is the only test seam.
- **Results:** `go test -race -count=1 ./internal/torrentengine/...` was green twice in a row. The
  swarm package took 13.5–14.0 s, under the 60 s budget. Coverage is 100.0% of statements for both
  packages. Under `-race`, no data race was reported in the engine or in our code.

### Scenarios and measured results

| Scenario | Fixture | Limit | Result |
|---|---|---|---|
| Single-file transfer | `v1-single`, `hybrid-single` | 20 s | pass. Bytes and SHA-256 match. The leecher's payload read and the seeder's payload written are ≥ the total. Only the payload files are in either directory. |
| Multi-file transfer | `v1-tree-no-empty`, `hybrid-tree` (with `empty.dat`) | 20 s | pass. Same checks; the zero-length file exists. |
| Only local peers | all four transfers | — | pass. Every discovery switch is off. One TCP listen address on 127.0.0.1. Every peer address seen is loopback. No non-loopback IP appears in either engine log (debug level). |
| No peers | `v1-single`; magnet of `v1-tree` | 2 s | pass. `IncompleteError` with 0 of 57344 bytes and pieces [0 1 2 3], and "no metainfo" for the magnet. Both return within 3 s. |
| Corrupted piece | `v1-tree-no-empty`, piece 1 flipped on the seeder | 3 s | pass. `Seed` reports piece 1 missing. The leecher ends with exactly [1] missing and 75000 − 32768 bytes complete. |
| Cancellation | `v1-large` at 256 KiB/s | — | pass. `context.Canceled` within 1 s of the cancel, with a partial transfer. |
| Measurement | `v1-large` at 256 KiB/s, sampled every 50 ms | 20 s | First payload at 51 ms. `DisallowDataUpload` on the seeder at 986 ms, after a quarter of the file. **0 payload bytes** after the stop within the next second. The number is recorded only; 010 interprets it. |
| Two runs in a row | `v1-single` | 5 s each | pass. Both former listen addresses refuse connections, and no engine goroutine is left after either run. `lsof -iTCP -sTCP:LISTEN` before and after the package shows no new listener. |
| Add stage of every fixture | all that parse | — | pass. Matches `fixtures.json`. |

### Engine findings (pinned anacrolix/torrent v1.61.0)

1. **v2-only torrents cannot be added.**
   - `Client.AddTorrentOpt` panics on a zero v1 infohash (`panicif.Zero`).
   - `Peer.Add` recovers the panic as an error, and `TestEveryFixtureIsAddedAsRecorded` asserts it.
   - This is recorded as `engine: add` for `v2-single` and `v2-tree`.
2. **The default file IO (mmap) cannot write a chunk that spans a zero-length file.**
   - The engine logs "writing received chunk 0: invalid argument", then "disabling data download".
     Piece 0 of `v1-tree` (docs/readme.txt, then the empty file, then clip.bin) never completes.
   - With `TORRENT_STORAGE_DEFAULT_FILE_IO=classic` (an environment variable read in the engine's
     `init`) the same transfer completes.
   - The control fixture `v1-tree-no-empty` transfers.
   - Hybrids are not affected: pad files put the empty file on a piece boundary.
   - `TestPinnedEngineCannotWriteAChunkAcrossAnEmptyFile` tracks it.
   - The storage decision is 009's.
3. **A BEP 52 single-file torrent is stored inside a folder of its name.**
   - Even with one entry, `FileTree.IsDir()` is true, so `hybrid-single` lives at
     `<dir>/single.bin/single.bin`.
   - `fixture.FilePath` follows this layout. File placement is 009/020's.
4. **The engine accepts two invalid fixtures.**
   - `invalid-hybrid-mismatch`: the v1 list disagrees with the file tree.
   - `invalid-path-traversal`: a `..` component.
   - Our parser (019) must reject both. Truncated bencode fails at load, a wrong info type at
     `UnmarshalInfo`, and a corrupted piece layer at `ValidatePieceLayers`. A short `pieces` and a
     negative length fail at add.
- **Refactoring:**
  - `RemoteAddrs(torrent)` was replaced by `Peer.SeenAddrs()`, because a completed transfer closes
    its connections and the torrent forgets its peers.
  - The fixture catalog gained `v1-tree-no-empty`.
  - `invalid-hybrid-mismatch` was re-recorded as `accepted`.
