# Report 010: stopping upload without stopping download

**Task:** [010](../../product/tasks/010-upload-stop-and-pause-spike.md)  
**Date:** 18 October 2026  
**Change version:** branch `task/010-upload-stop` (commits are listed per stage)

## Session architecture plan

The owner approved this plan on 18 October 2026, after an adversarial review (5 → 7 of 10). The two owner decisions (EngineSpike.app takes the measured remedy; TR-08 and the engine research are corrected) are listed in the plan. The approved plan follows verbatim.

### Context

Task [010](../../product/tasks/010-upload-stop-and-pause-spike.md) (P1 spike; `TR-01`, `TR-08`, `SET-08`) owns register item **O6** (global upload stop: confirmation boundary and timeout) → ADR `docs/decisions/global-upload-stop.md` (the register's name; the task file's `upload-policy.md` is corrected to it). Dependency 007 is `done`. Per 007 and 009 it also owns the **product remedy** for three stalls: finding 12 (download allowed right after disallowing it), 14 (upload switch stalls under load) and 22 (a fresh transfer stands still), with a test that measures it.

The task must prove: after a recorded boundary, our sent-payload counter and the other side's received counter stop growing while our download continues; the upload switch never cancels a manual pause; new connections, new/restored torrents and completed torrents get no payload while the stop is on; repeated toggles with data in the buffer; concurrent commands. Nothing changes in the product binary (018 links the engine; 053 builds the production policy).

**Owner decisions (2026-10-18):**
1. EngineSpike.app drops its home-made stall wake-up for the measured remedy (DTO field `recoveries` removed, spike bindings regenerated); `/qa` drives its Pause/Resume (= seeder upload switch) in the real window.
2. TR-08 and the engine research are corrected: the "initial TorrentSpec flags" are dead (finding 10); a new or restored torrent gets the switch **after `AddTorrentOpts`/`AddInfohash` and before `SetInfoBytes`** (behavior unchanged, mechanism corrected).

### Facts read in the pinned source (v1.61.0), each asserted by a test or a stage-A measurement

- `DisallowDataUpload` sets a plain flag and calls `onNeedUpdateRequests` (a deferred `tickleWriter`) per conn (`torrent.go:3100-3121`). Chunks are buffered only inside `upload()`, which re-checks `uploadAllowed()` per chunk under the client lock (`peerconn.go:1183-1212`), and `DisallowDataUpload` takes that lock. **So no Piece message is buffered after `DisallowDataUpload` returns** — the local boundary.
- `BytesWrittenData` is counted when a chunk is buffered (`PeerConn.write` → `conn-stats.go:44-50`), wire `BytesWritten` at the socket write, the peer's `BytesReadData` when a Piece is decoded. Already-buffered bytes still drain: up to two writer buffers (high water 32 KiB + one chunk each, `peer.go:256`, `peer-conn-msg-writer.go:104-124`) plus kernel socket buffers (macOS here: `sendspace`/`recvspace` 128 KiB, autotuning up to 4 MiB). The remote counter is the only proof of the drain.
- The choke happens when the writer next runs; with Fast (default on) a choke keeps the peer's pending requests (≤ 1 MiB read-ahead per conn, `MaxAllocPeerRequestDataPerConn`) and sends no Reject; they are served on the next unchoke. A request arriving while choking is rejected. A new connection starts choked.
- `seeding()` is false while upload is disallowed (`torrent.go:2075`): a completed torrent does not seed. The swarm sets `cfg.Seed = true`, so a partial torrent uploads too (needed for the chain S→A→L).
- **Lost writer wake-up (hypothesis H1 for findings 12, 14, 22).** `tickleWriter` is `chansync.BroadcastCond.Broadcast`, a no-op when no channel is outstanding (`chansync@v0.7.0/broadcast-cond.go:17-35`). The writer takes `Signaled()` only after `fillWriteBuffer` has released the client lock (`peer-conn-msg-writer.go:87-99`), so a request made ready (`servePeerRequest`, another goroutine) or a request update in that gap is lost. `onNeedUpdateRequests` returns early while `needRequestUpdate` is set (`peerconn.go:1801-1808`), so later events do not re-tickle either. The only generic waker is the writer's keep-alive timer, `ClientConfig.KeepAliveTimeout` (default 1 min; read nowhere else, `config.go:241`; the engine's own `TestingConfig` uses 1 ms). This matches finding 14's status dump (one request unserved in the seeder's queue).

### Design decisions (go into the ADR `global-upload-stop.md`)

- **Remedy = writer wake bound.** Every product client sets `KeepAliveTimeout = transferswitch.WakeInterval`; a lost wake-up then ends within one interval (the writer re-runs `fillWriteBuffer`, which serves ready requests and updates requests). Initial value **1 s**, chosen by stage A rule A2. Cost: one 4-byte keep-alive per interval on an idle *useful* connection only. This replaces the spike's wake-up and the test helper `keepMoving`. **If stage A refutes H1 for any finding, work stops at that point and the plan is amended with the owner** before a different remedy is written (no speculative watchdog code).
- **Confirmed stop.** `transferswitch.StopUpload(ctx, t, limits)`: record `Accepted`, call `DisallowDataUpload`, record `Applied`, then poll `Stats().BytesWrittenData` every `Poll`; **confirmed** when it stayed equal to the post-`Applied` value for `Quiet`; a change restarts the quiet window; no full window before `Deadline` → `ErrNotConfirmed` (wraps the observed growth); a done context → its error. Initial limits `Poll 25 ms, Quiet 250 ms, Deadline 2 s` (adjusted only by a stage-A measurement, reason recorded). The method returning is never the confirmation. The ADR states the product claim exactly: after `Applied` no new payload is handed to any connection; bytes already handed over (≤ the measured residual) are still delivered at the receiver's pace and cannot be recalled.
- **New, restored, magnet torrents:** switch set between `AddTorrentOpts`/`AddInfohash` and `SetInfoBytes` (without info the engine has no piece to send). `AllowDataUpload`/`DisallowDataUpload` are called only when the desired state changes; commands of one torrent are serialized by the caller (053).
- **Manual pause vs global stop:** the engine proof is that the upload switch never changes the download switch; the composition rule (global off → `AllowDataUpload` only for torrents not manually paused) is 013's policy and 053's application — 010 hands the engine facts over, it writes no policy code.
- **Timeline recorded per measured stop:** command accepted, `Applied`, local confirmed, remote last payload byte. "Desired policy saved" is `not_run`: there is no preferences store before 014/015 (owner 053).

