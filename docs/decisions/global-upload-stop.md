# Stopping upload without stopping download, and resuming without a stall

**Status:** `accepted` (engine part; the production policy is [053](../../product/tasks/053-global-upload-policy-production.md))  
**Date:** 2026-10-18  
**Task:** [010](../../product/tasks/010-upload-stop-and-pause-spike.md)  
**Register item:** O6 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `TR-08`, `SET-08`, `TR-01`

## Question

"Stop upload for all torrents" (TR-08, SET-08) must stop sending file data while downloads go on, for
current, new, restored and completed torrents, and must not cancel a manual pause (TR-01). The
production policy (053) and its UI (054) need to know:

- What exactly is the boundary after which no payload leaves, how is it confirmed, and what may
  still arrive at the other side after it?
- How long may confirmation take before it counts as an error?
- How does a torrent that is new or restored get the stop before it can send anything, given that
  the initial `AddTorrentOpts` flags are dead ([engine-lifecycle](engine-lifecycle.md), finding 10)?
- What is the remedy for the three stalls 007 and 009 handed over: download allowed right after a
  pause (finding 12), the upload switch under load (14), a fresh transfer (22)?

The engine is anacrolix/torrent v1.61.0. Facts marked "test" are asserted by a test in
`internal/torrentengine` (an engine update that changes them turns it red); "measured" ones are in
the [report](../verification/010-upload-stop-and-pause-spike.md); "source" ones were read only.

## Decision

### Every client sets `KeepAliveTimeout = transferswitch.WakeInterval` (1 s)

A connection's writer can sleep through a lost wake-up: `tickleWriter` is a
`chansync.BroadcastCond.Broadcast`, which does nothing while no waiter holds a channel, and the
writer takes its channel only after `fillWriteBuffer` released the client lock (source). A request
made ready in that gap waits until the writer runs again, and the keep-alive timer is the only
generic waker; the default is one minute. This is findings 14 and 22. With 1 s, 0 of 300 fresh
transfers and 0 of 60 runs of the spike's sequence under load stood still past 1.5 s, against 11 of
300 with the default (measured). The cost is one 4-byte keep-alive per second on an idle connection
that is useful to either side (measured: 4 B/s). The timer runs only on such a connection; one that
is useful to neither side has nothing to send. The engine adapter (018) sets it on its client and
asserts that in a test; the test swarm takes it through `swarm.Options.KeepAliveTimeout`.

### Stop upload: `transferswitch.StopUpload`

1. Record `Accepted`, call `Torrent.DisallowDataUpload()`, record `Applied`.
2. After it returns, the engine hands no new chunk to any connection: chunks are buffered only inside
   `upload()`, which re-checks the switch per chunk under the client lock that `DisallowDataUpload`
   takes (source; measured: the sent counter never moved after `Applied`, 180 of 180).
3. Confirm from the torrent's own sent-payload counter (`Stats().BytesWrittenData`, counted when a
   chunk is buffered): **confirmed** when it stayed unchanged for `Quiet` (250 ms), polled every
   `Poll` (25 ms); a change restarts the window; no full window before `Deadline` (2 s) is
   `ErrNotConfirmed`, which 053 shows as an apply error. The method returning is never the
   confirmation.
