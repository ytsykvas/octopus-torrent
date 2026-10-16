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
| B — module and fixture generator | in progress |
| C — swarm and scenarios | planned |
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