### Boundaries and files

Not changed: product `main.go`, React product UI, product bridge/facade/bindings, `go.mod` (no new dependency), `.npmrc`, isolation step (the product still links none of this).

| File | Change |
|---|---|
| `internal/torrentengine/transferswitch/{doc.go,switch.go,switch_test.go}` | new prototype that 018/053 build on: `WakeInterval`, `Limits`, `DefaultLimits`, `Uploader` (`DisallowDataUpload()`, `Stats() torrent.TorrentStats`; `*torrent.Torrent` satisfies it), `Confirmation{Accepted, Applied, Confirmed time.Time}`, `ErrNotConfirmed`, `StopUpload` |
| `internal/torrentengine/upload_test.go` | new: real-engine scenarios (below), package `torrentengine_test` |
| `internal/torrentengine/lifecycle_test.go` | edit: finding-12 doc comment (cause H1), new pinned test of the keep-alive end; helpers shared with `upload_test.go` stay in one place |
| `internal/torrentengine/testutil/swarm/peer.go` | edit: `Options.KeepAliveTimeout time.Duration` (0 = engine default) |
| `internal/torrentengine/testutil/swarm/counters.go` | edit (refactor): `FirstPayload`/`LastPayload`/`PayloadAfter` replaced by `First(name, Field)`, `Last(name, Field)`, `Delta(label, name) (Counters, error)` with fields `Payload`, `PayloadRead`, `PayloadWritten`; swarm tests migrated |
| `internal/torrentengine/testutil/fixture/specs.go` + `testdata/` | edit: fixture `v1-long` — one 16 MiB file, 64 KiB pieces (256), set "long": room for a residual of several MiB; `.torrent` and `fixtures.json` regenerated with `-update` |
| `internal/torrentengine/filestore/run_test.go` | edit (refactor): `keepMoving` deleted; harness peers use `KeepAliveTimeout: transferswitch.WakeInterval` |
| `internal/enginespike/service.go`, `transfer.go`, `service_test.go`, `testdata/transfer.json` | edit: `wake`, `still`, `waking`, `stallTicks`, `Recoveries` deleted; both peers use `WakeInterval`; `TestAStalledTransferIsWokenAndCounted` deleted, replaced by a repeated Pause/Resume test that completes with no wake-up code |
| `frontend/spike/wailsjs/...` | regenerated (`recoveries` gone) |
| `frontend/src/spike/{bridge.ts,SpikeApp.tsx}` + tests | edit: decoder and "N stall(s) woken" removed |
| docs | see Documents |

### Stages, tests first

Two checkpoints, each passing `go tool devtool check`: **1** = A, B, C; **2** = D, E.

**A — measurements (scratch tests, not committed; numbers go in the report).** Rules declared before measuring; each with and without `-race`.
1. *H1.* Reproduce finding 12 (immediate allow, 20 runs) and 22 (300 fresh `v1-large` transfers, `-race`, classic IO) with `KeepAliveTimeout` 3 s vs the default; record each stall's length and `WriteStatus` of both clients. H1 holds for a finding when, with 3 s, every stall ends within 3 s + 0.5 s of the last write and the default leaves it standing past 8 s. Finding 14: 60 runs of the spike's sequence under 14 `yes` processes with the chosen interval.
2. *Interval.* With 1 s: zero runs standing still past 1.5 s in rule A1's sets; keep-alive overhead ≤ 1 KiB/s per idle connection; else 2 s, else the owner.
3. *Residual and drain* on `v1-long`, 30 runs per row: (a) both peers 256 KiB/s; (b) sender unlimited, receiver 1 MiB/s (buffers full); (c) sender unlimited, receiver 256 KiB/s. Boundary after 25% received. Record: local `BytesWrittenData` growth after `Applied` (rule: always 0), remote bytes after `Applied` (max, p99), time to the last remote byte, `StopUpload` confirmation time. The asserted bounds of stage C = measured max × 1.5, rounded up to 64 KiB / 250 ms.
4. *Resume latency:* first remote byte after `AllowDataUpload` (preserved requests, 1 MiB limiter burst) — recorded.

**B — swarm and fixture (testutil).**
- RED `go test ./internal/torrentengine/testutil/...` → `undefined: swarm.Field` / `Options.KeepAliveTimeout`, and `fixture.Named("v1-long")` not found.
- GREEN: field helpers over synthetic logs (all branches), a peer started with `KeepAliveTimeout` reports it in its config, `v1-long` in the catalog test and the committed-bytes comparison.

