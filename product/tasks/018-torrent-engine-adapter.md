# 018 — Create a torrent engine adapter with explicit resource management

**Status:** `planned`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [007](007-engine-api-lifecycle-spike.md), [005](005-domain-dto-and-bridge-contract.md), [013](013-transfer-policy-state-machine.md)  
**Requirements:** `AI-03`, `AI-04`, `AI-05`, `STACK-01`, `QA-03`, `QA-08`

## Outcome

Isolate anacrolix/torrent in a single adapter and define which objects and background tasks it creates, uses and shuts down.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-03`, `AI-04`, `AI-05`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Define our own contracts `Add`, `GetMetadata`, `ApplyPolicy`, `ReadSnapshot`, `Drop` and `Close`, including errors and cancellation.
2. Link record UUIDs to engine objects and distinguish objects created for the current import from already existing ones that this import does not manage.
3. Pass storage, clock and event receiver explicitly; return copies of state snapshots, not mutable engine objects.
4. Manage cancellation at the level of the application and of an individual torrent, and return resource shutdown errors.
5. Create a test double of the adapter for testing services and a real implementation for controlled network tests.

7. Check the existing shared contracts and pure policies before adding new methods. Remove duplicates and unnecessary dependencies in the adapter on green tests; every new error and resource-shutdown branch has a behavioral test and is part of the 100% coverage.

## Artifacts and change locations

**Proposed locations:** `internal/torrentengine/engine.go; anacrolix_engine.go`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/018-torrent-engine-adapter.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Engine objects are not passed across the Go–TypeScript boundary.
- [ ] A request made before metadata is obtained returns a defined pending state, not a falsely ready file list.
- [ ] A late call for a removed UUID is ignored and does not restore the record.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Simultaneously read a snapshot and remove a torrent, and also shut down the client while adding; operations must finish without races, hangs or access to released resources.
- Check `new=false`: cancelling a new import must not remove an already existing torrent. Run the concurrent scenarios with data race detection.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Services receive the adapter through our own contract and do not access library objects directly. The right to shut down an object is determined by who manages it, not merely by holding a reference to it. An accepted command and a completed operation have different results.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): adapter tests use `internal/torrentengine/testutil/{fixture,swarm}`; do not write another harness. Engine facts to handle in the adapter: v2-only torrents panic on add (recover or reject before calling the engine); `VerifyDataContext` returns before completion is stored (wait until no piece is queued, hashing or marking); `Client.Close` returns before listeners close; payload counters are `BytesReadData`/`BytesWrittenData`, protocol bytes are `BytesRead`/`BytesWritten`. `IncompleteError.Completed` is the engine's `BytesCompleted`, which counts unverified chunks; use piece states for verified progress. Payload files stay mapped after `Close` with the default mmap IO. Never add a metainfo the parser (019) and path plan (020) have not accepted: `AddTorrent` truncates existing files at zero-length paths before anything is downloaded.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): build on the verified API map and the completion contracts (`Drop` → `Closed()`; `Client.Close` → `Client.Closed()`, the listen address refuses, no engine goroutine). Ownership: only the import that got `new=true` may `Drop`; a `new=false` handle is shared, and its `Drop` ends the existing torrent; only the client's creator closes it. Never call `VerifyDataContext` after `Drop` (it hangs the client, finding 9). The adapter is the single place that refuses a torrent without an accepted path plan (finding 13: the engine renames and then overwrites user files at torrent paths) and builds specs only from info bytes whose hash it checked against the stored infohash (finding 11). The shutdown checks ran only in the closed configuration (DHT, trackers and port forwarding off): prove them again with those on. Refuse to start when `GOPPROF` or `TORRENT_STORAGE_DEFAULT_FILE_IO` is set, as `swarm.Main` does for tests. The engine contract tests in `internal/torrentengine/lifecycle_test.go` stay beside the adapter; replace the gate's "engine integration" `not_run` line with the adapter's tests, and remove the `github.com/anacrolix/` clause of `CheckProductIsolation` (`internal/devtool/isolation.go`) when the product links the adapter.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md), [feature](../../docs/app/engine-storage.md)): build the adapter on `internal/torrentengine/filestore`; it is the code you keep. Call `classicio.Require` first in `main` (the engine reads its file IO in an `init`, so the process must start with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic`; prove the re-exec inside the `.app`, or use `LSEnvironment`). Build with `-tags nosqlite,noboltdb` and make those tags a gate step; `disable_libutp` is yours with 026. Always: `DataDir` set, `DefaultStorage = filestore.Refusing()`, one `TorrentStorage` per torrent through `AddTorrentOpts{InfoHash, Storage}` and `SetInfoBytes`. Keep the shutdown order `Client.Close` → `TorrentStorage.Stop` → `Completion.Close` → `Seal`, after the application has stopped asking for data and the writes in flight have finished. Surface `Completion.Err()` and `Session.Problems()`. The engine's `Close` does not wait for storage calls (they reach the storage after it returns), and a storage that offers more than `Piece` and `Close` is refused by the guard. The refusal of `GOPPROF` is the first statement of `main`, before `classicio.Require` (the engine's `init` functions have already run, and the restarted process inherits the variable). The step before `Client.Close` — stop asking for data and wait until the writes in flight have landed — has no engine signal; the tests wait for 200 ms without payload. You own a better condition. `TorrentStorage` serves one torrent, and the guard's errors carry local paths: map them to codes, do not store or log them (DATA-01..03).

