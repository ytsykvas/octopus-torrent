# Report 007: the anacrolix API and lifecycle on one version, and an end-to-end spike

**Task:** [007](../../product/tasks/007-engine-api-lifecycle-spike.md)  
**Date:** 16–17 October 2026  
**Change version:** branch `task/007-engine-lifecycle` (commits are listed per stage)

## Session architecture plan

The owner approved this plan on 16 October 2026, after two self-review passes (6.5 → 8 → 9 of 10).
The owner asked for at least 9.

- **Goal:** prove on one pinned engine version:
  - creating, using and shutting down the engine objects the application needs;
  - who owns a torrent that an import gets back;
  - one early end-to-end path, Go → Wails → React, with real bytes.
- **Facts before coding** (read in the source while planning; the sections below say which were
  then executed and which stay source reading only):
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
ADR [engine-lifecycle](../decisions/engine-lifecycle.md).

## Stage log

| Stage | Commit | Result |
|---|---|---|
| A — engine contract tests | `c26dbf4` | `pass` |
| B — gate: multiple apps, product isolation | `e5ee7ad` | `pass`, `GATE PASSED` |
| C — spike Go | `06a3e92` | `pass` |
| D — spike TypeScript | `06a3e92` | `pass`, `GATE PASSED` |
| E — QA, evidence, review fixes, documents | the stage E commit | `pass`, `GATE PASSED` (below) |

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

## Stage E — QA, evidence, documents, review

### `.app` QA of `EngineSpike.app`

The `macos-qa` agent ran on 17 October 2026, 00:11–00:14. It used the `.app` that the gate had built
at commit `06a3e92` and read the window text through Accessibility. It took no screenshots and used
the Apple Event quit only, never a signal. The transfer lasts about 16 s, so four instances were
used (pids 32103, 33014, 34061 and 34773). Before the run, no `$TMPDIR/octopus-engine-spike-*`
folder existed and no EngineSpike was running.

| # | Scenario | Observed | Result |
|---|---|---|---|
| 1 | Open | `State: idle`, `0 / 0 bytes (0%)`, `0 peers, 0 engine clients started`, the buttons Start, Pause, Resume and Reload UI | `pass` |
| 2 | Start | After 2 s: `State: running`, `1 peer, 2 engine clients started`. Then 278528 (26%), 606208 (57%), 737280 (70%) and 884736 (84%) on later reads. Listeners: two, both on `127.0.0.1`. The one connection runs between them. No UDP socket. One folder, `$TMPDIR/octopus-engine-spike-3133238330`. | `pass` |
| 3 | Pause and Resume | `State: paused`, `278528 / 1048576 bytes (26%)`, unchanged after more than 3 s. After Resume: 851968 (81%), then 1032192 (98%). A second instance gave the same pattern at 14%. | `pass` |
| 4 | Reload UI | While paused: the state, the bytes and `2 engine clients started` came back unchanged. While running: progress continued over six reads (10% → 56%), still with 2 clients, and ended at `State: complete`, `1048576 / 1048576 bytes (100%)`. A reload when complete kept `complete` and 2 clients. | `pass` |
| 5 | Quit | `osascript … to quit` printed the expected `-128`. The process was gone in about 1 s on every instance, `lsof -i` of its pid was empty, and no `octopus-engine-spike-*` folder was left. | `pass` |
| 6 | Isolation | `go version -m build/bin/OctopusTorrent.app/Contents/MacOS/OctopusTorrent \| grep -c anacrolix` printed `0`. `frontend/dist/assets` holds one file, `index-BhS6OE5t.js`. | `pass` |

The agent also noted these, none of which contradicts the task:
- After Resume, the transfer briefly ran faster than 64 KiB/s (about 440 KB in 3 s). The rate
  limiter's bucket filled during the pause. The "about 16 s" holds only without a pause.
- About one read in six, right after Start or Reload, had no bytes line. This is not explained:
  the page renders that line whenever it has a transfer. It never lasted; the next read had it.
