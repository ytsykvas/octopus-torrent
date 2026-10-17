# Engine storage: payload placement, verified-piece marks and the rehash rule

**Status:** `implemented` (prototype: tests and a test harness; the product does not link it before [018](../../product/tasks/018-torrent-engine-adapter.md))  
**Requirements:** `SET-02`, `TR-05`, `ADD-04`, `DEL-01`, `AI-05`  
**Related tasks:** [009](../../product/tasks/009-storage-and-completion-spike.md); next [018](../../product/tasks/018-torrent-engine-adapter.md), [020](../../product/tasks/020-destination-filesystem-service.md), [028](../../product/tasks/028-application-startup-quit.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md), [030](../../product/tasks/030-relink-missing-payload.md)  
**Last verified version:** branch `task/009-storage-completion`  
**Last updated:** 2026-10-17

## Goal and behavior

```
chosen folder (UI → bridge → Go, validated by 020)
  → classicio.Require()                       the process runs with classic file IO
  → filestore.NewTorrentStorage(saveRoot, completion, session, logger)
        engine file storage (part files off) inside a guard
  → client.AddTorrentOpt{InfoHash, Storage} + SetInfoBytes(info)
        payload goes to <saveRoot>/<name>[/…]; marks go to <dataDir>/piece-completion.sqlite
  → quit: Client.Close → Stop → Completion.Close → Seal  → files.Record
  → next start: files.Decide(record, …) → Completion.Distrust(pieces) → the engine hashes only those
```

- The payload is written only below the folder the user chose. The engine's data, and the marks, live in
  the application's data folder.
- After a restart the application believes the marks of a file only if the file is exactly as the earlier
  clean run left it; every other file is hashed by the engine, and a missing file is reported.
- A completion store that cannot be opened or written is an error that reaches the application. There is
  no fallback to memory.
- **Implemented in 009:** all of the above as a prototype with tests on the real engine, real disks and
  real volumes. **Not yet:** the adapter (018), the data directory and the stored record (014/028/029), the
  user-facing messages (020, 030).

The decision and the alternatives are in the ADR [engine-storage](../decisions/engine-storage.md).

## Architecture analysis before the change

- **Layers.** `internal/library` keeps the marks (repository: all SQL). `internal/torrentengine/filestore` is
  engine-adapter code: the only new package with engine types. `internal/files` holds the rule and
  imports no engine package. `internal/torrentengine/classicio` imports none either. All sit on the
  [architecture map](../../product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first).
- **Owners.** The service that creates the engine client creates the `Completion`, the `Session` and one
  `TorrentStorage` per torrent, and ends them in the order above. The engine never closes a storage it was
  given.
- **Side effects.** The guard and the storage write the payload files; `Completion` writes only its database;
  `Seal` syncs the written files. Nothing deletes anything.
- **After a crash.** There is no record, so the next start verifies everything.
- **Reused.** `library.Open` for the marks database, the 006 fixtures and swarm for every scenario, the
  008 child-process and disk-image patterns (now `internal/testutil/volume`), the 007 completion contracts.
- **Refactored.** The `hdiutil` helpers of `internal/library/disk_test.go` moved to `internal/testutil/volume`;
  `swarm.Main` shares its body with the new `swarm.MainClassicIO`.

## Contracts and data

| API | Contract |
|---|---|
| `classicio.Require(Process)` | nil when the variable is `classic`; an error for another value; otherwise restarts the process with it set |
| `library.OpenCompletion`, `LoadMarks`, `SaveMarks`, `Close` | Marks per (infohash, piece); a batch is one transaction; `ErrUnsupportedVersion` for another version or purpose |
| `filestore.OpenCompletion`, `NewCompletion` | `storage.PieceCompletion`; reads a torrent once; batches of ≤ 256 rows every 250 ms; sticky `Err`; `Persistent()` false; `Distrust`; `Close` flushes |
| `filestore.NewTorrentStorage` | A guarded engine storage for one torrent. A relative, unclean, missing or non-folder path, and an HFS+ folder, are errors |
| `TorrentStorage.Stop`, `Reads`, `Baseline`, `Seal` | Refuse and await operations; count piece reads; keep the files an earlier run left; sync and return the record (after `Stop`) |
| `filestore.Layout(saveRoot, info)` | The content root and every file path, by the engine's rule |
| `filestore.Refusing()` | The client's default storage |
| `files.Record`, `files.Decide`, `files.Decision` | What a run leaves; the rule; `VerifyAll`, `VerifyPieces`, `Trusted`, `Missing` |

Errors: `ErrRootChanged`, `ErrStopped`, `ErrNotStopped`, `ErrFileSystem`, `ErrNoSaveFolder`, `ErrClosed`,
`library.ErrUnsupportedVersion`. A failed write of marks is reported by `Err` and by every later `Set`.

## Scenarios and TDD

RED/GREEN commands and results per stage are in [report 009](../verification/009-storage-and-completion-spike.md).
The scenarios, all on the real engine in a closed loopback swarm:

- placement for `v1-single`, `v1-tree`, `hybrid-single`, `hybrid-tree`;
- clean restart (full and partial), a killed process, a changed or moved file, an edit while the
  application runs;
- FAT, exFAT and APFS volumes, a replugged volume;
- a save folder deleted or replaced, a volume pulled while leeching and while seeding, mark writes
  refused, a full volume at open.

## Implementation and reuse

`internal/torrentengine/filestore`: `completion.go` (marks), `guard.go` (`TorrentStorage`, `Seal`),
`storage.go` (`NewTorrentStorage`, `Layout`, `Refusing`), `session.go`. `internal/files`: `fingerprint.go`,
`trust.go`. `internal/library/completion.go`. Test support: `swarm.MainClassicIO`, `swarm.Options`
(`DefaultStorage`, `DataDir`), `internal/testutil/volume`.

Invariants: no operation reaches the engine once the save folder is not the recorded one; a mark is
trusted only through `Decide`; a session with any problem never produces a clean record.

## Checks and 100% coverage

`go tool devtool check` is the gate; `go test -race -count=1 ./internal/library ./internal/files
./internal/testutil/... ./internal/torrentengine/...` runs the module checks (about 60 s; the volume
tests need `hdiutil`). Every own Go block of the new packages is covered by unit tests; no `.app` profile is
needed because the product does not link them. `.app` scenarios and shutdown with discovery on are
`not_run`.

## Refactoring and debt check

No debt left. The prototype is the base of 018, not a stub: it is the code the adapter keeps. Open items
are owned by tasks: persistence of the record (014/029), the user messages (020/030), the `.app` re-exec
(018), the remedy for finding 22 (010).

## Usage and recovery

For tests: `swarm.MainClassicIO` in `TestMain`; the scenario harness in
`internal/torrentengine/filestore/scenario_test.go` shows a whole run (`start`, `download`, `end`).
Recovery: an unclean record or an unreadable marks store means "verify everything"; the marks of a torrent
are advisory and can always be rebuilt by hashing.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-17 | Created by task 009 | [report 009](../verification/009-storage-and-completion-spike.md), [ADR](../decisions/engine-storage.md) |