**C — `transferswitch` and the engine scenarios.**
- RED `go test ./internal/torrentengine/transferswitch` → `undefined: StopUpload`. Unit tests with a **test double** `Uploader` (named as such): confirmed after a quiet window; growth restarts the window; steady growth → `ErrNotConfirmed` at the deadline; canceled context; times ordered `Accepted ≤ Applied ≤ Confirmed`.
- RED `go test ./internal/torrentengine -run 'Upload|Stall'` → `undefined: transferswitch...`. Real engine, peers on `WakeInterval`; `v1-long` where a residual is measured (1, 2, 3; never run to completion), `v1-large` where a transfer must complete (4, 5, 8, 9) to keep the gate short. Rate limits per scenario: the sender of the measured link unlimited or 512 KiB/s, the receiver 256 KiB/s–1 MiB/s, as fixed by stage A:
  1. `TestStoppingUploadKeepsTheDownloadGoing` — chain S→A→L (L connected only to A): after `StopUpload` on A, A's `PayloadWritten` delta is 0, L's `PayloadRead` delta ≤ residual bound and stays flat after the drain bound, A's `PayloadRead` keeps growing from S.
  2. `TestRepeatedStopsWithDataInTheBufferEndWithinTheMeasuredResidual` — sender unlimited, receiver limited, 5 stop/start cycles in one transfer; each stop confirmed and bounded remotely, each start resumes within `WakeInterval` + 1 s.
  3. `TestANewConnectionGetsNoPayloadWhileUploadIsStopped`.
  4. `TestATorrentAddedUnderTheStopUploadsNothing` — `AddInfohash` → `DisallowDataUpload` → `SetInfoBytes` over complete data (the restore path, verified) and over empty data (a new import): the leecher gets 0 payload bytes from the first connection; after `AllowDataUpload` it completes.
  5. `TestACompletedTorrentDoesNotSeedWhileUploadIsStopped` — A completes under the stop, L gets 0; after `AllowDataUpload` L completes.
  6. `TestTheUploadSwitchLeavesAManualPauseInPlace` — torrent P with both directions disallowed: upload stop on, then `AllowDataUpload` called on P directly: P's read and written payload stay 0 (download switch untouched).
  7. `TestConcurrentCommandsKeepEachTorrentsSwitches` — one client, four torrents (`v1-single`, `v1-tree-no-empty`, `v1-large`, `v1-long`): from goroutines, add one under the stop, pause one, resume one, stop upload on the rest; the expected switch state of each comes from the test table; payload per direction matches it.
  8. `TestPinnedEngineEndsALostWakeUpOnlyAtTheKeepAliveTick` (H1 pinned): finding-12 sequence with `KeepAliveTimeout` 3 s: no progress for ≥ 1.5 s, then progress before 5 s. Failure message: re-check 010/018/025. `TestPinnedEngineStallsWhenDownloadIsAllowedRightAfterDisallowing` stays on the default (the defect).
  9. `TestTheWakeIntervalEndsTheStallOfAnImmediateResume` — same sequence with `WakeInterval`: completes within the transfer limit.

**D — refactor with the remedy (tests green before and after).** `filestore` harness on `WakeInterval` (`keepMoving` deleted; the 009 scenarios run twice); EngineSpike service, DTO, shared example, bindings (`go tool wails generate module` in `cmd/engine-spike`), TS decoder/UI/tests.

**E — documents, `/branch-review`, gate, `/qa`, `done`.** `/qa` scenarios on EngineSpike.app: start; 5 Pause/Resume cycles — while paused the shown payload does not grow for 3 s, after Resume it grows; the transfer completes; quit by Apple Event; no non-loopback socket.

### Coverage

`go tool devtool check`: `transferswitch` 100% from its unit tests (+ engine tests); swarm and fixture changes covered by their own tests; the EngineSpike `.app` profile is produced by the gate's run as today; TS spike files keep four 100% metrics. No product code changes, so the OctopusTorrent profile is unchanged.

### Real vs doubles

Real: pinned engine, loopback swarm, classic IO where the harness uses it, CPU load for finding 14, EngineSpike.app. Double (named): the `Uploader` in `transferswitch` unit tests. Not provable here: real Internet peers and real kernel autotuning against remote hosts (054, AS-18); the policy store timing (053); the product `.app` (054).

### Documents

- New: ADR `docs/decisions/global-upload-stop.md` (O6: engine part accepted, production 053); report `docs/verification/010-upload-stop-and-pause-spike.md` (this plan as the session plan, RED/GREEN, measurements, findings from 25); feature doc `docs/app/transfer-switches.md`; index rows in `docs/README.md`.
- Edit: `docs/decisions/engine-lifecycle.md` (findings 12/14: cause and remedy, rule text), `engine-storage.md` (22: remedy, `keepMoving` gone), `controlled-swarm.md` (field helpers, `KeepAliveTimeout`), register O6 in `project-baseline.md`; `docs/tooling/test-torrents.md` (`v1-long`), `docs/tooling/engine-spike.md`, `docs/development/testing.md` (runtime); `product/PROJECT_REQUIREMENTS.md` TR-08 wording; `product/ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md` §2–3; architecture §3 (new package) and §5 (confirmed stop); "From 010" notes in tasks 013, 018, 025, 053, 054; task 010 proposed locations; `CLAUDE.md` big picture; `README.md` current state; task 010 status + `go tool devtool plan-nav`.

### Out of scope

Policy state machine (013), preferences schema (014/015), engine adapter and client config in the product (018), production toggle and AppError mapping of `ErrNotConfirmed` (053), UI (054), reducing the kernel-held residual (needs an engine change: new decision).