4. **What the product may claim:** after `Applied` no new payload leaves this client. Payload means
   piece data: protocol messages (metadata for a magnet peer, haves, keep-alives) still flow. Chunks already
   written still reach the receiver at its own pace and cannot be recalled: on loopback up to about
   1 MiB per connection with an unlimited sender (the receiver's request window), one to a few chunks
   with a rate-limited one, arriving within residual ÷ receiver rate (measured: ≤ 1008 KiB, ≤ 3.9 s at
   256 KiB/s, ≤ 0.97 s at 1 MiB/s). On a real network the bound is the receiver's request window and
   the socket buffers (send buffers autotune up to 4 MiB here); it is not measured here (054).
5. The choke follows when the writer next runs; with the Fast extension the peer's pending requests
   survive it and are served at once when upload is allowed again (measured: first byte ≤ 15 ms).

### New, restored and magnet torrents get the switch before their info

`AddTorrentOpts{InfoHash, Storage}` (or `AddInfohash`) → `DisallowDataUpload()` → `SetInfoBytes`.
Without info a torrent has no piece to send. Tested for a new import (downloads, accepts the
leecher, sends nothing) and for a restored complete entry. TR-08 and the engine research were
corrected accordingly (owner decision, 2026-10-18).

### Completed torrents

`seeding()` is false while upload is disallowed (source), and a complete torrent that does not seed
does not want peers: `newConnsAllowed` refuses every new connection (test). After `AllowDataUpload`
it accepts them again and seeds (test).

### Manual pause and the global stop

The upload switch never changes the download switch: a torrent with both directions disallowed stays
at zero downloaded bytes through the global stop and even through an `AllowDataUpload` call (test:
`TestTheUploadSwitchNeverChangesTheDownloadSwitch`). That call does allow its upload: never making it
for a paused torrent is the policy's rule, not the engine's.
The composition — global off allows upload only for torrents that are not manually paused — is the
policy of 013 and the application of 053; switches are called only when their state changes, and the
commands of one torrent are serialized.

### Resume download: `transferswitch.ResumeDownload` (remedy for finding 12)

Finding 12 is not a lost wake-up: a 3 s keep-alive did not end it (measured, 4 of 4). The pause
cancels every request; with Fast each awaits a Reject. When download is allowed again before those
arrive, the wanted chunks cannot be requested; the last Reject empties the set and triggers a request
update with the reason `peerUpdateRequestsRemoteRejectReason`, for which `applyRequestState` adds no
request ("the peer will send an unchoked message"), and nothing asks again (source; test). Any
update for another reason resumes it.

The trap is not limited to the first moment after the resume. A pause that lands while chunks are
still on their way leaves the Rejects behind those chunks, and while the cancelled requests await
them they count against the request limit, so the engine requests nothing new; the trap can close
after the first bytes of the resume (test: a 1 MiB residual at 256 KiB/s stood still 3 of 3 runs
with a "return at the first byte" remedy, 2 of 3 with "wait for a quiet counter, then allow").

`ResumeDownload(ctx, t, nudge, limits)` (owner decision, amendment 2): return the context's error
without touching the engine if it is already done; call `AllowDataDownload`; then, until nothing is
missing or the context ends, call `nudge` whenever no payload arrived for the current wait, which
starts at `Nudge` (1 s), doubles with each nudge up to `MaxNudge` (16 s) and resets when payload
arrives. The nudge is `Nudge(t, uploadAllowed)`: `DisallowDataUpload` — which always requests an
update — and `AllowDataUpload` only when the policy allows upload now. **The caller runs the nudge
under its per-torrent command lock and reads the policy at that moment**, so a global stop or a pause
applied during a resume is never undone by a later nudge (unit test). 025 runs `ResumeDownload` for
as long as the torrent is not paused, under the torrent's context, cancelled on pause or drop.

Cost: a torrent whose peers cannot serve it is nudged at most every 16 s; with upload allowed a
nudge may send a choke and an unchoke. Measured: 20 of 20 immediate resumes completed (first byte
≤ 1.05 s); the residual scenario kept moving in 8 of 8 runs, each after one nudge.

This is what the engine's own request-update timer would do, but it is switched off on the pin
(`enableUpdateRequestsTimer = false`, `client.go:1230`); libtorrent re-issues timed-out requests
and marks silent peers as snubbed for the same reason.

### Engine findings (numbered after the 24 of 006–009)

25. **No request after the last cancelled request is rejected.** The cause of finding 12, above.
    Test: `TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected` (a 1 s keep-alive does not end
    it; one `DisallowDataUpload` on the leecher does).
26. **A connection's writer can miss a wake-up; only its keep-alive timer runs it again.** The cause of
    findings 14 and 22. Measured, not asserted by a test (it is a race): see the first decision.
27. **After `DisallowDataUpload` returns, no new chunk is buffered; `BytesWrittenData` counts a chunk
    when it is buffered, not when it is sent.** What was written before still drains, up to the
    receiver's request window. Tests: `Confirmation.Handed` is 0 in the chain, the repeated-stop and the
    concurrent scenarios; measured in 180 of 180 runs.
28. **A complete torrent whose upload is disallowed accepts no new connection.** Test:
    `TestACompleteTorrentAcceptsNoConnectionWhileUploadIsStopped`.
29. **With the Fast extension, a choke keeps the peer's requests; they are served within
    milliseconds of `AllowDataUpload`.** Measured; the repeated-stop test bounds it.
30. **`DisallowDataUpload` always requests a request update on every connection; `AllowDataUpload`
    does only when it changes the state.** What makes the nudge of `ResumeDownload` work while upload
    stays stopped. Source; test: `TestResumeDownloadEndsTheStallOfAnImmediateResume` (upload stopped).

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Confirm from the wire counter (`BytesWritten`) | Counts bytes that left the process | A slow receiver blocks the socket write, so the counter stands still while data is pending; it would confirm too early | The buffered-chunk counter has an exact source guarantee; the drain is proved by the remote counter in tests |
| `ClientConfig.NoUpload` for the global stop | One flag | Client-wide and set before start; no verified contract for changing it at run time | Per-torrent switches are the agreed mechanism (research §3) |
| A watchdog that toggles upload when nothing moves (the spike's and `keepMoving`'s remedy) | Works for 14/22 | Guesses "should be moving"; churns idle torrents; does not reach finding 12 on the downloading side | The keep-alive bound fixes the cause for 14/22 with no policy code |
| A gap between pause and resume, or a quiet counter before the allow | No code, or a short wait | 1 s still stalled 2 of 20 (007); a quiet window can fall inside a rate-limited drain (2 of 3) | The watching nudge does not depend on timing |
| Stop at the first payload byte after the resume (the first form of amendment 1) | No watching after the resume | The first bytes can be the residual; the trap closes after them (3 of 3) | Replaced by amendment 2 |
| Pause by dropping the torrent or its connections (what Transmission's stop and libtorrent's pause do) | No cancelled requests, so no trap | `Drop` is allowed only to the importing service; how the pinned engine reconnects after `SetMaxEstablishedConns(0)` is not verified | Left to 025 to evaluate; the nudge makes the switch-based pause safe meanwhile |
| Initial `AddTorrentOpts` flags | One call | Never read by the pinned engine (finding 10) | Dead |

## Verified grounds

- Report: [010](../verification/010-upload-stop-and-pause-spike.md), stage A (scratch measurements, numbers
  kept) and stage C (committed tests). Environment: Apple M4, macOS 27.0.1, Go 1.27.2,
  anacrolix/torrent v1.61.0, loopback swarm, with and without `-race`.
- Committed tests, all `pass`: `internal/torrentengine/transferswitch` (unit, with a test double) and
  `internal/torrentengine/upload_test.go` (real engine): the chain S→A→L (upload stopped, download
  continues), five stops and starts with full buffers, a new connection, a complete torrent, an add
  under the stop (new import and restored entry), a torrent that completes under the stop, the
  download switch untouched by the upload switch, concurrent commands on four torrents, the cause of
  finding 12 and its remedy, a resume with a residual in flight.
- Not run: the policy-saved timing (no preferences store before 014/015; 053), residual on a real
  network and the product `.app` (054).

## Consequences

- **018** sets `KeepAliveTimeout = transferswitch.WakeInterval` on the client, adds torrents with the
  switch before `SetInfoBytes`, and moves `transferswitch` into the adapter.
- **013** computes download and upload permissions independently; global off never allows upload
  for a manually paused torrent.
- **025** resumes download only through `ResumeDownload`, for as long as the torrent is not paused,
  with the nudge under its per-torrent lock; it may evaluate a disconnecting pause instead.
- **053** persists the intent first, applies `StopUpload` to every torrent, reports
  pending/confirmed/`ErrNotConfirmed` per torrent from the confirmation, and applies the stop to new
  and restored torrents before their info. **054** explains that bytes already sent still arrive.
- `keepMoving` (filestore tests) and the spike's own wake-up are gone; their peers use the interval.
- Recheck on an engine update: findings 25, 27, 28 and 30 through their tests
  (`TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected`, `Confirmation.Handed` in the upload
  scenarios, `TestACompleteTorrentAcceptsNoConnectionWhileUploadIsStopped`,
  `TestResumeDownloadEndsTheStallOfAnImmediateResume`), and findings 26 and 29 by repeating the
  measurements of the report (stage A1, A3).
