# Transfer switches: a confirmed upload stop, a resume without a stall, the writer wake bound

**Status:** `implemented` (prototype package and engine tests; the product does not link it before [018](../../product/tasks/018-torrent-engine-adapter.md))  
**Requirements:** `TR-08`, `SET-08`, `TR-01`  
**Related tasks:** [010](../../product/tasks/010-upload-stop-and-pause-spike.md); next [013](../../product/tasks/013-transfer-policy-state-machine.md), [018](../../product/tasks/018-torrent-engine-adapter.md), [025](../../product/tasks/025-pause-resume-application.md), [053](../../product/tasks/053-global-upload-policy-production.md), [054](../../product/tasks/054-global-upload-ui-mvp-gate.md)  
**See also:** [ADR global-upload-stop](../decisions/global-upload-stop.md), [engine storage](engine-storage.md), [test torrents and swarm](../tooling/test-torrents.md)  
**Last verified version:** branch `task/010-upload-stop`  
**Last updated:** 2026-10-18

## Goal and behavior

The pinned engine's per-torrent switches return before anything is proven, and two of its defects
make transfers stand still. `internal/torrentengine/transferswitch` gives later code three things:

| Need | What the package offers | What it relies on |
|---|---|---|
| Stop upload and know it stopped | `StopUpload(ctx, t, limits) (Confirmation, error)` | After `DisallowDataUpload` returns no new chunk is buffered; the sent-payload counter counts buffered chunks |
| Resume download after a pause | `ResumeDownload(ctx, t, nudge, limits) (nudges, error)` and `Nudge(t, uploadAllowed)` | One request update for a reason other than a Reject ends the finding-12 stall, which can form until the cancelled requests are all answered |
| A writer that never sleeps for long | `WakeInterval` (1 s) for `ClientConfig.KeepAliveTimeout` | The keep-alive timer is the only generic waker of a lost wake-up |

`Confirmation` holds `Accepted`, `Applied` (the switch returned), `Confirmed` (one quiet window of
the sent counter later) and `Handed` (payload handed to connections after `Applied`; 0 on the pin).
`ErrNotConfirmed` means the counter kept moving until the deadline; upload stays disallowed.
`DefaultLimits()`: poll 25 ms, quiet 250 ms, deadline 2 s, first nudge after 1 s, doubling up to 16 s;
limits that cannot work are `ErrInvalidLimits`.

`ResumeDownload` keeps watching until nothing is missing or its context ends: the caller runs it for
as long as the torrent is not paused and runs the nudge under its per-torrent lock, reading the
upload policy at that moment, so a stop or a pause applied meanwhile is never undone.

What still reaches a peer after `Applied` is what was already written: up to the receiver's request
window (about 1 MiB on loopback with an unlimited sender), delivered at the receiver's pace. The
product never claims more than "no new payload after the switch".

## Architecture analysis before the change

- Layer: engine adapter side (`internal/torrentengine/...`, the only place with anacrolix types).
  The product binary does not link it yet; the isolation step still keeps anacrolix out (018 lifts it).
- No policy here: when to stop or resume, and which torrents are manually paused, is 013 (pure
  policy) and 053/025 (application). The package only makes one switch's effect provable.
- Reused: the swarm and fixtures (006), the classic-IO filestore harness (009). Replaced: the test
  helper `keepMoving` and the spike's wake-up, both workarounds for finding 14/22.

## Contracts and data

- `Uploader`: `DisallowDataUpload()`, `Stats()`. `Switch`: `DisallowDataUpload()`, `AllowDataUpload()`.
  `Downloader`: `AllowDataDownload()`, `BytesMissing()`, `Stats()`. `*torrent.Torrent` satisfies all three
  (compile-time assertions in `switch.go`).
