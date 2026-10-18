# 010 — Measure stopping upload without stopping download

**Status:** `done`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [007](007-engine-api-lifecycle-spike.md)  
**Requirements:** `TR-01`, `TR-08`, `SET-08`

## Outcome

Confirm with network measurements that upload of file data stops while the download continues.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-01`, `TR-08`, `SET-08`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. In a controlled local group of clients, sustain simultaneous download and upload; record the time the command is accepted, the time the desired policy is saved and the time it is actually applied.
2. Check `DisallowDataUpload` and `AllowDataUpload` on active connections, including when data is already in the transfer buffer.
3. Compare the sent-data counters of our client with the received-data counters of the other client; account for protocol messages separately.
4. From the measurements, determine the acceptable time to finish transfers already in progress and the maximum time to wait for confirmation of the stop. Exceeding this time must produce an error.
5. Repeat the check for new clients and torrents, download completion, restart and manual pause. Do not substitute a full torrent pause for stopping upload.

## Artifacts and change locations

**Actual locations:** `docs/decisions/global-upload-stop.md` (the register's name for O6), `docs/verification/010-upload-stop-and-pause-spike.md`, `docs/app/transfer-switches.md`, `internal/torrentengine/transferswitch`, `internal/torrentengine/upload_test.go`.
**Evidence of completion:** the record `docs/verification/010-upload-stop-and-pause-spike.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] After the recorded boundary, the counters of data uploaded and data received by the other side stop growing, while the download continues.
- [x] Changing the global upload policy does not cancel the manual pause of any torrent.
- [x] A failed control check blocks acceptance of TR-08 and SET-08; independent checks of basic downloading may continue.
- [x] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Repeat the toggle during active upload with data in the buffer; success is confirmed by a measured stop of the transfer, not by the method returning.
- Simultaneously add a torrent, start and pause other entries and change the global policy; downloading must continue where allowed, and manual pauses must be preserved.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

A failed result of this prototype blocks completion of tasks 053 and 054 and final acceptance of the whole agreed scope, but not the independent download and import tasks. Control returning from the flag-changing method is not confirmation that the transfer of file data has stopped.

**Basis for technical decisions:** [Upload implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/torrent.go). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): measure with `swarm.Record` (payload counters sampled per torrent, `Mark` for the command time, `Log.FirstPayload`/`LastPayload`/`PayloadAfter`, replaced in 010 by the per-direction `Log.First`/`Last`/`Delta`) on the rate-limited `v1-large` fixture. 006 recorded, without interpreting: after `DisallowDataUpload` on the seeder at 256 KiB/s, 0 payload bytes reached the leecher in the next second (50 ms sampling); `Count` separates payload from protocol bytes. `swarm.Connect` marks peers trusted (all share 127.0.0.1 and the engine bans by IP); a trusted peer that sends bad data is never banned and its piece is re-requested in a loop.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): the `DisallowDataUpload`/`DisallowDataDownload` fields of `AddTorrentOpts` are never read (finding 10): use the `Torrent` methods. Allowing download right after disallowing it stalls the transfer for good (finding 12: 20 of 20 runs at once, 2 of 20 with a 1 s gap). The seeder's upload switch stalls too, but rarely and only under CPU load (finding 14: 1 in 50–60 runs of the spike's sequence with 14 busy processes, never without load); a second off and on wakes it, and the spike app counts such wake-ups. The cause inside the engine was not established for either (010 found both: findings 25 and 26). This task owns the product's remedy and a test that measures it; `TestPinnedEngineStallsWhenDownloadIsAllowedRightAfterDisallowing` tracks finding 12.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): a fresh transfer on the closed loopback swarm sometimes stands still for good, with no upload switch used: one request sits unserved in the seeder's queue and both sides wait (finding 22). Measured with 300 fresh transfers per row and a 4 s limit: with `-race` 13 stalled with classic file IO (4.3%) and 5 with mmap (1.7%), without `-race` none. A second upload switch off and on, 50 ms apart, serves the request; the test helper `keepMoving` in `internal/torrentengine/filestore/run_test.go` does that when the seeder uploaded nothing for 1.5 s, and the count of wake-ups is logged. The product remedy and its measurement are yours; the helper is not product code. (Resolved in 010: `KeepAliveTimeout = transferswitch.WakeInterval`, finding 26; `keepMoving` deleted.)

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

## Execution result

**Date:** 2026-10-18. **Status:** `done`.

- **Changes:** `internal/torrentengine/transferswitch` (new prototype): `WakeInterval` (1 s keep-alive for every client), `StopUpload` (confirmed stop from the sent-payload counter, `ErrNotConfirmed` after 2 s), `ResumeDownload` + `Nudge` (resume without the finding-12 stall, watching until complete), `Limits`/`DefaultLimits()`. Real-engine scenarios in `internal/torrentengine/upload_test.go`; swarm `Options.KeepAliveTimeout` and per-direction log functions; fixture `v1-long`. Refactor: the filestore harness's `keepMoving` and the spike's wake-up (and its `recoveries` DTO field, bindings regenerated) deleted.
- **Findings:** 25 (cause of 12: no request after the last cancelled request is rejected), 26 (cause of 14/22: a lost writer wake-up, ended only by the keep-alive timer), 27–30 (no buffering after the switch and the residual, a complete stopped torrent refuses connections, Fast keeps requests through a choke, the nudge mechanism). ADR [global-upload-stop](../../docs/decisions/global-upload-stop.md) (O6 accepted for the engine part; production 053); feature [transfer-switches](../../docs/app/transfer-switches.md); TR-08 wording corrected.
- **Checks:** `go tool devtool check` → `GATE PASSED`; engine and spike suites `pass` with and without `-race`; `.app` QA of EngineSpike.app 7 of 7 `pass`; `/branch-review` findings resolved. Details and every RED/GREEN in the [report](../../docs/verification/010-upload-stop-and-pause-spike.md).
- **Not run:** the "policy saved" time (053), the residual on a real network and the product `.app` (054).
- **Limitations:** bytes already handed to a connection still arrive (up to about 1 MiB per connection on loopback, at the receiver's pace); the stop covers piece data only; the engine package's tests take about 95 s with `-race`.
- **Next task:** [011 — magnet metadata and the final folder](011-magnet-metadata-storage-spike.md).
