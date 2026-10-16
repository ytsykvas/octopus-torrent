# Report 007: the anacrolix API and lifecycle on one version, and an end-to-end spike

**Task:** [007](../../product/tasks/007-engine-api-lifecycle-spike.md)  
**Date:** 16 October 2026  
**Change version:** branch `task/007-engine-lifecycle` (commits are listed per stage)

## Session architecture plan

The owner approved this plan on 16 October 2026, after two self-review passes (6.5 → 8 → 9 of 10).
The owner asked for at least 9.

- **Goal:** prove on one pinned engine version:
  - creating, using and shutting down the engine objects the application needs;
  - who owns a torrent that an import gets back;
  - one early end-to-end path, Go → Wails → React, with real bytes.
- **Facts before coding:**
  - anacrolix/torrent v1.61.0 (2025-12-17) is still the newest tag.
  - `AddTorrentSpec` returns the same `*Torrent` with `new=false` for a known infohash. It ignores
    `Storage`.
  - The `DisallowDataUpload` / `DisallowDataDownload` fields of `AddTorrentOpts` are never read.
  - `VerifyDataContext` after `Drop` keeps the client lock.
  - `Client.Close` always returns nil and does not wait for its background goroutines.
  - Wails deletes `<wailsjsdir>/wailsjs/go` when it generates bindings, and copies `frontend/dist`
    over any other embedded `dist`.
- **Owner decisions:**
  - The prototype is a separate spike `.app` (`cmd/engine-spike`), and the gate learns more than
    one Wails app. 034 deletes the spike.
  - v2-only torrents are rejected with a clear error before the engine sees them.
- **Boundaries:**
  - engine contract tests in `internal/torrentengine` (test-only until 018);
  - `swarm.Peer.Client` and `swarm.Main`;
  - the spike service `internal/enginespike` and the spike app `cmd/engine-spike`;
  - the spike UI in `frontend/src/spike`, with its bindings in `frontend/spike/wailsjs`;
  - gate changes: multiple Wails apps, and a "product isolation" step.
  - The product binary does not change, and it does not link the engine.
- **Stages:**
  - A — engine contract tests;
  - B — gate;
  - C — spike Go;
  - D — spike TypeScript;
  - E — QA, evidence, documents, review, `done`.
- **Tests first:** listed per stage below. Each RED is recorded before its implementation.
- **Coverage:** `go tool devtool check`, Go unit plus both `.app` profiles, and every TypeScript file
  under `frontend/src`.

The full approved plan, with its self-assessment, is kept in the session. Its decisions are in the
ADR `engine-lifecycle`, written in stage E.

## Stage log

| Stage | Commit | Result |
|---|---|---|
| A — engine contract tests | — | `not_run` |
| B — gate: multiple apps, product isolation | — | `not_run` |
| C — spike Go | — | `not_run` |
| D — spike TypeScript | — | `not_run` |
| E — QA, evidence, documents, review | — | `not_run` |

## Stage A — engine contract tests

**Environment:** Apple M4, macOS 27.0.1, Go 1.27.2, anacrolix/torrent v1.61.0. Every command runs
from the repository root.

**RED:**

| Command | Result |
|---|---|
| `go test ./internal/torrentengine/testutil/swarm -run 'TestMain\|TestClient'` | `fail` as expected: `undefined: mainLimit`, `undefined: Main`, `p.Client undefined` |
| `go test -race -count=1 ./internal/torrentengine/` (first version) | `fail`. The cause was a wrong expectation, not a missing behavior: `AddTorrentSpec` accepted info bytes of another torrent with no error. This contradicted the planned failed-add test, so the source of `hashInfoBytes` was re-read and the finding recorded below. |
| same, the peer-metadata scenario (first version) | `fail` with `no metainfo: context deadline exceeded`. The lying peer rejected connections ("don't want conns right now"), because a torrent that wants nothing accepts none. The test now asks that peer for all pieces. |

**GREEN:**

| Command | Result |
|---|---|
| `go test -race -count=1 -cover ./internal/torrentengine/testutil/swarm` | `pass`, 100.0% of statements, 12.5 s |
| `go test -race -count=1 -v ./internal/torrentengine/`, run twice | `pass` both times, 4.9–5.1 s |
| `go test -race -count=1 ./internal/torrentengine/...` | `pass` |