- No DTO crosses the bridge; 053 maps `ErrNotConfirmed` to an `AppError`.
- Swarm changes: `Options.KeepAliveTimeout`; per-direction log functions `Log.First(name, Field)`,
  `Log.Last(name, Field)`, `Log.Delta(label, name) Counters` with `Field` = `Payload`, `PayloadRead`,
  `PayloadWritten`. Fixture `v1-long`: 16 MiB, 256 pieces of 64 KiB.

## Scenarios and TDD

| Scenario | Test | Level |
|---|---|---|
| Confirmed after a quiet window; growth restarts it; steady growth is `ErrNotConfirmed`; cancel | `transferswitch/switch_test.go` | unit, test double |
| Resume allows and returns when complete; keeps watching after payload; the wait doubles to its cap; a stop during a resume is not undone; cancel before and after the allow; invalid limits | `transferswitch/switch_test.go` | unit, test double |
| Chain S→A→L: A's upload stops, L drains then holds, A keeps downloading | `TestStoppingUploadKeepsTheDownloadGoing` | real engine |
| Five stops and starts with full buffers | `TestRepeatedStopsWithDataInTheBufferEndWithinTheMeasuredResidual` | real engine |
| New connection to a stopped, downloading torrent | `TestANewConnectionGetsNoPayloadWhileUploadIsStopped` | real engine |
| Complete stopped torrent refuses connections | `TestACompleteTorrentAcceptsNoConnectionWhileUploadIsStopped` | real engine |
| New import and restored entry added under the stop | `TestATorrentAddedUnderTheStopUploadsNothing` | real engine |
| Completes under the stop, seeds only after it | `TestACompletedTorrentDoesNotSeedWhileUploadIsStopped` | real engine |
| The upload switch never changes the download switch | `TestTheUploadSwitchNeverChangesTheDownloadSwitch` | real engine |
| Concurrent stop, pause, add, resume on one client | `TestConcurrentCommandsKeepEachTorrentsSwitches` | real engine |
| Cause of finding 12 and its remedy | `TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected`, `TestResumeDownloadEndsTheStallOfAnImmediateResume` | real engine |
| A resume while a residual is still in flight | `TestResumeDownloadAfterAPauseWithAResidualInFlightKeepsMoving` | real engine |

RED/GREEN commands and the measurements behind every bound are in the
[report](../verification/010-upload-stop-and-pause-spike.md).

## Implementation and reuse

- `internal/torrentengine/transferswitch/switch.go`: the three entries above, no goroutine of its own;
  both functions return when their condition holds or their context ends.
- Test bounds in `internal/torrentengine/upload_test.go` come from stage A3 (× 1.5): residual 1536 KiB
  per connection, drain residual ÷ receiver rate + 250 ms, 500 ms for a 256 KiB/s sender, first byte
  after a start within `WakeInterval` + 250 ms.

## Checks and 100% coverage

`go tool devtool check`: `transferswitch` is 100% of statements from its unit tests; the swarm and
fixture changes are covered by their own tests. Engine scenarios run with `-race` in
`go test ./internal/torrentengine/...` (about 85 s for the package now, most of it the `v1-long`
scenarios).

## Refactoring and debt check

- `keepMoving` deleted from `internal/torrentengine/filestore/run_test.go`; the harness peers use the
  interval and the 009 scenarios stay green.
- The spike's `wake`/`Recoveries` deleted; both clients use the interval; DTO, shared example and
  bindings follow.
- The swarm's sum-only log functions were replaced by per-direction ones (no parallel API kept).
- Open primitives for later tasks, not debt: persisting the intent (053), the product client
  configuration (018).

## Usage and recovery

- A torrent added while the stop is on: switch before `SetInfoBytes`.
- `ErrNotConfirmed`: report the torrent as not applied; the switch stays disallowed; a later retry
  calls `StopUpload` again.
- `ResumeDownload` for as long as the torrent is not paused, under its context; cancel it on pause or
  drop and apply the pause after it returned; run the nudge under the per-torrent lock.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-18 | Created in task 010 | [report 010](../verification/010-upload-stop-and-pause-spike.md), [ADR](../decisions/global-upload-stop.md) |