- Start and Pause in the `complete` state change nothing. Start returns the finished run, as
  documented.

### Build evidence of the spike

The commands ran from the repository root after the gate's build at `06a3e92`. Each exited 0.

| Command | Result |
|---|---|
| `ls -l cmd/engine-spike/build/bin/EngineSpike.app/Contents/MacOS/EngineSpike` | 22,930,560 bytes |
| `otool -L` of the same binary | `libresolv`, `libc++`, `libobjc`, `libSystem`, and the frameworks UniformTypeIdentifiers, Foundation, Cocoa, WebKit, UserNotifications, AppKit, CoreFoundation, Security and CoreGraphics. Nothing outside the system. |
| `go version -m` of the same binary | `-tags=desktop,wv2runtime.download,production`, `CGO_ENABLED=1`; 86 modules, among them `github.com/anacrolix/torrent v1.61.0` |
| `go version -m build/bin/OctopusTorrent.app/Contents/MacOS/OctopusTorrent` | 12 modules, no `anacrolix` |
| `env -C cmd/engine-spike go list -deps -f '{{if .CgoFiles}}{{.ImportPath}}{{end}}' .` | `runtime/cgo`, `github.com/anacrolix/go-libutp`, `github.com/go-llsqlite/crawshaw/c`, `github.com/go-llsqlite/crawshaw` |
| the same for the product (`.`) | none. With the build's tags (`-tags desktop,production`) the product adds `runtime/cgo` and the two Wails darwin packages, which the spike has too: the engine's cgo is only the three spike rows above. |
| `env -C cmd/engine-spike go list -deps .` filtered for transports | `dht/v2`, `go-libutp`, the HTTP and UDP trackers, `webseed`, `webtorrent`, `upnp`, and `pion` ICE and WebRTC |

### Verified, not yet proven, read in the source only

**Verified by 007:** the lifecycle tests of stage A, findings 9–13 with their tests, the spike's Go
and TypeScript tests, the gate's multi-app run, and the `.app` QA above. All of it ran in the swarm's
closed configuration: loopback only, DHT, trackers, port forwarding and every other discovery path
off.

**Not yet proven by 007:**
- **Stopping the upload as a product policy** (010, TR-01, TR-08). The spike's Pause switches one
  seeder's upload in a closed swarm, and the bytes hold for a few seconds. That is not a measurement
  of the switch, and finding 14 shows it can stall.
- **Pausing and resuming the download.** Finding 12 shows the obvious sequence stalls. The remedy is
  010's.
- **Restoring data and state after a restart:** the storage, its piece completion across a restart,
  and the library (009, 029, 052). The swarm keeps piece completion in memory, and nothing was
  restarted.
- **Shutdown with discovery on.** No leftover goroutines was proven only with DHT, trackers and
  port forwarding off (018).
- **Magnet metadata in storage that is not the user's folder:** 011.

**Read in the source only, not executed:**
- `NewDefaultClientConfig` without a `DataDir` stores data and its `.torrent.db` in the working
  directory.
- `Client.Close` does not wait for the DHT, tracker, port-forwarding and accept goroutines. The
  tests only see the outcome: `WaitEngineStopped` with every discovery path off. "`Client.Close`
  always returns nil" was executed, but only in that configuration.
- `AddTorrentSpec` returns `new=false` also for a known v2 short hash.
- `-tags nosqlite,disable_libutp` removes both cgo packages. It was not built; 009 decides.

### Review (`/branch-review`) and what changed

Three reviewers read the branch against `main` on 17 October 2026: code-reviewer (architecture and
quality), security-reviewer (data safety and attack surface) and tech-lead (design and readiness).
None found a critical defect in the code. Every finding was fixed in stage E, or the decision not
to fix it is recorded here.

