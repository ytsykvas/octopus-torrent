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
| C — swarm and scenarios | in progress |
| D — documents, review, acceptance | planned |

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
