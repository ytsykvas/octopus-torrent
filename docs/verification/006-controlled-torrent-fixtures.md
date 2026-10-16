# Report 006: controlled torrents and a local swarm

**Task:** [006](../../product/tasks/006-controlled-torrent-fixtures.md)  
**Date:** 16 October 2026  
**Change version:** commits `d803ce3` (A), `650b157` (B), `0070799` and `b3ce3ba` (C), `23e0e9d`, `aa19254` and the final commit (D)

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
| D — documents, review, acceptance | done |

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
  - `newClient` is the test seam for the engine start; `pieceBusy` was added during stabilization.
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

### Stabilization (after the stage C commit)

Six runs of the whole package then gave four failures from two causes. Both are engine timing,
not test data:

5. **`Client.Close` returns before its listeners are closed.**
   - The engine closes each listener with `go s.Close()`, so a dial right after `Close` could still
     connect ("127.0.0.1:59407 still accepts connections").
   - `Refuses` now polls with a 1 s dial timeout until the address refuses, or reports at the
     deadline.
   - Ordered quit belongs to 028.
6. **`VerifyDataContext` returns before the result is stored.**
   - `pieceHashed` increments `numVerifies` and wakes the waiter before it marks the completion
     (`Marking`). A completion read right after it can be stale: in 3 of 40 isolated runs a seeder
     with a corrupted piece reported every piece complete.
   - `Seed` now waits, within its context, until no piece is queued, hashing or marking (`settle`).
   - The `pieceBusy` test seam covers settle's deadline branch.
   - Our adapter (018) needs the same wait.

The single-file `hybrid` regression was also fixed. The engine-layout change had made `addV1` use
the file list for `hybrid-single`. It now follows the spec, so the v1 part keeps single-file mode,
and `hybrid-single.torrent` is again byte-identical to stage B.

The corrupted-piece test now waits, up to the transfer limit, until every other piece has arrived,
and then checks with a 0.5 s wait. Before that it waited a fixed 3 s.

Results after the fixes:

| Run | Result |
|---|---|
| `TestACorruptedPieceIsTheOnlyOneMissing`, 60 iterations (before the wait change) | 60 pass |
| the same, 40 iterations (after the wait change) | 40 pass, 22.9 s |
| the swarm package, 8 separate runs | 8 pass, 14.0–14.4 s each |
| the swarm package, `-count=10` in one process | pass, 128.7 s |
| `./internal/torrentengine/...` with coverage, twice | pass, swarm 11.6 s, 100.0% in both packages |

One 40-iteration batch of the corrupted-piece test, run between the two fixes, failed, and its
output was not kept. The cause was not identified for certain. The likely cause is the fixed 3 s
wait, which the later change replaced. The later 100 isolated iterations and 18 package runs
passed.

## Stage D — review, fixes, documents

`/branch-review` was run with three agents:
- `code-reviewer`: no critical issue; quality notes.
- `tech-lead`: needs changes. The over-the-wire corruption was not tested, mmap retention was not
  recorded, and the evidence was incomplete.
- `security-reviewer`: one critical engine finding, a stale govulncheck statement, and missing
  tripwires.

Every finding was fixed or answered.

| Finding | Change |
|---|---|
| Engine truncates existing files at zero-length paths on add (security, critical) | Fixtures `invalid-empty-traversal`, `invalid-embedded-separator`, `invalid-dot-components`. `TestPinnedEngineTruncatesExistingFilesAtZeroLengthPathsOnAdd` proves truncation on synthetic files: a collision in the torrent folder, one `..` level, and an embedded separator. This is limitation 8 in the ADR, with notes in 018, 019, 020 and 023. |
| govulncheck fails once the swarm links the engine (security) | `pion/dtls/v3` → v3.1.10, `pion/stun/v3` → v3.1.7, `otel` → v1.47.0, each the latest version out of the cooldown. Recorded in both ADRs. `golang.org/x/net` stays open; see the blocker below. |
| No tripwire behind the config switches (security) | IP block list that lets only loopback pass. Refusing HTTP transport, HTTP and tracker dialers, tracker socket and DNS lookup, recorded in `Peer.Tripped`. `Add` refuses trackers, web seeds and DHT nodes. Tests: `TestTripwiresRefuseEveryPathOutOfTheSwarm`, and the transfer test asserts `Tripped() == ""`. |
| File helpers trust their directory (security) | `WritePayload` requires an absolute directory and never replaces a file (`O_EXCL`). `CorruptPiece` refuses data that is not the fixture's own, and refuses metainfo-only fixtures. |
| `GOPPROF` / `TORRENT_STORAGE_DEFAULT_FILE_IO` change the engine (security, tech lead) | `TestMain` refuses both. |
| Scripts rule gaps (security, code review) | The rule now also checks extensionless files with a shebang. It requires `set -eu` first, the `go build` line, and `exec` last. |
| Corruption over the wire untested (tech lead) | `TestACorruptedPieceReceivedOverTheWireIsNeverAccepted`: the seeder verified its data, then piece 1 changed on its disk. The leecher logs "piece 1 failed hash" and ends with exactly [1] missing. `Connect`'s `Trusted` is now documented: the engine bans by IP, and every peer shares 127.0.0.1. |
| Payload files stay mapped after `Close` (tech lead) | `TestPinnedEngineKeepsPayloadFilesMappedAfterClose` (lsof). This is limitation 7, with notes in 009, 018 and 028. "Resources are released" is narrowed to listeners and goroutines. |
| Package-wide leak check (tech lead) | `TestMain` runs `WaitEngineStopped` after all tests. |
| Personal data in diagnostics (tech lead) | The transfer test asserts that no engine log names the home folder or the user. |
| ADR claimed tests for limitations 5 and 6 (tech lead) | Reworded: 5 and 6 are timing races the harness waits out; 1–4, 7 and 8 are asserted. |
| Seams restored by hand; `Recorder` without cleanup; `Stop` twice panics (code review, tech lead) | `replace` restores a seam in `t.Cleanup`, and `TestErrorsAreReturnedNotHidden` is split into subtests. `Recorder.Stop` is idempotent (`sync.Once`), and the test registers it in `t.Cleanup`. |
| `NewPeer` leaked its storage when the client failed (code review) | The storage is closed on that path. |
| Unchecked `WalkDir`; wrong `LastPayload` comment; shared slices from `All()`; names `Engine`, `build`, `strings2any`; hard-to-read `CorruptPiece` loop; burst 0 (code review) | All fixed. `EngineModule`, `buildCatalog` and `pathList` are the new names; `All`/`Named` return copies; the burst-0 behavior is commented. |
| Rate limiting not asserted (code review) | The recorder test asserts that a quarter of `v1-large` takes at least 0.5 s at 256 KiB/s. |
| `IncompleteError.Completed` counts unverified chunks (tech lead) | Documented on the type and in the note for 018. |
| Empty-file coverage block in `WaitEngineStopped` under the gate (first full run) | One `pause(ctx, d)` helper replaces three `select` blocks (`settle`, `Refuses`, `WaitEngineStopped`). |