| Finding | Reviewer | Resolution |
|---|---|---|
| `internal/enginespike` uses anacrolix types outside `internal/torrentengine` | code | Recorded as an explicit exception until 034 in the ADR and the feature doc, together with the spike's own bindings outside `frontend/src/bridge` (security L7). The product isolation step is the control. Wrapping the engine type in the swarm would only rename it. |
| The stage log said `not_run`; no "Execution result" | code, tech-lead | Stage log filled; the task's handoff added. |
| The report did not separate verified, not proven and source only | tech-lead | The section above. |
| The ADR listed the `.app` QA as passed before it ran | tech-lead | The QA ran (above), so the claim now holds. |
| The states were listed in Go and in TypeScript separately | code | `transfer.json` lists them once; both suites compare their list with it. |
| The shared example test discarded JSON errors | code | It fails on an unreadable example. |
| Close errors discarded in the lifecycle tests | code | `Client.Close` and the second storage's `Close` are asserted. |
| The deadlock test gave the child a fixed 2 s from its start | code, tech-lead | It now waits up to 20 s for "VerifyDataContext returned", then a 2 s grace, and kills the child in a `defer`. The child has its own `-test.timeout=60s` (security L1). |
| The finding 12 test waited 3 s, less than a resumed transfer needs | tech-lead | 8 s, so a fixed engine turns it red. |
| Start held the mutex across the seeder's check; a quit waited for it | code, tech-lead, security | The service has a lifetime context that Shutdown ends before it takes the mutex. Start derives its waits from it. A failed Start's clean-up is bounded (5 s). New test: Shutdown during a Start returns within 1 s. |
| The only destructive call had no negative test | security | New tests: nothing beside the spike's folder is touched, after a run and after a failed Start. Hardened: only `seeder` and `leecher` are removed recursively; the folder itself with `os.Remove`, so a folder with foreign content is kept and reported (tested). |
| Finding 13 understated: the renamed file is then overwritten | security | New test `TestPinnedEngineOverwritesAUsersFileAtATorrentPathWhenItDownloads`. ADR and notes say so; notes added to 018, 029, 030, 031 and 032. |
| Finding 11's defence was worded as impossible for magnets; wrong tasks | security | 011, 022 and the ADR now describe isolated metadata storage; notes for 018, 023 and 029; 019's note says a `.torrent` cannot mismatch. |
| A refused command's alert stayed on screen | code, tech-lead | A successful reply clears it (tested). |
| The QA anomaly was explained without evidence | code | Reworded as not explained. |
| Isolation listed the product without the release tags | tech-lead, security | `go list -tags desktop,wv2runtime.download,production -deps .` |
| `pgrep -f` pattern was not anchored | security | Anchored at the start and quoted. |
| Ctrl-C and Automation docs named only OctopusTorrent | security | Both apps named. |
| `chansync` had no row in the version table | security | Row added (v0.7.0, 2025-08-05, checked with `go list -m -json`). |
| `GOPPROF` is read by a linked engine | security | Notes for 018 and 009; the spike does not refuse it (feature doc says why). |
| 018's note did not name `isolation.go` | code | It does. |
| Package-variable seams in the spike | code | Kept: the same pattern as the swarm's seams, documented, and 034 deletes the package. |
| The pause test's 500 ms grace is empirical | tech-lead | Kept: it passed under load, 80 of 80 runs with 14 busy processes (below). |
| A binary-level isolation check (`go tool nm`) | security | Not added: the step now uses the release tags, and `go version -m` in the QA cross-checks the built binary. |
| Quit by bundle path instead of bundle ID | security | Not changed: the gate checks before and after the build that no instance runs. |

**RED and GREEN of the fixes:**

| Command | Result |
|---|---|
| `go test -count=1 ./internal/devtool` with the expected `go list -tags …` and the anchored `pgrep` | `fail` as expected, then `pass`, 100.0% of statements |
| `go test -count=1 -run Stalled ./internal/enginespike` (new test first) | `fail` as expected: `undefined: setUpload`, `undefined: switchUpload`, `Recoveries undefined` |
| `go test -race -count=1 -cover ./internal/enginespike` | `pass`, 100.0% of statements |
| `go test -race -count=3 -run Overwrites ./internal/torrentengine/` | `pass`. A first version on `v1-tree` stalled on piece 0 (006 limitation 2, a zero-length file). Its folder showed the user's `readme.txt` renamed to `readme.txt.part`, grown to 10000 bytes and overwritten from offset 0. The test now uses `v1-single`. |
| `npm --prefix frontend test -- src/spike` | `pass`, 21 tests |