### Verification

`go test -race -count=1 ./internal/torrentengine/...` twice; `go test -count=1 ./internal/torrentengine/...` once (timing rows without `-race`); `npm --prefix frontend test`; `go tool devtool check` → `GATE PASSED` at both checkpoints; `/branch-review`; `/qa` on EngineSpike.app. `not_run` allowed: policy-saved timing (053), real-network residual and the product `.app` (054).

### Self-assessment: 7/10 (was 5/10 before the adversarial review)

**What the review fixed:**
- The confirmation first watched wire bytes; a receiver-limited socket blocks the write and fakes quiet. Now the rule rests on the source fact that nothing is buffered after `DisallowDataUpload` returns, and the drain is proved only by the remote counter.
- `v1-large` (1 MiB) is smaller than the possible kernel-held residual (autotuning up to 4 MiB); `v1-long` (16 MiB) added so a residual cannot be mistaken for completion.
- The swarm helpers summed both directions, which cannot show "upload stopped, download continues" on one peer; per-field helpers replace them (no duplicate API).
- New torrents: the dead spec flags are replaced by a switch before `SetInfoBytes`, tested for both a new import and the restore path.
- The remedy was a guess (watchdog toggling); it is now a source-backed hypothesis with a pinned test, and a refuted hypothesis stops the work instead of producing speculative code.
- Manual pause: proved at the engine boundary without writing 013's policy early.

**Remaining weak points:**
- H1 is read from source, not yet measured; if finding 12 has a second cause, the remedy part of the task is re-planned (the upload-stop criteria do not depend on it).
- Engine tests assert time windows (drain, resume, keep-alive end); bounds come from measurement × 1.5 with `-race`, but CPU-starved runs can still flake — the report will record any repeat.
- The residual on loopback is not the residual on a real network; the ADR states the claim as "no new payload after `Applied`", which holds regardless.
- The gate gets slower (estimate +40–60 s for the new `v1-long` scenarios); measured and recorded in `testing.md`.
- Large task; checkpoint 1 is a gate-passing stopping point.

### Amendment 1 (2026-10-18, owner approved after stage A1)

- **H1 holds for finding 22:** 300 fresh `v1-large` transfers, `-race`, classic IO: default keep-alive 11 stalled past 8 s (3.7%); `KeepAliveTimeout` 1 s: 0 past 1.5 s, longest gap 1.01 s. `WakeInterval = 1 s` stays the remedy for 14/22 (14 still measured under load in A1).
- **H1 is refuted for finding 12:** with `KeepAliveTimeout` 3 s the immediate resume stood still 4 of 4 runs for 12 s. Cause, read in the source and confirmed by experiment: `applyRequestState` adds no request when the update reason is `peerUpdateRequestsRemoteRejectReason` (`requesting.go`, "don't add requests on receipt of a reject"). The pause cancels every request; while download is allowed again, those requests still await their Reject; the last Reject triggers an update with that reason, which requests nothing, and nothing else asks again (the engine expects a new Unchoke). Status dump in the stall: unchoked, interested, 0 requests, 0 awaiting acknowledgement, 63 wanted pieces. Any update with another reason resumes it: the seeder's upload off/on (an Unchoke) or our own `DisallowDataUpload` (+ `AllowDataUpload` when upload is allowed) — 5 of 5 each, transfer moving again within 70 ms.
- **Remedy for 12 (owner decision):** `transferswitch.ResumeDownload(ctx, t, uploadAllowed, limits) (nudges int, err error)`: take `BytesReadData`, call `AllowDataDownload`, then poll; return at the first payload byte read since the call or when nothing is missing; after every `Nudge` interval (1 s) with no payload, nudge with `DisallowDataUpload` and, when `uploadAllowed`, `AllowDataUpload`; a done context returns its error. The trap can only be set while no payload has arrived since the resume (all wanted requests were awaiting a Reject), so the detection is exact; the caller bounds the call with the torrent's context (025 cancels it on pause/drop). `Downloader` interface: `AllowDataDownload`, `DisallowDataUpload`, `AllowDataUpload`, `Stats`, `BytesMissing`. Unit tests with a test double; engine tests: immediate resume (20 runs in stage A, a committed test), resume with the upload stop on (`uploadAllowed=false`: the nudge never allows upload).
- Test 9 becomes `TestResumeDownloadEndsTheStallOfAnImmediateResume`; test 8 pins the reject-reason trap instead of the keep-alive: `TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected` (the immediate resume stays still with `KeepAliveTimeout` 1 s, and one `DisallowDataUpload` on the leecher moves it). The existing finding-12 test keeps its default-config form.

### Amendment 2 (2026-10-18, owner approved after the branch review)