Not changed, with reasons:
- **Unreadable listed files** are skipped by `check.go`'s content reader. This is an existing gate
  behavior outside this task. Git lists only files the user can read.
- **Runner commands in `.conductor/settings.toml` and `.octopus/project.json`** are not scripts. They
  call the checked `.octopus/scripts/*.sh`.
- **`Connect` stays trusted, with no option.** No test needs untrusted peers yet; 010/018 have the
  note.

### `golang.org/x/net`: the owner's exception

- Linking the engine brings `golang.org/x/net/http2` into the program. Five HTTP/2 vulnerabilities
  in `golang.org/x/net` v0.59.0 therefore became reachable through the `net/http` client of
  `internal/devtool` (`OSSystem.Get`): GO-2026-6603, -6610, -6611, -6612, -6617.
- At 5bf2d40, before this task, `go tool govulncheck ./...` reports "No vulnerabilities found"
  (checked in a separate worktree).
- The fix is v0.60.0, published 2026-10-08, which is 8 days old. I asked the owner. On 2026-10-16
  they chose a one-time exception: take v0.60.0 now.
- The gate had no way to let it through, so the release-age step gained `releaseAgeExceptions`:
  - exact versions, each with the decision;
  - the step names them ("accepted younger");
  - the step fails once a version is old enough, so the entry is removed on 2026-10-22.
- **RED:** `TestReleaseAgeExceptionsAreExactAndExpire` failed to build (`c.ageExceptions
  undefined`, `undefined: releaseAgeExceptions`).
- **GREEN:** `internal/devtool` at 100.0%.
- After `go get golang.org/x/net@v0.60.0`:
  - `go tool govulncheck ./...` reports no symbol-level finding; the module-level finding
    GO-2026-5932 remains;
  - `go tool devtool check deps` → `DEPENDENCIES PASSED` ("358 Go modules are at least 14 days old;
    accepted younger: golang.org/x/net@v0.60.0").
- Recorded in the toolchain-versions ADR (policy row and version row), the quality-gate ADR,
  `.claude/docs/dependencies.md` and the controlled-swarm ADR.

### Full gate on 23e0e9d

`go tool devtool check` → **GATE FAILED**.

- **pass:** every step except one, including:
  - `go tests` (`-race`, raw counters);
  - `.app coverage run`;
  - `go coverage`: every block of every own Go file is covered (unit + `.app`);
  - frontend tests (19 files at 100%);
  - `scripts`, docs, plan;
  - release age (186 npm versions, 358 Go modules ≥ 14 days);
  - `npm audit`, updates;
  - single code version.
- **FAIL:** `govulncheck`, only the five `golang.org/x/net` v0.59.0 findings above.
- **not_run (not implemented yet):** engine integration (007, 018), test-bridge UI tests (035).

### Final gate (task closed)

`go tool devtool check` on the final tree → **GATE PASSED**.

- **pass:**
  - every step, including the `.app` coverage run and the merged Go coverage (every block of
    every own file);
  - frontend tests (19 files at 100%);
  - `scripts`;
  - release age: 186 npm, 358 Go; accepted younger: `golang.org/x/net@v0.60.0`;
  - `govulncheck`, `npm audit`, docs, plan, single code version.
- **not_run (not implemented yet, non-blocking):** engine integration (007, 018), test-bridge UI
  tests (035).

### Debt check

- No test is skipped, no ignore directive or lint disable was added, and no threshold changed.
- Engine defects are named tests or harness waits with a reason, and every one is noted in the task
  that owns its product decision.
- One planned expiry: the `golang.org/x/net@v0.60.0` exception fails the gate from 2026-10-22 until
  its entry is removed. That is intended.
- No other open item.