### Finding 14, found while the review fixes were tested

14. **The seeder's upload switch can stall too.**
    - **How it showed up:** `TestPauseHoldsTheBytesAndResumeCompletesTheTransfer` failed once while
      another process saturated the CPU. After Resume the bytes stood at 196608 of 1048576 for 20 s.
    - **Measured** (temporary scratch tests, not committed; 14 `yes` processes as load):

      | Run | Stalled |
      |---|---|
      | the spike's two pause tests, without load | 0 of 52 |
      | the same with load | 1 of 50 |
      | the spike's sequence through a scratch test with load | 1 of 60 |
      | the bare engine (seeder, leecher, upload off 1.5 s, on), with load | 0 of 65 |
      | the spike's two pause tests with load, after the wake-up was added | 0 of 80 failed |

    - **The one stall caught with the engine's status** (`Client.WriteStatus` of both peers): the
      seeder was not choking the leecher, the leecher was interested and had one request
      outstanding, and the seeder held that one request in its queue without serving it. A second
      off and on 50 ms apart served it, and the transfer completed.
    - **The cause inside the engine is not established.** `AllowDataUpload` only asks each
      connection to update its requests; whether the writer is woken depends on state the public
      API does not show.
    - **What the spike does:** when a running transfer stands still for 2 s, the ticker switches the
      upload off, and on again at the next tick, and counts it in `recoveries`.
      `TestAStalledTransferIsWokenAndCounted` drops one "allow" to make the stall certain.

### Final gate

`go tool devtool check` from the repository root, on the stage E working tree, exit code 0:

| Step | Result |
|---|---|
| npm ci, npm cooldown, go env, typecheck, lint, frontend build, gofmt, go vet, go tests | `pass` |
| frontend tests | `pass`: 22 own files at 100% lines, statements, functions and branches |
| bindings | `pass`: OctopusTorrent `frontend/wailsjs/` (6 files), EngineSpike `frontend/spike/wailsjs/` (6 files) |
| `.app` coverage run | `pass`: OctopusTorrent (pid 36362) and EngineSpike (pid 54040) launched and quit |
| go coverage | `pass`: every block of every own Go file covered (unit tests + both `.app` runs) |
| escape hatches, scripts | `pass` |
| product isolation | `pass`: 271 packages with the release tags, none of the spike, test support or the engine |
| release build, docs, plan | `pass` |
| release age | `pass`; the accepted younger version is still `golang.org/x/net@v0.60.0` (until 2026-10-22) |
| npm audit, govulncheck | `pass` |
| updates | `pass`, informational: `github.com/anacrolix/chansync` v0.7.0 → v0.8.0 is out of quarantine. Not taken: 007 kept the version the engine requires, and an engine update decides it. |
| engine integration | `not_run`: task 018 |
| test-bridge UI tests | `not_run`: task 035 |
| single code version | `pass` |

**Result:** `GATE PASSED`.

### Refactoring and debt check

- **Refactoring:** the 006 `TestMain` guard became `swarm.Main`, shared by three test packages. The
  single-app gate code became the `wailsApps` table. The spike's Pause and Resume share one
  `command` and one upload switch. Its clean-up removes only named folders.
- **Debt:** none left by this task. The spike is temporary by design, with an owner and a date: 034
  deletes it, and its note lists every path. The two boundary exceptions end with it. The findings
  are tracked by tests (9–13) or by the spike's counter and task 010's note (14). The scratch tests
  of findings 12 and 14 were deleted; their numbers are recorded above.