- **The first-byte exit is not exact.** The tech-lead review found the hole: when the pause leaves a residual in flight (an unlimited sender, a slow receiver), the residual's chunks arrive after `AllowDataDownload`, so `ResumeDownload` returned at the first byte, and the last Reject came after that and set the trap. Reproduced with `TestResumeDownloadAfterAPauseWithAResidualInFlightKeepsMoving` (`v1-long`, sender unlimited, receiver 256 KiB/s): with the first-byte API 3 of 3 runs stood still; a variant that waited for the residual to drain before returning still stood still in 2 of 3.
- **Remedy:** `ResumeDownload(ctx, t, nudge, limits)` keeps watching until nothing is missing or its context ends. After every wait with no payload it calls `nudge`; the wait starts at `Nudge` (1 s), doubles with each nudge up to `MaxNudge` (16 s) and resets when payload arrives. The caller (025) runs it for as long as the torrent is not paused and runs `nudge` under its per-torrent lock as `Nudge(t, uploadAllowed)` with the policy read at that moment (security review M1: a stale `uploadAllowed` captured at the call could undo a global stop applied during the resume). A context already done allows nothing (security review M2). `Downloader` shrinks to `AllowDataDownload`, `BytesMissing`, `Stats`; the nudge uses `Switch` (`DisallowDataUpload`, `AllowDataUpload`). Cost of an idle watcher: one `Stats()` every 25 ms per resumed, incomplete torrent, and at most one nudge per 16 s once the swarm has nothing to give.
- `Limits` gains `MaxNudge` and a validation (`ErrInvalidLimits`); `Confirmation` gains `Handed` (payload handed to connections after `Applied`) so tests assert the local rule through the API.

## Stage log

| Stage | Commit | Result |
|---|---|---|
| A — measurements | — (scratch, not committed) | done; H1 refuted for finding 12 → plan amendment 1 approved by the owner |
| B, C, D — swarm and fixture, `transferswitch` and engine scenarios, refactor with the remedy | one commit with E | `pass` (see below) |
| E — documents, review, gate, QA | same commit | review → plan amendment 2 approved by the owner; see below |

## Stage A — measurements

**Environment:** Apple M4, macOS 27.0.1 arm64, Go 1.27.2, anacrolix/torrent v1.61.0; loopback swarm, `sendspace`/`recvspace` 128 KiB, send and receive autotuning up to 4 MiB. Temporary scratch tests (`internal/torrentengine/zz_scratch_test.go`, `internal/torrentengine/filestore/zz_scratch_test.go`), not committed; the numbers are kept here. A stall is a gap of at least 1.5 s with no completed byte, or no completion within the window.

### A1 — the stalls and their causes

| Measurement | Configuration | Result |
|---|---|---|
| Finding 12, immediate `DisallowDataDownload` → `AllowDataDownload` | `v1-large`, 256 KiB/s, `-race`, `KeepAliveTimeout` 3 s | 4 of 4 stood still for the whole 12 s window: **H1 refuted for 12** |
| Finding 12, engine status in the stall | same, 1 run | leecher unchoked and interested, `reqq 0+0` (no request, none awaiting a Reject), 63 wanted pieces at normal priority; seeder not choking, its queue empty |
| Finding 12, nudge in the stall after 2 s | no nudge / seeder upload off 50 ms on / leecher `DisallowDataUpload`+`AllowDataUpload` | 0 of 5 / 5 of 5 / 5 of 5 completed; moving again within 70 ms |
| Finding 12, remedy loop (nudge each 1 s while no payload since the resume) | `KeepAliveTimeout` 1 s, 20 runs, `-race` | 20 of 20 completed; 19 needed one nudge, 1 none; first byte after the resume ≤ 1.05 s |
| Finding 22, fresh `v1-large` transfers, no switch | classic IO, unlimited, `-race`, 300 runs each | default keep-alive: 11 stood still past 8 s (3.7%); `KeepAliveTimeout` 1 s: 0, longest gap 1.01 s: **H1 holds for 22** |
| Finding 14, spike sequence (seeder upload off 1.5 s, then on) | `v1-large`, 64 KiB/s, 14 `yes` processes, `KeepAliveTimeout` 1 s, `-race`, 60 runs | 0 stalls; longest gap after the allow 280 ms |

**Cause of finding 12** (read in the source, confirmed by the nudge rows): `applyRequestState` (`requesting.go`) skips every new request when the update reason is `peerUpdateRequestsRemoteRejectReason` ("don't add requests on receipt of a reject … the peer will send an unchoked message"). The pause cancels every request; with Fast each cancel awaits a Reject. When download is allowed again before those Rejects arrive, the wanted chunks cannot be requested (they await acknowledgement); the last Reject empties the set and triggers an update with the reject reason, which requests nothing, and no later event asks again. A 5 ms gap avoided it in 007 because the Rejects came back while download was still disallowed; a 1 s gap did not always, because the Rejects queue behind piece data that the receiver reads at its rate limit.

**Cause of findings 14 and 22** (read in the source, confirmed by the keep-alive rows): `tickleWriter` is a `chansync.BroadcastCond.Broadcast`, a no-op when no waiter holds a channel; the writer takes its channel only after `fillWriteBuffer` released the client lock, so a request made ready in between is not sent until the writer runs again, and its keep-alive timer is the only generic waker.

### A2 — the interval

Rule A2 holds for 1 s: zero runs standing still past 1.5 s in the A1 sets (finding 22: longest gap 1.01 s; finding 14: 280 ms; finding 12 is not a keep-alive matter). Overhead on a connected pair whose leecher paused download, 10 s, `-race`: default keep-alive 0 protocol bytes out per side, 1 s keep-alive 40 bytes per side (4 B/s per idle useful connection). **`WakeInterval` = 1 s.**

### A3 and A4 — residual, drain and resume on `v1-long`

Boundary after 1 MiB received; `StopUpload` with the default limits; the receiver's payload counter sampled every 2 ms in its own goroutine; after 5 s of observation `AllowDataUpload`. 30 runs per row.