**From the 009 checkpoint** ([refactor after 009](../../docs/verification/refactor-after-009.md)): the gate has two pending steps, `engine integration` (yours) and `test-bridge UI tests` (035). Whichever of 018 and 035 replaces the last of them deletes the helper that builds pending steps and `notBuiltYet` in `internal/devtool/check.go` with it: uncalled code has zero-count blocks and fails the gate.

**From 010** ([global-upload-stop](../../docs/decisions/global-upload-stop.md), [feature](../../docs/app/transfer-switches.md)): set `ClientConfig.KeepAliveTimeout = transferswitch.WakeInterval` (1 s; it bounds the lost writer wake-up behind findings 14, 22 and 26). Add torrents with the upload switch set between `AddTorrentOpts`/`AddInfohash` and `SetInfoBytes` when the global stop is on (the add-option flags are dead, finding 10). Move `internal/torrentengine/transferswitch` into the adapter: a stop goes only through `StopUpload` and a resume of download only through `ResumeDownload`; assert in a test that the product client has the interval. A complete torrent under the stop refuses connections (finding 28): do not report that as a network error. Re-measure finding 26 when `github.com/anacrolix/chansync` changes: v0.8.0 was deferred by the owner ([toolchain-versions](../../docs/decisions/toolchain-versions.md#current-set)); its `BroadcastCond` keeps the no-op `Broadcast`, so it would not remove the keep-alive.

**From 011** ([magnet-pending-storage](../../docs/decisions/magnet-pending-storage.md), [feature](../../docs/app/magnet-metadata.md)): move `filestore.Deferred` and `internal/torrentengine/pendingmagnet` into the adapter. Check that the client is not closed before every add and drop what was added otherwise (finding 32: the engine adds to a closed client and never closes the torrent). Keep `IgnoreUnverifiedPieceCompletion` false (the deferred storage relies on the engine trusting a known completion). Serialize adds per infohash across every add path (`.torrent`, magnet, restore): while a magnet is pending, any other add of its infohash gets `new=false` and the deferred torrent. Build the one product client with `pendingmagnet.NewGate(cfg)`: the engine would otherwise assemble and parse a peer's metadata itself, and some shapes crash or stall it before any hook, while other peer messages crash it outright (findings 33–43). `Fetch` cannot check that its client has the gate, so 018 guarantees it. The gate also drops ut_holepunch (the engine panics answering one from a non-advertising peer), keeps only ut_metadata and ut_pex from a handshake, and caps request lengths and v2 hash messages (the engine keeps unbounded per-name and per-length counters). Decide whether to re-enable hole punching outside the pending phase, and whether other message types need attention. Re-run the pinned 33–43 tests and the wire tests on any engine upgrade: the gate relies on the engine ignoring a reject with trailing bytes and on the callbacks running before dispatch.

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
