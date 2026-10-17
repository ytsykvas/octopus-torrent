# Engine spike app

**Status:** `implemented` (temporary: task 034 deletes it)  
**Requirements:** `AI-05`, `AI-07`, `STACK-03`  
**Related tasks:** [007](../../product/tasks/007-engine-api-lifecycle-spike.md); deleted by [034](../../product/tasks/034-wails-facade-real-bindings.md)  
**Last verified version:** task 007 ([report](../verification/007-engine-api-lifecycle-spike.md))  
**Last updated:** 2026-10-17  
**See also:** [ADR engine-lifecycle](../decisions/engine-lifecycle.md), [test torrents and the swarm](test-torrents.md), [quality gate](quality-gate.md)

## Goal and behavior

`EngineSpike.app` is a separate Wails window. It proves the whole path engine → Go DTO → Wails
binding and event → React with real bytes, before the product's adapter (018) and real bindings
(034) exist:

```
React SpikeApp (frontend/src/spike)
  ├─ subscribe("enginespike:transfer")      first, so no event is lost
  ├─ Snapshot / Start / Pause / Resume      through frontend/src/spike/bridge.ts
  │     → frontend/spike/wailsjs/go/enginespike/API.js   (generated)
  │     → enginespike.API (the only bound type)
  │     → enginespike.Service: one mutex, revision++ per state
  │          seeder ⇄ leecher: two swarm peers on 127.0.0.1, 1 MiB of v1-large at 64 KiB/s
  └─ every 250 ms: runtime.EventsEmit(Transfer) until complete
```

Its states are `idle`, `running`, `paused`, `complete` and `failed`:
- **Start** creates the spike's own temporary folder, the payload and two clients, then connects
  them. While a run exists, Start returns that run.
- **Pause** stops the seeder's upload, and **Resume** lets it upload again. This is not the product's
  pause policy (010, 013, 025). The leecher's download switch is avoided because of engine
  finding 12.
- **A stall is woken and counted.** When a running transfer stands still for 2 s (8 ticks), the
  ticker switches the seeder's upload off, and on again at the next tick, and adds one to
  `recoveries`. The page shows the count. The pinned engine can leave the transfer standing still
  after the upload is allowed again (engine finding 14), rarely and under CPU load.
- **Reload UI** reloads the page through `WindowReloadApp`. A reload creates no client:
  `clientsStarted` stays at 2.
- **Quitting** ends the service's lifetime first, so a Start still in progress gives up instead of
  delaying the quit. It then closes both clients and removes only what the spike created: its
  `seeder` and `leecher` folders recursively, then its own folder, which must be empty by then. A
  folder with anything else in it is kept, and the failure is reported.

## Architecture analysis before the change

- **Why a separate app.** The task forbids the spike's commands in the working build, and the
  engine must not enter the product before 018. The owner chose a separate app over a temporary
  feature in the product.
- **What the spike reuses:**
  - the network test materials of 006 (`testutil/fixture`, `testutil/swarm`);
  - the contract decoders and `callGo` of the product's bridge;
  - the gate.
- **Its own parts:** the page, the bindings, the bundle ID and the `main`.
- **Lifecycle.** `Service` owns the lifecycle and is never bound, so Wails cannot expose
  `Startup` or `Shutdown` to the page. No bound method takes an argument, so nothing from the page
  reaches the engine or the file system.
