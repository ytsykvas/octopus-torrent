# 028 — Implement startup, single instance and quit of the .app

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [018](018-torrent-engine-adapter.md), [015](015-transactional-repositories.md), [017](017-local-diagnostics-privacy.md), [003](003-wails-macos-build-spike.md)  
**Requirements:** `SET-02`, `STACK-05`, `DATA-01`

## Outcome

Ensure a single torrent client and a single owner of DB writes, with a managed shutdown of the .app.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-02`, `STACK-05`, `DATA-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. Define a stable bundle ID, the local data directory and the root context.
2. Verify the single-instance policy so that two processes do not write to the same library.
3. Report readiness and startup errors; a frontend reload does not create a new torrent.Client.
4. On shutdown, first cancel imports, watchers and event publishing, then shut down the engine and close the DB.
5. Record the behavior of closing the window and Cmd+Q; do not add a separate background process.

6. Block concurrent ownership of one library before opening the engine and allowing transfer. Check a second process, a crash of the first, a partial startup and release of the lock; a file with a stale PID is not sufficient evidence of a live owner.
7. Create a new `runtimeID` for every Go startup. If the saved journal contains an unfinished deletion, such a record is restored stopped and is not started automatically until the operation is reconciled.

## Artifacts and change locations

**Proposed locations:** `main.go; internal/app/lifecycle; internal/platform`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/028-application-startup-quit.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Shutdown leaves no workers or subscriptions behind.
- [ ] Repeated initialization does not duplicate the client.
- [ ] The library is available without the internet after the DB is opened successfully.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] Two processes do not open the same payload for writing and do not restore the same library at the same time; after a crash of the owner, the next startup resumes work without manually deleting user data.

## Checks

- Quit the application while metadata is being obtained and during an active transfer.
- Check a second launch, a UI reload and a failure in the middle of initialization.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Wails: how the application is structured](https://wails.io/docs/howdoesitwork/). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): recover a panicking operation mutation as `failed`/`internal.unexpected`, and make the ledger stop step 1 of the ordered quit (operations, publisher, engine, database); today `OnShutdown` only stops the ledger, bounded by 5 s.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): `Client.Close` on anacrolix/torrent v1.61.0 returns before its listeners are closed (`go s.Close()`); the ordered quit must not report the engine stopped until the listen address refuses connections (see `swarm.Refuses`). Payload files stay mapped after `Client.Close` with the default mmap IO until the process exits; quit must not claim the files released.

**From 008** ([sqlite-driver](../../docs/decisions/sqlite-driver.md)): the data directory is `~/Library/Application Support/dev.tsykvas.octopustorrent`, override `OCTOPUS_DATA_DIR`; if 014 has not implemented it, this task does, with the workspace and gate overrides. `run` already reads the bundled SQLite's version at start and refuses to start without it; that reason goes only to stderr today, so give it a user-visible form. The database closes last in the ordered quit, through `library.Store.Close` (it has no context; its checkpoint waits at most the 5 s `busy_timeout`, so budget for it), whose `ErrBusy`/`ErrFull` mean the WAL stays for the next start, not a loss. Two processes on one library are told apart by the single-instance lock, not by SQLite's `ErrBusy`. Create exactly one `Store` per process at start and inject it: two stores on one file mean two writers, and the writer-only checkpoint no longer holds. Quit order: stop the ledger (no `Write` in flight), then `Close`, within the quit budget; consider a `Close(ctx)`. A read-only WAL file opens and fails only at the first write: probe it at start (`BEGIN IMMEDIATE; ROLLBACK`) so the reason is shown then. The application-level part of 008's read-only-folder and full-disk checks ("the application shows the reason") is `not_run` in 008, which opens no file database in the app; it is this task's (or 014's) to prove in the `.app`.

**From 009** ([engine-storage](../../docs/decisions/engine-storage.md)): startup calls `classicio.Require` first, creates the data directory, opens `Completion` (an open failure is an explicit error, never a fallback), then per torrent writes "not clean", applies `files.Decide`, `Completion.Distrust`, `TorrentStorage.Baseline` and adds the torrent. Quit stops asking for data, waits for the writes in flight, then `Client.Close` → `TorrentStorage.Stop` → `Completion.Close` → `Seal`, and stores the record and "clean" in one transaction. A killed process never reaches the last step, which is how a crash is seen. The step before `Client.Close` (stop asking for data, wait until the writes in flight have landed) is an open primitive the engine gives no signal for; measure the quit time of a torrent with many files too (`Seal` fsyncs every written file).

**From the 009 checkpoint** ([refactor after 009](../../docs/verification/refactor-after-009.md)): when this task (or 018, whichever touches `run` first) changes `main.go`, split the start test of `main_test.go`: extract `startedOptions(t)` for the repeated start-capture closure, assert the error formatter on its exact JSON text instead of equality with a second formatter, and give the formatter/stderr check its own test; window, assets and bind stay together.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and evidence of checks.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
