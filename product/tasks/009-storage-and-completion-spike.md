# 009 — Check file placement and persistence of verified-piece state

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [007](007-engine-api-lifecycle-spike.md), [008](008-sqlite-driver-durability-spike.md)  
**Requirements:** `SET-02`, `TR-05`, `ADD-04`

## Outcome

Prove that torrent data is written into the chosen folder, and that the state of completed pieces is persisted on disk and safely restored.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-02`, `TR-05`, `ADD-04`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): APIs and assumptions not yet confirmed by a prototype.
- The results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Compare the supported storage constructors on single-file, multi-file and hybrid test torrents.
2. Determine the values of `saveRoot`, `contentRoot` and the physical path of every file; store the state of completed pieces separately from the files themselves.
3. Open the persistent completed-piece state store explicitly and return an error if this fails; do not hide the problem by falling back to an in-memory-only store.
4. After a partial and a full download, check a normal shutdown, a restart and a forced process termination.
5. Corrupt the saved state or modify the files and, based on the check results, record the rehashing rules.

## Artifacts and change locations

**Proposed locations:** `docs/decisions/storage-layout.md; docs/verification/recovery-spike.md`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** an entry `docs/verification/009-storage-and-completion-spike.md` or an exact link to an existing verification or build report in the handoff summary. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The torrent's physical files are located exactly in the directory chosen by the user.
- [ ] After a restart, existing correct bytes are not downloaded again.
- [ ] Unavailability of the persistent state store has an explicit result, and corrupted or modified files are not shown as 100% verified.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not performed or failed are not marked as successful.

## Checks

- Truncate a file, rename it and change its contents without changing its size; every mismatch must be detected without a false completed state.
- Forbid writing the completed-piece state, crash the process and detach an external volume; the program must report the problem and keep the library and existing files.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and in the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

If the persistent state store is unavailable, the application must explicitly show an error or a limited operating mode. A hidden fallback to an in-memory-only store is unacceptable. A failure of this store must not destroy the user's saved intentions in the library.

**Basis for technical decisions:** [Storage API](https://pkg.go.dev/github.com/anacrolix/torrent/storage), [Completion source](https://raw.githubusercontent.com/anacrolix/torrent/master/storage/piece-completion.go). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): on anacrolix/torrent v1.61.0 the default file IO (mmap) fails to write a chunk that spans a zero-length file ("writing received chunk 0: invalid argument", then the download is disabled): fixture `v1-tree` never completes, `v1-tree-no-empty` does; `TORRENT_STORAGE_DEFAULT_FILE_IO=classic` (read in the engine's `init`, so only via the process environment) avoids it. With cgo on (Wails) the default piece completion is SQLite through cgo (`go-llsqlite/crawshaw`), bbolt without cgo; build tags `nosqlite,disable_libutp` remove both C dependencies. A BEP 52 single-file torrent is stored as `<dir>/<name>/<name>`. Decide storage IO, piece completion and file placement with these facts; the swarm uses in-memory completion only for tests. The default mmap IO also never unmaps payload files: they stay mapped after `Client.Close` until the process exits (tracked by `TestPinnedEngineKeepsPayloadFilesMappedAfterClose`), so trashed files keep their disk space and an external volume cannot be ejected.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** a minimal correct implementation, tests and the required regression; **100% of all owned executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, the actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock it and the independent tasks that can continue.
- Update the documentation, the contract and the README if the API, structure or build commands changed; do not leave required context only in the chat.