**Refactoring:** the 006 `TestMain` body became `swarm.Main(Runner) int`. The swarm and the
torrentengine test packages, and later the spike, share it instead of copying the guard.

### Verified lifecycle (anacrolix/torrent v1.61.0)

| Test | Proves |
|---|---|
| signatures `var` block | `NewClient`, `TorrentSpecFromMetaInfoErr`, `AddTorrentSpec`, `AddTorrentOpt`, `AddTorrentInfoHash`, `Torrent`, `Torrents`, `Client.Close`, `Client.Closed`, `GotInfo`, `Info`, `Stats`, `BytesCompleted`, `Length`, `DownloadAll`, `Drop` and `Torrent.Closed` keep the signatures the later tasks use. Checked at compile time. |
| `TestATorrentLivesFromAddToDropAndTheClientFromNewToClose` | A new spec gives `new=true`, and `GotInfo` is already closed. The counters start at zero. The transfer completes with `BytesCompleted == Length`, the leecher's payload read ≥ length, and the seeder's payload written ≥ length. `Drop` closes `Closed()`, removes the torrent from the client and leaves the file. `Client.Close` returns nil and closes `Client.Closed()`. The addresses refuse connections, and no engine goroutine is left. |
| `TestAddingAKnownTorrentReturnsTheSameTorrentWithNewFalse` | A second `AddTorrentSpec` returns the same `*Torrent` with `new=false` and no error. Its `Storage` is ignored: nothing is written there. `AddTorrentInfoHash` behaves the same way. A `Drop` through the second handle closes the first one. |
| `TestAFailedAddIsAnErrorAndLeavesAnExistingTorrentWorking` | An info dictionary that cannot be read is an error from `TorrentSpecFromMetaInfoErr` and from `AddTorrent`, and adds nothing. Info bytes that are not bencode: a new torrent gets an error and is dropped, while an existing torrent gets an error, stays open and still completes. |
| `TestATorrentWaitingForMetadataIsDroppedAndClosedPromptly` | With no peers, `GotInfo` stays open, `Info()` is nil and `Length()` is 0. `Drop` closes `Closed()` within 1 s. `Client.Close` during the wait returns within 1 s and closes the torrent. Nothing is left running. |
| `TestClosingTheClientRepeatedlyDuringTransfersLeavesNothingRunning` | Three cycles, each with a rate-limited transfer in progress and a torrent waiting for metadata. `Client.Close` closes every torrent and `Client.Closed()`. A second `Close` and a `Drop` after `Close` are harmless. The addresses refuse connections, and no goroutine is left. The three cycles take 0.9 s. |

### Engine findings (numbered after the eight of 006)

9. **`VerifyDataContext` on a dropped torrent keeps the client lock.** It returns, and the next call
   that takes the lock never returns. `TestPinnedEngineDeadlocksOnVerifyAfterDrop` proves it in a
   child process: the child logs "VerifyDataContext returned", then stays inside `Torrents()` until
   the parent kills it after 2 s.
10. **The `DisallowDataUpload` field of `AddTorrentOpts` is ignored.** The field is never read. A
    seeder added with it still uploads, and the leecher completes
    (`TestPinnedEngineIgnoresTheUploadSwitchOfTheAddOptions`).
11. **Info bytes that do not match a v1-only infohash are accepted.**
    - `hashInfoBytes` (`torrent.go`) returns an error only in the branches where the bytes match the
      infohash or its short v2 form. Bytes of another torrent fall through with no error.
    - `TestPinnedEngineAcceptsInfoBytesThatDoNotMatchTheInfohash` proves it twice: from a spec, and
      over the wire. In the wire case, a magnet leecher asked for `v1-single` accepted a lying
      peer's `v1-tree` info dictionary under `v1-single`'s infohash. On metadata arrival, before any
      download, it created that dictionary's zero-length file in its folder.
    - Together with finding 8 (zero-length paths truncate existing files), a peer can make a magnet
      import create or truncate files that the user never confirmed.
    - The defence for 011 and 022: hash the received info bytes against the requested infohash
      before the torrent reaches the real save folder. The metadata wait must not use the user's
      folder as its storage.