- **Boundary exceptions, until 034** ([ADR](../decisions/engine-lifecycle.md#decision)):
  `internal/enginespike` uses anacrolix types outside `internal/torrentengine`, and
  `frontend/src/spike/bridge.ts` calls generated bindings outside `frontend/src/bridge`. The
  gate's product isolation step keeps both out of the product.
- **Engine environment.** A linked engine reads `GOPPROF` at start-up. The spike does not refuse
  it: started from Finder it gets launchd's environment, and the product gets that guard in 018.

## Contracts and data

`enginespike.Transfer`, with `internal/enginespike/testdata/transfer.json` as the shared example
that both test suites check. The same file lists the five states once; both suites compare their own
list with it:

| Field | Type | Meaning |
|---|---|---|
| `revision` | decimal string (`bigint` in TS) | Order of the published states. The page keeps only the highest. |
| `state` | `idle` \| `running` \| `paused` \| `complete` \| `failed` | |
| `bytesCompleted`, `length` | decimal string | Verified bytes at the leecher, out of the total. |
| `payloadRead`, `payloadWritten` | decimal string | Piece data the leecher received and sent. |
| `peers` | integer | Active peers of the leecher's torrent. |
| `clientsStarted` | integer | Engine clients this service created. |
| `recoveries` | integer | Stalls the ticker woke (engine finding 14). |
| `error` | string | Why the last Start failed; otherwise empty. |

- **Errors.** A refused command comes back as an `Error` that `callGo` maps to
  `bridge.callFailed`, with Go's text in `params.detail`:
  - `the spike has not started yet`;
  - `the spike is stopped`;
  - `no transfer is running`.
- **Event.** `enginespike:transfer` carries one `Transfer`. A payload that breaks the contract
  reaches the page as `contract.invalidDto`.

## Scenarios and TDD

The RED and GREEN record is in the [report](../verification/007-engine-api-lifecycle-spike.md).

- **Go** (`internal/enginespike/service_test.go`, real swarm, `-race`):
  - a real transfer whose events never go backwards;
  - Start twice keeps one run;
  - Pause holds the bytes, and Resume completes the transfer;
  - a repeated Pause;
  - a stall (a dropped "allow") is woken once, counted, and the transfer completes;
  - Shutdown during a Start ends the Start promptly;
  - nothing beside the spike's folder is removed, after a run or a failed Start, and a folder with
    foreign content is kept;
  - 20 concurrent commands get unique revisions;
  - Shutdown during a transfer releases everything, and nothing is published after it;
  - commands outside a run are refused;
  - each failed Start (seeder, leecher, folder, payload, unknown fixture, unreadable metainfo, an
    engine panic, missing payload) releases what it created, and Start can be retried;
  - the shared example.
- **Go** (`cmd/engine-spike/main_test.go`):
  - `run` binds only the `API` and wires the lifecycle;
  - a start failure is reported;
  - a shutdown failure is reported within its limit.
- **TypeScript** (`frontend/src/spike/*.test.ts(x)`):
  - the decoder against the shared example;
  - the bridge calls and events;
  - subscribe before snapshot;
  - the revision rule for events, the snapshot and command replies;
  - errors, and a later successful reply clears a refused command;
  - StrictMode and unmount;
  - reload;
  - the entry point.

## Implementation and reuse

| Path | Role |
|---|---|
| `cmd/engine-spike/` | `main.go`: `run(start, stderr, svc)`; `wails.json` (`frontend:dir` `../../frontend`, `wailsjsdir` `../../frontend/spike`); `build/darwin` plists with the bundle ID `dev.tsykvas.octopustorrent.enginespike`. The page is embedded from `assets/`, which is not named `dist` because `wails build` copies `frontend/dist` over any other embedded `dist`. |
| `internal/enginespike/` | `Service`, `API`, `Transfer`. Seams are package variables used only by the tests. |
| `frontend/spike/index.html`, `frontend/vite.spike.config.ts` | The page entry, built into `cmd/engine-spike/assets` (ignored by Git) by `npm run build`. |
| `frontend/spike/wailsjs/` | The spike's generated bindings, kept apart because Wails deletes `wailsjs/go` of the directory it writes. |
| `frontend/src/spike/` | `bridge.ts`, `SpikeApp.tsx`, `main.tsx`. |

## Checks and 100% coverage

```sh
go test -race -count=1 ./internal/enginespike ./cmd/engine-spike   # the service, 100% of statements
npm --prefix frontend test -- src/spike                            # the page, 100% per file in the gate
npm --prefix frontend run build && (cd cmd/engine-spike && go tool wails build -clean -trimpath)
open cmd/engine-spike/build/bin/EngineSpike.app
osascript -e 'tell application id "dev.tsykvas.octopustorrent.enginespike" to quit'
```

- `go tool devtool check` lists the app in `wailsApps`. It regenerates its bindings, builds it with
  coverage, launches it and quits it by its bundle ID, so its `main()` is covered.
- The product isolation step proves that `OctopusTorrent.app` links none of the spike, test
  support or the engine.

## Refactoring and debt check

The spike is temporary by design, with an owner and a deadline: 034 deletes it. It is not debt, but
034 must remove all of these:
- `cmd/engine-spike`, `internal/enginespike`, `frontend/src/spike`, `frontend/spike`;
- `vite.spike.config.ts` and the second half of `npm run build`;
- the `tsconfig` and `.oxlintrc` entries;
- its `.gitignore` lines;
- its `wailsApps` row (the product isolation step stays);
- this document.

## Usage and recovery

- **The window shows `not loaded`:** the snapshot call failed. The alert shows the code and Go's
  text.
- **`failed`:** the alert shows why. Start again; the failed run already released what it created.
- **The gate reports "EngineSpike is already running":** quit it, then run the gate again.

## Change history and evidence

- 2026-10-17, task 007: the spike app, its gate rows and the engine findings 9–14
  ([report](../verification/007-engine-api-lifecycle-spike.md), [ADR](../decisions/engine-lifecycle.md)).
