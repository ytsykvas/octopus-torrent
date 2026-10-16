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
ADR [engine-lifecycle](../decisions/engine-lifecycle.md).

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