## Stage B — gate: multiple Wails apps, product isolation

**RED:** `go test ./internal/devtool` did not compile, as expected:
- `too many arguments in call to appCoverageRun` (the new `module` parameter);
- `undefined: wailsApp`, `undefined: wailsApps`;
- the old single-app `CheckGoCoverage` signature.

**GREEN:**

| Command | Result |
|---|---|
| `go test -count=1 -cover ./internal/devtool` | `pass`, 100.0% of statements |
| `go tool devtool check fast` | `GATE INCOMPLETE`, as `check fast` always reports. The `.app` run and network steps are `not_run`. The first run failed on the stage A report's link to the not yet written ADR, and on `go.mod`, which `go mod tidy` (run by the Wails release build) changed. The lifecycle tests import `github.com/anacrolix/chansync/events` directly, so v0.7.0 moved from indirect to direct with the same version. The link now waits for stage E, and the `go.mod` change is committed. |
| `go tool devtool check` | `GATE PASSED` in 43 s. The product's instrumented build now runs as `env -C . GOFLAGS=… -coverpkg=octopustorrent/… go tool wails build -s -trimpath`. |

**What changed in the gate:**
- **Wails apps.** Every Wails project is one row of `wailsApps`: name, directory, bundle, bundle ID,
  main file and bindings directory. The product is the first row.
  - The `.app` run builds, opens and quits every app in turn, with `env -C <dir>`.
  - It accepts only counter files that carry the pid of the app it launched, so an earlier app's
    counters cannot satisfy a later one.
  - Every message names the app.
- **Bindings.** The step regenerates each app's bindings in its own directory and compares them by
  path.
- **Coverage.** `CheckGoCoverage` requires the main file of every app in the `.app` profile.
- **Product isolation (new step).** `go list -deps -f {{.ImportPath}} .` of the product must contain
  no `octopustorrent/internal/enginespike`, no `/testutil/` package and no `github.com/anacrolix/`
  package. The real product links 226 packages and none of these.
- **"engine integration"** now names only task 018.
- **Table check.** A test reads the committed `wails.json`, both plists, the main file and the
  bindings directory of every row, so the table cannot drift from the projects.

## Stages C and D — the spike app (Go and TypeScript)

The two stages land in one commit. `cmd/engine-spike` embeds the page that the TypeScript stage
builds, so neither half passes the gate alone.

### First step: a Wails project in a subdirectory

`cmd/engine-spike` had a `wails.json` with `frontend:dir: ../../frontend` and
`wailsjsdir: ../../frontend/spike`, and a probe `main.go` that bound an empty `API`.
`go tool wails build -s -trimpath` in that directory gave `pass`:
- `EngineSpike.app` was built in 3 s;
- the bindings appeared only in `frontend/spike/wailsjs/`, with no file of the product changed.

The probe `main.go` was then deleted, and the real code was written test-first.

### RED

| Command | Result |
|---|---|
| `go test ./internal/enginespike/` | `fail` as expected: `undefined: Transfer`, `undefined: Event`, … |
| `npm --prefix frontend test -- src/spike` | `fail` as expected: the modules `./bridge`, `./SpikeApp` and `./main` do not exist |
| first `go test -race -count=1 -cover ./internal/enginespike` | `pass`, but only 98.1%. The two uncovered blocks pointed at design faults, not missing tests: an ignored `fixture.Named` error, and a `Leech` error that could not be reached. The fixture name became a seam. The leecher now adds the torrent before the seeder seeds. Each error branch now has a real cause: an unknown fixture, an unreadable metainfo, and the v2-only fixture the engine panics on. |

### GREEN