| Row (sender / receiver) | `-race` | Sent after `Applied` (local, all runs) | Residual at the receiver min / median / max | Last byte after `Applied` median / max | Confirmed after `Applied` max | First byte after `AllowDataUpload` median / max |
|---|---|---|---|---|---|---|
| 256 KiB/s / 256 KiB/s | yes | 0 | 64 / 80 / 80 KiB | 290 / 296 ms | 250 ms | 8 / 13 ms |
| unlimited / 1 MiB/s | yes | 0 | 784 / 816 / 1008 KiB | 777 / 970 ms | 250 ms | 10 / 15 ms |
| unlimited / 256 KiB/s | yes | 0 | 768 / 816 / 1008 KiB | 3.171 / 3.925 s | 250 ms | 9 / 12 ms |
| 256 KiB/s / 256 KiB/s | no | 0 | 16 / 16 / 32 KiB | 41 / 102 ms | 250 ms | 2 / 2 ms |
| unlimited / 1 MiB/s | no | 0 | 688 / 864 / 1008 KiB | 822 / 964 ms | 250 ms | 2 / 4 ms |
| unlimited / 256 KiB/s | no | 0 | 784 / 832 / 1008 KiB | 3.227 / 3.919 s | 250 ms | 2 / 2 ms |

Reading:
- The local rule holds: the sent-payload counter never moved after `DisallowDataUpload` returned (180 of 180), so every confirmation came after exactly one quiet window.
- The residual is what the sender had already written when the switch returned. With a rate-limited sender it is one or a few chunks; with an unlimited one it reaches about 1 MiB, the receiver's request window (the engine sends only requested chunks), whatever the receiver's rate. It drains at the receiver's pace: about residual ÷ receiver rate.
- No row completed the 16 MiB file, so a residual was never mistaken for completion.
- Upload moves again within milliseconds of `AllowDataUpload`: the Fast extension kept the peer's requests through the choke.

**Bounds asserted by the stage C tests** (measured max × 1.5, rounded up to 64 KiB / 250 ms): residual ≤ 1536 KiB per connection; drain ≤ residual bound ÷ receiver rate + 250 ms for an unlimited sender, ≤ 500 ms for a sender limited to 256 KiB/s; first byte after a start ≤ `WakeInterval` + 250 ms (measured ≤ 15 ms; a lost wake-up may add one interval).

## Stage B — swarm and fixture

**Environment:** as in stage A.

| Command | Result |
|---|---|
| `go test ./internal/torrentengine/testutil/swarm -run KeepAlive` (new `TestAPeerTakesItsKeepAliveTimeoutFromTheCaller`) | `fail` as expected: `unknown field KeepAliveTimeout in struct literal of type Options` |
| same after `Options.KeepAliveTimeout` | `pass` |
| `go vet ./internal/torrentengine/testutil/swarm` with the per-direction log tests and the old `counters.go` | `fail` as expected: `log.First undefined (type Log has no field or method First)` |
| `go test ./internal/torrentengine/testutil/fixture` with `TestTheLongFixtureLeavesRoomForAResidual` | `fail` as expected: `no fixture named "v1-long"`, `8 fixtures with payload, want 9` |
| same after the `v1-long` spec and `-args -update` | `pass` |
| `go test -race -count=1 -cover ./internal/torrentengine/transferswitch ./internal/torrentengine/testutil/...` | `pass`; fixture 100.0%, swarm 99.5% in isolation (the line after the classic-IO re-exec in `testmain.go`, covered under the gate through `filestore`, unchanged since 009) |

## Stage C — `transferswitch` and the engine scenarios

| Command | Result |
|---|---|
| `go vet ./internal/torrentengine/transferswitch` with `switch_test.go` only | `fail` as expected: `undefined: Limits` |
| `go test -race -count=1 -cover ./internal/torrentengine/transferswitch` | `pass`, 100.0% of statements |
| `go test -race -count=1 ./internal/torrentengine -run 'Upload|Stall|Rejected|Resume|Concurrent|Connection|Completed|Pause|Added' -v`, first run | 9 `pass`, 3 `fail`: the new-connection, add-under-the-stop and completed-torrent scenarios waited for a connection that never came. Cause in the source: `newConnsAllowed` refuses every new connection to a complete torrent that does not seed (finding 28). The scenarios were split: a new connection to a torrent that still downloads (accepted, no payload) and a complete torrent (no connection, no payload; after `AllowDataUpload` and a new connect it completes) |
| same scenarios after the split | `pass` |
| amendment 2: `go test -race -count=1 ./internal/torrentengine -run TestResumeDownloadAfterAPauseWithAResidualInFlightKeepsMoving` against the first-byte `ResumeDownload`, 3 runs | `fail` as expected, 3 of 3: no payload for the whole window after the residual |
| same against a variant that returned only after the residual drained, 3 runs | `fail`, 2 of 3: the trap still formed after the return |
| `go test -race -count=1 -cover ./internal/torrentengine/transferswitch`, 3 runs | `pass`, 100.0% of statements |
| the residual scenario with the watching `ResumeDownload`, 8 runs | `pass`, 8 of 8 |

The engine scenarios are proofs of the real engine's behavior with the remedy; their RED is the defect itself: `TestPinnedEngineStallsWhenDownloadIsAllowedRightAfterDisallowing` (default configuration) and `TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected` (1 s keep-alive, no nudge) stand still, and the A1 measurements above show each stall without its remedy.

Scenario logs worth keeping (first green run, `-race`): the chain S→A→L confirmed 250 ms after the switch, the leecher received 96 KiB after the command, the last byte 370 ms after it, and A downloaded 4.1 MiB meanwhile; `ResumeDownload` completed after 1 nudge with upload allowed and after 0 with upload stopped.

## Stage D — refactor with the remedy