| Command | Result |
|---|---|
| `go test -race -count=2 -cover ./internal/enginespike` | `pass`, 100.0% of statements |
| `go test -race -count=1 -cover ./cmd/engine-spike` | `pass`. The only uncovered block is `main()`, which the `.app` run covers. |
| `npm --prefix frontend test -- src/spike` | `pass`, 20 tests |
| `npm --prefix frontend run typecheck`, `npm --prefix frontend run lint` | `pass` (after typing the mocks and turning the bridge's members into properties for `unbound-method`) |
| `go tool devtool check` | `GATE PASSED`. The `.app` run launched and quit OctopusTorrent and EngineSpike. Bindings match for both apps (6 files each). 22 own TypeScript files are at 100%, and every Go block is covered. Product isolation passed with 226 packages. |

### Two more engine findings, made while the spike was tested

12. **Allowing download right after disallowing it stalls the transfer for good.**
    - **How it showed up:** the spike's command-order test hung in 3 of 10 runs, with the leecher at
      0 bytes and one peer connected.
    - **Reproduced on the bare engine** (`v1-large` at 256 KiB/s, after the first payload, 20 runs
      each; temporary scratch tests, not committed):

      | Sequence | Stalled |
      |---|---|
      | `DisallowDataDownload` then `AllowDataDownload` at once | 20 of 20 |
      | the same with 1 s between them | 2 of 20 |
      | the same with 5 ms between them | 0 of 20 |
      | a later disallow/allow pair 50 ms apart, after a stall | 0 of 20 (it wakes the transfer up) |
      | the seeder's `DisallowDataUpload` / `AllowDataUpload`, at once, 1 s apart, or twice | 0 of 60 |

    - **The cause inside the engine is not established.** 010 owns the product's remedy.
    - `TestPinnedEngineStallsWhenDownloadIsAllowedRightAfterDisallowing` asserts the deterministic
      case.
    - **What the spike does instead:** its Pause and Resume switch the seeder's upload, never the
      leecher's download. They call the engine only when the state changes.
13. **When a piece fails its check, the engine renames each existing file of that piece to
    `<name>.part`.** This is its default part-file mode.
    - **How it showed up:** the 006 test `…TruncatesExistingFilesAtZeroLengthPaths…` failed once in
      the gate. The victim file was gone instead of empty: `MarkNotComplete` → `onFileNotComplete` →
      rename (`storage/file-piece.go:288`) raced the assertion.
    - **After the piece checks settle, the outcome is deterministic** (5 of 5):
      - every zero-length victim is truncated and renamed to `.part`, beside the torrent folder too;
      - an unrelated, non-empty file of the user at a torrent path (`tree/docs/readme.txt`) loses its
        name. It becomes `readme.txt.part` with its bytes kept.
    - The 006 test now waits for the checks (`settle`) and asserts exactly this. It is renamed
      `TestPinnedEngineTruncatesAndRenamesExistingFilesAtTorrentPathsOnAdd`.
    - **For 020/023:** the collision check must cover every file of the torrent, not only the
      zero-length ones, and must run before the add.

### What the spike is

- **`internal/enginespike`.**
  - `Service` owns the lifecycle: `Startup`, `Shutdown`, two swarm peers, the spike's own temporary
    folder, and a 250 ms ticker.
  - `API` is the only bound type: `Start`, `Pause`, `Resume`, `Snapshot`.
  - One mutex orders the commands, the ticks and `Shutdown`. Every published `Transfer` takes the
    next revision.
  - `Shutdown` runs once. It stops the ticker, closes both peers (their addresses refuse
    connections) and removes only the folder this service created.
- **`cmd/engine-spike`.** `run(start, stderr, svc)` binds only the API and wires `OnStartup` and
  `OnShutdown` (5 s). `main` is one line.
- **The `wails.json` and plists** carry the bundle ID `dev.tsykvas.octopustorrent.enginespike`. The
  app icon is a copy of the product's.
- **`frontend/src/spike`.**
  - `bridge.ts` is the only caller of `frontend/spike/wailsjs`. It decodes every answer and event with
    the contract decoders.
  - `SpikeApp.tsx` subscribes before it takes the snapshot and keeps only the highest revision.
  - `main.tsx` is the entry point.
- **Build.** `vite.spike.config.ts` builds `frontend/spike/index.html` into
  `cmd/engine-spike/assets` (ignored by Git). `npm run build` builds both pages. The product's
  `dist` has one bundle and nothing of the spike.
- **Shared example.** `internal/enginespike/testdata/transfer.json` holds the event name and one
  Transfer. The Go and TypeScript suites both check against it.