| Command | Result |
|---|---|
| `go test -race -count=1 ./internal/enginespike -run TestRepeatedPausesCompleteWithTheProductKeepAlive` | `fail` as expected: `the clients started with keep-alive [0s 0s], want 1s for both` |
| `npm --prefix frontend test -- src/spike` with the tests updated (no `recoveries`) | `fail` as expected: 7 tests (the decoder rejects the shared example without `recoveries`; the text "0 stalls woken") |
| `env -C cmd/engine-spike go tool wails generate module` | `models.ts` loses `recoveries`; the runtime files' modes, changed by the generator, restored |
| `npm --prefix frontend test -- src/spike`; `npm --prefix frontend run lint -- src/spike` | `pass` (21 tests); no warning |
| `go test -race -count=1 ./internal/torrentengine/... ./internal/enginespike ./cmd/engine-spike` (after `keepMoving` and the spike wake-up were deleted) | `pass`; `internal/torrentengine` 82.5 s, `filestore` 44.1 s, `enginespike` 23.5 s |

## Stage E — review, documents, gate, QA

### Branch review (`/branch-review`, three agents, read-only)

| Lens | Finding | Resolution |
|---|---|---|
| tech-lead | `ResumeDownload` returned at the first residual byte; the last Reject could set the trap afterwards | amendment 2: the resume keeps watching until complete; `TestResumeDownloadAfterAPauseWithAResidualInFlightKeepsMoving` (RED 3 of 3 with the old API) |
| tech-lead, security M1 | the nudge's `uploadAllowed` was fixed at the call, so a nudge could undo a global stop or a pause applied during the resume | `nudge func()` run by the caller under its per-torrent lock with the policy read then; `TestAStopDuringAResumeIsNotUndoneByANudge` |
| security M2 | a done context still allowed download | context checked before the allow; `TestACanceledResumeAllowsNothing` asserts no engine call |
| security L1, tech-lead | the approved `BytesMissing` exit was dropped without a record | restored: `Downloader.BytesMissing`, `TestResumeDownloadAllowsAndReturnsWhenNothingIsMissing` |
| security L2 | the manual-pause test proved the download direction only, under a name that promised more | renamed `TestTheUploadSwitchNeverChangesTheDownloadSwitch`; ADR and feature doc say exactly that; the composition rule stays with 013/053 |
| security L3, L4 | the stop covers piece payload only; nothing enforces `WakeInterval` in the product | notes in 053 and 054 (protocol messages still flow), 018 (assert the interval in a test) |
| security L5 | zero `Limits` panicked in `time.NewTicker` | `Limits.validate`, `ErrInvalidLimits`, `TestLimitsThatCannotWorkAreRefused` |
| security warning | `Stats()` polling takes the client lock | note in 053 (stagger or batch a stop of many torrents) |
| tech-lead | the 018 note said `StopUpload`/`ResumeDownload` are the only callers of the switches | reworded: a stop only through `StopUpload`, a resume of download only through `ResumeDownload` |
| tech-lead | the chain test measured A's growth from the confirmed mark, not `Applied` | `Confirmation.Handed` (growth after `Applied`) asserted as 0 |
| code-reviewer | `DefaultLimits` was a mutable package variable; `Downloader` carried upload methods; interface checks lived in a test | `DefaultLimits()` function; `Switch` split out; compile-time assertions at package level |
| code-reviewer | quiet-window test timed by a sleeping goroutine; `log.Last` result unchecked; dead `chained` entry and `fixtureOf` in the concurrent test; the spike test could pass with no cycle | growth on a fixed `Stats` call; `ok` asserted; table simplified; `t.Fatalf` when the transfer completes before a cycle |
| code-reviewer | per-call nudge cap suggested | replaced by the doubling wait (at most one nudge per 16 s), owner decision in amendment 2 |
| code-reviewer | `Counters.Get` builds a small array per call | kept: three fields, test code only, an out-of-range `Field` is a programming error that panics in tests |
| code-reviewer, tech-lead | stage E results and the task file's stale paragraphs | this section; task file updated |

The engine-side alternatives raised by the owner (a disconnecting pause like Transmission and libtorrent; the pinned engine's disabled request-update timer) are recorded in the ADR and handed to 025.

### Final runs (after amendment 2 and the review fixes)

**Environment:** Apple M4, macOS 27.0.1 arm64, Go 1.27.2, anacrolix/torrent v1.61.0, 18 October 2026.

| Command | Result |
|---|---|
| `go test -race -count=1 ./internal/torrentengine/... ./internal/enginespike ./cmd/engine-spike` (before amendment 2) | `pass`, twice |
| same after amendment 2 | `pass`; `internal/torrentengine` 96 s, `enginespike` 36 s |
| same after the last test rewrites | `pass`; `internal/torrentengine` 95.2 s, `filestore` 44.8 s, `swarm` 13.2 s, `transferswitch` 2.3 s, `enginespike` 35.8 s |
| `go test -count=1 ./internal/torrentengine/... ./internal/enginespike ./cmd/engine-spike` (no `-race`) | `pass`; `internal/torrentengine` 92.9 s, `filestore` 42.0 s, `enginespike` 34.4 s |
| `go tool devtool check` | `GATE PASSED`: frontend 23 own files at 100% on four metrics; every block of every own Go file covered (unit tests + the `.app` runs of OctopusTorrent and EngineSpike); bindings of both apps match; product isolation holds (314 packages); `single code version` pass. `not_run`: engine integration (018), test-bridge UI tests (035). `updates`: `github.com/anacrolix/chansync` v0.7.0 → v0.8.0 is out of quarantine — deferred by the owner (anacrolix/torrent v1.61.0 requires v0.7.0; v0.8.0 is an `unsafe` performance rewrite with the same no-op `Broadcast`, so finding 26 stands), recorded in `toolchain-versions.md` |

The engine package's test time grew from about 15 s (009) to about 95 s with `-race`: most of it is the `v1-long` residual scenarios and the hold windows, which must outlast the measured drain. Recorded in `docs/development/testing.md`.

### `.app` QA of `EngineSpike.app`

The `macos-qa` agent ran on 18 October 2026, 10:53–10:58, on the uncommitted branch after `GATE PASSED`. It built the spike with `env -C cmd/engine-spike go tool wails build -trimpath` (ignored output), read the window through Accessibility, took no screenshots and quit every instance with the Apple Event only. Four instances (pids 77649, 78711, 79913, 81286); none was running before, none after.

| # | Scenario | Observed | Result |
|---|---|---|---|
| 1 | Open | `State: idle`, `0 / 0 bytes (0%)`, `0 peers, 0 engine clients started`, buttons Start, Pause, Resume, Reload UI; no "stall" or "woken" text anywhere in the window | `pass` |
| 2 | Start | `State: running`, `1 peer, 2 engine clients started`; 16384 → 147456 → 294912 bytes over 4 s; `lsof`: two TCP listeners on 127.0.0.1 and one loopback connection, no UDP, no other address | `pass` |
| 3 | Pause and Resume | 10 cycles over four transfers (1 + 4 + 4 + 1; five in one transfer did not fit, because the limiter's refilled bucket sends 200–250 KB after each Resume): after every Pause `paused` and the bytes flat for at least 3 s over 6–7 reads; after every Resume `running` and the bytes grew within the same second | `pass` |
| 4 | Completion with no wake-up code | `complete`, `1048576 / 1048576 bytes (100%)` with no further click, about 1 s after the last Resume (8 s after a 9 s pause) | `pass` |
| 5 | Reload UI while paused | `paused`, `49152 / 1048576 bytes (4%)`, `2 engine clients started` came back unchanged; Resume then completed | `pass` |
| 6 | Quit | `-128` as expected; every process gone within 3 s; `lsof -i` empty; no `$TMPDIR/octopus-engine-spike-*` left | `pass` |
| 7 | Negative: Pause, Start, Resume when complete | state and bytes unchanged | `pass` |

No resume needed anywhere near the 1 s wake interval: the spike's transfers kept moving with the product keep-alive and no wake-up code.

### Acceptance criteria

| Criterion | Evidence | Result |
|---|---|---|
| After the recorded boundary, our uploaded counter and the other side's received counter stop growing while the download continues | `TestStoppingUploadKeepsTheDownloadGoing` (A's sent payload after `Applied` 0, L's residual within the bound then flat, A keeps downloading); `TestRepeatedStopsWithDataInTheBufferEndWithinTheMeasuredResidual`; A3: 180 of 180 runs with no local growth after `Applied` | `pass` |
| Changing the global upload policy does not cancel a manual pause | engine boundary: `TestTheUploadSwitchNeverChangesTheDownloadSwitch`, `TestConcurrentCommandsKeepEachTorrentsSwitches`; a nudge never undoes a stop: `TestAStopDuringAResumeIsNotUndoneByANudge`. The composition rule (global on → `AllowDataUpload` only for torrents not manually paused) is 013's policy and 053's application | `pass` at the engine boundary; the policy is handed over |
| A failed control check blocks TR-08 and SET-08; independent download checks may continue | the control checks are ordinary tests of the gate: a failure fails `go tool devtool check`, which runs every step and never stops early, so the download scenarios still report; 053 and 054 depend on 010 in the plan | `pass` |
| All checks performed at the right level; nothing `not_run` or failed shown as passed | the tables above; `not_run` below | `pass` |
| Check: repeated toggle during active upload with data in the buffer, confirmed by a measured stop | the repeated-stops test: 5 cycles, unlimited sender, each stop confirmed from the counter and bounded at the receiver | `pass` |
| Check: concurrent add, start, pause and global change; download continues where allowed, pauses preserved | `TestConcurrentCommandsKeepEachTorrentsSwitches` (four torrents on one client, commands from goroutines) | `pass` |
| Work order 5: new clients and torrents, download completion, restart, manual pause | new connection, new import and restored entry added under the stop, completion under the stop, a complete torrent refusing connections; "restart" is a fresh client over the existing data (the restored entry), not a process restart of the product, which has no engine before 018 | `pass` |

### Not run

| Check | Why | Owner |
|---|---|---|
| "Desired policy saved" time in the timeline | no preferences store before 014/015 | 053 |
| Residual and drain on a real network (kernel autotuning against remote peers) | the loopback swarm only; the product claim does not depend on it | 054 (AS-18) |
| The stop in the product `.app` | the product links no engine before 018 | 054 |

### Refactoring and debt

- Replaced, not duplicated: the swarm's summed log functions (per-direction `First`/`Last`/`Delta`), the filestore harness's `keepMoving`, the spike's wake-up and its `recoveries` DTO field.
- `transferswitch` is a prototype that 018 moves into the adapter and 053/025 call; it is not linked by the product, which the isolation step proves.
- No `TODO`, skipped test, ignore directive or lowered threshold was added (gate: `escape hatches` pass).
- Open for later tasks, not debt: the policy (013), persistence (053), the product client configuration and its test (018), the pause design choice between switches and disconnecting (025).
