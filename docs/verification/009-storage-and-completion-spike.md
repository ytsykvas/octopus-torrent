# Report 009: file placement and persistence of verified-piece state

**Task:** [009](../../product/tasks/009-storage-and-completion-spike.md)  
**Date:** 17 October 2026  
**Change version:** branch `task/009-storage-completion` (commits are listed per stage)

## Session architecture plan

The owner approved this plan on 17 October 2026, after an adversarial review (5 → 7 of 10). The three owner
decisions (rehash rule, part files off, system scenarios as Go tests with real child processes and
volumes) are listed in the plan. The approved plan follows verbatim.

### Context

Task [009](../../product/tasks/009-storage-and-completion-spike.md) (P1 spike; requirements `SET-02`, `TR-05`, `ADD-04`) owns register item **O5**: engine storage backend, payload location, durable piece completion → `docs/decisions/engine-storage.md`. Dependencies 007 and 008 are `done`. The task must prove:

- files land exactly in the folder the user chose;
- after a restart, correct bytes are not downloaded again;
- an unavailable completion store is an explicit error, never a hidden in-memory fallback;
- corrupted or modified files are never shown as verified;
- a crash, refused completion writes and a detached volume keep the library and the existing files and report the problem.

Nothing changes for the owner in the product: it still does not link the engine (018 does).

**Owner decisions (2026-10-17):**
1. Completion marks are trusted only after a clean quit; otherwise the data is verified first. (Refined below after review: distrust is per file, and the fingerprints are taken when a file is verified, not only at quit.)
2. Engine part files are **off**.
3. System scenarios are Go tests with real child processes, real disks and `hdiutil` volumes (as in 008). `.app` scenarios are `not_run`, handed to 029/052.

### Facts read in the pinned source (v1.61.0), each asserted by a test

- `storage.NewFile`, `NewMMap`, `NewFileWithCustomPathMaker` and `NewFileOpts` (part files off, nil completion) call `pieceCompletionForDir`, which logs and **falls back silently to the in-memory store** when the store cannot open (`storage/piece-completion.go:67-77`).
- File IO is **mmap** unless `TORRENT_STORAGE_DEFAULT_FILE_IO=classic` is set before the process starts (read in the storage package's `init`, `file-io-mmap.go:23-43`). mmap fails chunks over zero-length files (006), never unmaps, and sizes a file to full length on first write.
- Classic IO opens the file on every chunk write with `O_CREATE` and, on `ENOENT`, runs `MkdirAll` and retries (`file-torrent-io.go:78-101`, `file-io-common.go:15-33`). **A vanished save folder or a detached volume is silently recreated on the internal disk.**
- Stored `Ok=true` completion is never hashed (`torrent.go:2917`). `filePieceImpl.Completion` only checks that each file exists and is long enough (`file-piece.go:81-147`): a same-size rewrite stays "complete", and a missing or truncated file marks its pieces incomplete with `Ok=true` (they are re-downloaded, not hashed). That check also calls `Set(false)` on **our persistent store** for every piece of a file it cannot read (`file-piece.go:119-128, 149-166`), wiping marks.
- `Completion.Ok=false` makes the engine hash that piece at add (`queueInitialPieceCheck`, `torrent.go:2905-2922`).
- A failed `Set(true)` is only logged and the piece stays complete in the session (`torrent.go:2643-2657`). A `Get` error disallows download (`torrent.go:1779-1785`). `Set(true)` runs before the payload flush (`file-piece.go:169-179`).
- `AddTorrentOpt` ignores the storage error (`client.go:1536`); `SetInfoBytes` returns it (`torrent.go:1955`).
- Chunk writes and `MarkComplete` run without the client lock; `Client.Close` waits only for `storageLock` (`torrent.go:1117`). So storage calls can still arrive after `Close` returns.
- `Get` runs once per piece inside `onSetInfo`, under the client lock (`torrent.go:600`); `Set` runs on the hasher goroutine.
- Per-torrent storage is never closed by the client; `fileTorrentImpl.Close` does nothing (`file-torrent.go:114`).
- Engine stores: crawshaw SQLite opens in **WAL** by default (`crawshaw/openflags.go:52-54`), so the 008 ADR's statement "rollback journal" is wrong and is corrected; bbolt runs `NoSync` and `Close` never syncs.
- Existing files at torrent paths: a zero-length path is truncated at add (`file-client.go:97-98`); with part files off, writes go straight into existing files (007 #13); `isSubFilepath` is a string check only, so a symlinked `<saveRoot>/<name>` is followed (`file-client.go:92`).
- Placement with `NewFileOpts` defaults: v1 single `<saveRoot>/<name>`; v1 multi `<saveRoot>/<name>/<path…>`; v2/hybrid single `<saveRoot>/<name>/<name>`, no pad files.

### Design decisions (go into the ADR `engine-storage.md`)

- **Storage.** One `storage.NewFileOpts` per torrent through `AddTorrentOpts.Storage`: `ClientBaseDir = saveRoot`, part files off, explicit completion. `ClientConfig.DefaultStorage` is a storage whose `OpenTorrent` refuses, so nothing is written without a chosen folder. Add path: `AddTorrentOpts{InfoHash, Storage}` → `SetInfoBytes(mi.InfoBytes)`; its error is the add's error.
- **Classic IO.** The product must start with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic`. New package `internal/torrentengine/classicio` (no engine import) has `Require(lookup, exec, executable, args, environ)`: nil when classic, an error for any other value, and `exec` of itself with the variable added when unset. Provisional for 018's `main`; `LSEnvironment` in the plist is the recorded alternative. This replaces 007's rule "refuse when set".
- **Guard wrapper** (`filestore`, wraps `ClientImpl`/`TorrentImpl`/`PieceImpl`). Before every read, write, completion check and `OpenTorrent` it checks that `saveRoot` still names the directory recorded at open (device and inode), and refuses otherwise. A refusal is an error, so the engine disallows download and never reaches `MkdirAll`; `Completion()` returns the error without delegating, so marks are not wiped. It also:
  - records every read, write, completion and `OnWriteChunkError` error as the session's storage error (the signal the rehash rule uses);
  - counts piece reads (the proof that nothing was hashed);
  - after `Close`, refuses new writes and waits for those in flight.
  First step of stage C: check which optional interfaces and `TorrentImpl` fields (`NewReader`, `NewPieceReader`, `WriteTo`) the file storage provides, and forward them or record what hiding them changes.
- **Completion store.** Our own SQLite file `<dataDir>/piece-completion.sqlite`, opened by `library.Open`. The library API is batch-shaped, not per row: `LoadMarks(ctx, infohash)` and `SaveMarks(ctx, batch)` (one transaction). `filestore.Completion` implements `storage.PieceCompletion`:
  - loads a torrent's marks once into memory; every `Get` is answered from memory;
  - `Set` updates memory and queues the row; one goroutine commits batches (every 250 ms or 256 rows);
  - a failed commit is sticky: `Err()` and the next `Set` and `Close` return it; an open failure is an error, so there is no hidden fallback;
  - `Close` flushes synchronously and waits for in-flight calls; a call after `Close` returns `ErrClosed` and marks the session unclean;
  - `Persistent()` is decided by measurement (stage A rule 5): `true` makes the engine fsync the whole payload file on every completed piece; `false` plus an fsync of every payload file at clean quit is expected to win, and is sound because a crash is verified anyway.
  - Marks are advisory: they are never trusted on their own (see the rule), and an unreadable store is an explicit error whose rebuild is a separate action (029).
- **Rehash rule** (policy code in `internal/files`, no engine import; 029 wires it). Input: the session record kept by `filestore` (clean quit, no storage error, per-file fingerprints) and the files now. Per file:
  1. a file the record lists as present is missing → `Missing` (report; 030; nothing is added or downloaded). A file never written (selective or partial download) is not an error;
  2. the record is absent or not clean (crash, storage error, closed-late write, store unreadable) → `Verify` all pieces;
  3. the file system is not `apfs` or `hfs` (`statfs`), or `st_dev` differs from the record (replugged volume) → `Verify` all (never `Missing`);
  4. the file's size, mtime, ctime or inode differs from its fingerprint, or on `hfs` (1 s timestamps) its mtime is within 2 s of the fingerprint → `Verify` that file's pieces;
  5. otherwise `Trust`.
  Fingerprints are taken when the last piece of a file is verified, and at clean quit for files still downloading, after the payload fsync. So a same-size edit made while the app runs changes the fingerprint of a completed file and is caught. The limit: an external edit to a file that is still downloading, made while the app runs, is not detected until its pieces are verified (recorded).
  Verification is per file: the store answers `Ok=false` for the distrusted pieces and the engine hashes only those (a verified piece clears it). This replaces `IgnoreUnverifiedPieceCompletion`. "Verify before connecting peers" is dropped: the engine requests nothing for pieces of unknown completion.
- **Product build tags** `nosqlite,noboltdb` (no C SQLite from the engine) are proven buildable here; making them a gate step and putting them in the Wails build is 018's task. `disable_libutp` is a transport question for 018/026.

### Boundaries and files

Not changed: React, bridge, facade, bindings, product `main.go`, `go.mod`, the isolation step (the product still links none of this).

| File | Change |
|---|---|
| `internal/library/completion.go`, `completion_test.go` | new: `OpenCompletion`, `LoadMarks`, `SaveMarks`, `Close`; table `piece_completion(infohash BLOB, piece INTEGER, complete INTEGER, PRIMARY KEY(infohash, piece)) WITHOUT ROWID`, `user_version` 1; a newer version → a distinct `ErrUnsupportedVersion` (never `ErrCorrupt`, so 029's rebuild cannot destroy a newer library). Note for 014: this schema precedes its migration policy. |
| `internal/testutil/volume/` | new package moved from `internal/library/disk_test.go`: `Attach`, `Fill`, `Detach`, left-over image checks, behind a `testing.TB` and a command-runner seam so its error branches are covered; `internal/library/disk_test.go` uses it |
| `internal/torrentengine/classicio/` | new |
| `internal/torrentengine/filestore/` | new prototype that 018 builds on: `completion.go`, `guard.go` (wrapper and session record), `storage.go` (`NewTorrentStorage`, `Refusing`, `Layout`), tests; its `TestMain` combines `swarm.MainClassicIO` with the left-over-image check |
| `internal/files/` | new: `fingerprint.go`, `trust.go` (the rule), tests |
| `internal/torrentengine/testutil/swarm/` | edit: `MainClassicIO(m)`; `Options.Storage` (a `storage.ClientImpl` and its own `DataDir`) and `AddWith(AddTorrentOpts)` so a peer can use our storage and a separate data dir; `Settings.Storage` text follows |
| `internal/torrentengine/storage_test.go` | new: default-engine facts |
| docs | see Documents |

### Stages, tests first

The work is committed in two checkpoints: **Session 1** = A, B, C1, C2; **Session 2** = C3, D. Each checkpoint passes `go tool devtool check`.

**A — measurements (scratch tests, deleted; numbers go in the report).** Rules declared before measuring.
1. Open failure is an error: missing directory, read-only directory, path is a directory, garbage file.
2. The store is durable when `Close` returns; after SIGKILL it opens and holds only whole committed batches.
3. A second process opening the same file gets an error within 6 s.
4. A mark is read back after reopen, with `Persistent` either way.
5. `Persistent`: run a 64 MiB loopback transfer (256 KiB pieces, `-race` off) with `Persistent()=true` and `=false` over a map-backed baseline that reports the same value (the stock map store reports `false`, so it is not a fair baseline for `true`). Choose `false` when `true` costs more than 2× the baseline; the owner decides if neither stays within 2×.
6. Cost of the guard's per-operation `stat`; p99 of `SaveMarks` for a 256-row batch.
7. Build check: `-tags nosqlite,noboltdb` builds `filestore` and `go list -deps` shows no crawshaw cgo.
The engine's bolt and crawshaw stores are rejected by source facts (silent fallback in constructors, `NoSync`, WAL); each gets one fact test, not a probe suite.

**B — library marks and the volume package.**
- REFACTOR first: `go test -race -count=1 ./internal/library` green, then the volume package (RED: package missing; GREEN with 100% of its statements), library green again.
- RED `go test ./internal/library -run Completion` → `undefined: OpenCompletion`. Tests: round trip; unknown piece absent; overwrite; survives reopen; batch is atomic; read-only and missing directory; a garbage file → `ErrCorrupt`; a newer `user_version` → `ErrUnsupportedVersion`; a full volume (the filler leaves under 64 KiB; a `SaveMarks` of 20000 rows must grow the file) → `ErrFull` with earlier marks intact; canceled context → `ErrCanceled`.

**C1 — placement and the guard.**
- RED: `go test ./internal/torrentengine/classicio ./internal/torrentengine/filestore ./internal/files` → undefined symbols.
- Placement: `v1-single`, `v1-tree` (the empty file completes under classic IO), `hybrid-single`, `hybrid-tree`: after a full download the files under `saveRoot` equal `Layout`, with the fixture's lengths and SHA-256; no `.pad`, no `.part`, no database; `DataDir` and the working directory hold only `piece-completion.sqlite*`. The constructor comparison (`NewFileOpts` defaults, infohash dir maker, `NewMMapWithCompletion`) on `v1-single`, `hybrid-single` and `v1-tree-no-empty` is recorded as facts.
- `Refusing()`: a torrent without its own storage fails to add and writes nothing. A storage error at add surfaces through `SetInfoBytes`.
- Guard: after the save folder is deleted or replaced mid-transfer, no byte and no directory appears at the old path, the download is disallowed, the marks are not wiped, and the session record is not clean. A write in flight during `Close` finishes or is refused, never lost silently; a close mid-transfer test is repeated.
- Facts recorded: an existing file at a torrent path is truncated or overwritten; a symlinked `<saveRoot>/<name>` is followed (owners 020/023); `promotePartFile` logs a flush warning per completed file with part files off.
- Default-engine facts (`storage_test.go`): `NewFile`, `NewMMap` and `NewFileOpts` with an unwritable directory end with a non-persistent store (completion does not survive a restart).

**C2 — restart, crash, mismatches.** Peers connect only after the add. "Nothing hashed" is proved by the guard's read counter, with no peer connected: zero reads for every trusted piece. The classic-IO child processes use the in-repo pattern of `library/process_test.go`.
- Clean full restart: close client, then store, fsync, snapshot; restart with `Trust` → complete at once, zero reads, zero payload bytes after connecting.
- Clean partial restart (K of 64 pieces of `v1-large`, a file never written): `Decide` gives `Trust`; K pieces complete with zero reads; session-2 payload ≤ (64−K)·16 KiB.
- Crash: a child leeches from the parent's rate-limited seeder and prints each completed piece; the parent SIGKILLs it after ≥ 20 pieces. No session record → `Verify` all; every piece the child reported is complete after verification; session-2 payload ≤ the missing pieces.
- Mismatch after a clean full download: truncate, rename, same-size rewrite, same-size rewrite with the mtime restored (ctime differs), and a same-size edit made during the session (fingerprint of the completed file differs). Recorded facts for a trusted add (same-size stays "complete" — a tracked finding). With the rule: the damaged files' pieces are incomplete after verification, the rest stay complete with zero reads, nothing downloads again that was valid. A rename gives `Missing`.
- File systems: the same-second rewrite on an HFS+ volume → `Verify`; an exFAT volume → `Verify` all (fact test); a replugged volume (new `st_dev`) → `Verify`, not `Missing`.
- Store unavailable: `OpenCompletion` on a missing, read-only or not-a-directory data dir → error, no file created; a corrupt store → `ErrCorrupt`, payload and the library database untouched.

**C3 — volumes (session 2).**
- Refused completion writes: engine level with a failing `SaveMarks` **test double** (named as such): the `Set` error is logged by the engine only, the piece stays complete in the session, `Err()` reports it, the next `Set` and `Close` return it, the session is not clean. Real level: a data dir on a volume filled *before* `OpenCompletion` → open error; the library test of B covers a full disk during a write.
- Detached volume (`detach -force`) while leeching and while seeding: the guard refuses; the mount-point folder stays empty (nothing is recreated on the internal disk); marks in the store are intact; the library database and the payload already on the volume are untouched; after re-attach, `Decide` → `Verify` for the unclean session, and no valid byte is downloaded again. What the stock engine would have done (recreate the folder) is recorded as the contrasting fact in C1.
- Left-over image check in `TestMain`.

**D — documents, `/branch-review`, gate, `done`.**

### Coverage

`go tool devtool check`: every new package is in the inventory and covered by unit tests, with no `.app` profile (the product does not link them). Package-variable seams only where a real cause is not producible (exec failure, hdiutil failure). `MainClassicIO` re-executes the test binary: the pre-exec counters are lost, but the same line runs again in the new process, and the exec branch is covered through the fake. Early in C1, confirm that `-coverprofile` and `-test.gocoverdir` survive the exec under the gate; if not, those packages run their tests in a child process and the report says so. No TypeScript change.

### Real vs doubles

Real: pinned engine, loopback swarm, APFS temp dirs, HFS+ and exFAT `hdiutil` volumes, SIGKILL child processes, modernc SQLite. Double (named): the failing `SaveMarks`; the exec and hdiutil seams. Not provable here: power loss (rule 2 covers it by design), `.app` restart and crash (`not_run`, 029/052), discovery on (018).

### Documents

- New: ADR `docs/decisions/engine-storage.md` (O5 accepted); report `docs/verification/009-storage-and-completion-spike.md` (this plan as the session plan, RED/GREEN, measurements, findings 15+); feature doc `docs/app/engine-storage.md`; index rows in `docs/README.md`.
- Edit: `docs/decisions/sqlite-driver.md` (the crawshaw journal correction and the marks table), `docs/decisions/engine-lifecycle.md` (classic-IO rule), `docs/app/library-store.md`, `docs/tooling/test-torrents.md`, `docs/development/testing.md`, architecture §3 and §6, register O5, task notes for 011, 018, 020, 023, 026, 028, 029, 030, 031/032, `CLAUDE.md` big picture, `README.md`, task 009 status and `go tool devtool plan-nav`.

### Out of scope

Linking the engine into the product and the Wails build tags (018); persisting the session record and fingerprints in the library schema (014/028/029); relink UI (030); magnet temporary storage (011); upload and pause (010); pre-add collision and symlink checks (020/023); uTP.

### Verification

`go test -race -count=1 ./internal/library ./internal/files ./internal/testutil/... ./internal/torrentengine/...` run twice; `go test -tags nosqlite,noboltdb ./internal/torrentengine/filestore`; `go tool devtool check` → `GATE PASSED` at each checkpoint; `/branch-review`. `not_run`: `.app` scenarios, engine integration with discovery on, power loss.

### Self-assessment: 7/10 (was 5/10 before the adversarial review)

**What the review fixed:**
- The detach and deleted-folder scenarios: the stock engine recreates the folder on the internal disk and wipes marks on read failure. A guard wrapper now prevents both and is tested.
- The rehash rule: files never written, replugged volumes, edits during a session, exFAT and the 1 s HFS+ timestamps. Distrust is per file.
- Completion store throughput and the client-lock freeze: one load per torrent, write-behind batches, sticky errors, `Persistent` decided by a fair measurement.
- Shutdown order: `Close` waits for in-flight calls.
- Add path: errors are no longer dropped.
- Three proofs that could not fail: the read counter replaces byte counts; the failing-store double replaces a timing-based full-volume test.
- The swarm changes, the volume package's coverage and boundaries, the 008 ADR correction, scope split in two checkpoints.

**Remaining weak points:**
- The guard hides the file storage's optional interfaces if they cannot be forwarded; C1's first step measures that, and the fallback is a documented performance cost.
- The re-exec under the gate's coverage run is untested until C1; a child-process fallback exists.
- The task is large for one session; the checkpoint after C2 allows stopping at a gate-passing state.
- A write-behind store loses up to 250 ms of marks on a crash; acceptable because a crash is verified anyway.
- Fingerprint persistence across a real app restart is designed here but built in 029; until then the rule is proven with an in-memory record.
- Power loss and the `.app` scenarios stay `not_run`.

## Stage log

| Stage | Commit | Result |
|---|---|---|
| A — measurements | `e7ca03e` | `pass` |
| B — library marks, volume package | `5631f10` | `pass` |
| C1 — placement and the guard | `c239f71` | `pass` |
| C2 — restart, crash, mismatches | `9bc9859` | `pass` |
| C3 — volumes | — | `not_run` |
| D — documents, review, `done` | — | `not_run` |

## Stage A — measurements

**Environment:** Apple M4, macOS 27.0.1, Go 1.27.2, anacrolix/torrent v1.61.0. Scratch tests in a
temporary package (`internal/torrentengine/scratch009`, deleted before the commit); the numbers below
are the evidence. Every command ran from the repository root.

| Rule | Evidence | Result |
|---|---|---|
| 1. Open failure is an error | Existing 008 tests of `library.Open`: `TestANewDatabaseInAReadOnlyFolderIsRefusedAndCreatesNothing`, `TestAFileThatIsNotADatabaseIsRefusedAndKept`, `TestADamagedPageIsReportedAsCorrupt`, `TestARelativePathIsRefused` | `pass` (the same store opens the marks file; the marks table is tested in stage B) |
| 2. Durable when `Close` returns; SIGKILL keeps whole commits | `TestEveryAcknowledgedCommitSurvivesAKilledProcess`, `TestCloseReportsACheckpointBlockedByAnotherReader` | `pass` (same) |
| 3. A second process gets an error | `TestAnotherProcessHoldingTheLockMakesAWriteBusy` | `pass` (same) |
| 4. A mark is read back after reopen | Stage B (`completion_test.go`) | `not_run` here |
| 5. `Persistent` | `TORRENT_STORAGE_DEFAULT_FILE_IO=classic go test -count=1 -v ./internal/torrentengine/scratch009`: 64 MiB, 256 KiB pieces, loopback, classic IO, part files off, map completion reporting the chosen value, `-race` off, two runs each | **`false`**: 0.228 s and 0.237 s. `true`: 1.214 s and 1.020 s, 4.3–5.3 times slower, above the 2× limit. |
| 6. Cost of the guard's `stat`; `SaveMarks` | the same run | `os.Stat` of a directory: 1.0 µs per call. A 256-row batch in one transaction: p50 5.0 ms, p99 10.8 ms. One row per transaction: p50 4.0 ms, p99 10.5 ms. A read of one mark in a read transaction: p50 10 µs, p99 17 µs. |
| 7. Build tags | `go list -test -deps` of a package using the file storage, with and without `-tags nosqlite,noboltdb[,disable_libutp]` | Default: cgo in `go-libutp`, `go-llsqlite/crawshaw/c`, `go-llsqlite/crawshaw`; `go-llsqlite/adapter`, `bbolt` linked. With `nosqlite,noboltdb`: crawshaw, adapter and bbolt are gone; `go-libutp` stays. With `disable_libutp` added: no cgo package of the engine is left. `go vet -tags nosqlite,noboltdb` passes. |

**Decisions from the numbers:**
- `Persistent()` is `false`. The engine then does not fsync the whole payload file on each completed
  piece, which costs more than 4 times the transfer time. Payload is synced once at a clean quit, and
  a crash is verified anyway (the rehash rule).
- A durable commit costs 4–11 ms whatever the batch size, so the completion store writes in batches
  from one goroutine; a `Get` is answered from memory.
- The guard may `stat` the save folder before every operation: 1 µs.
- Product tags: `nosqlite,noboltdb` always; `disable_libutp` is 018/026's transport decision.

## Stage B — library marks and the volume package

**REFACTOR first.** `go test -race -count=1 ./internal/library` was green (100%). The `hdiutil` helpers
of `internal/library/disk_test.go` moved to the new package `internal/testutil/volume`
(`New`, `Attach`, `Detach`, `Fill`, `Main`), which `disk_test.go` now uses.

**RED:**

| Command | Result |
|---|---|
| `go test -count=1 ./internal/testutil/volume` | `fail` as expected: `undefined: run`, `undefined: Main`, `undefined: New`, `undefined: Options`, `undefined: Fill` |
| `go vet ./internal/library` (with `completion_test.go`) | `fail` as expected: `undefined: CompletionStore` |

**GREEN:**

| Command | Result |
|---|---|
| `go test -race -count=1 -cover ./internal/testutil/volume` | `pass`, 100.0% of statements |
| `go test -race -count=1 -covermode=atomic -coverprofile=… ./internal/library`, no block with count 0 | `pass`, 100.0% of statements |
| `go test -race -count=2 ./internal/library ./internal/testutil/...` | `pass` both times |

**What was built.**
- `library.OpenCompletion(ctx, path)` opens the marks store with `library.Open`, so it has the 008 settings.
  `SaveMarks` writes a batch in one transaction; `LoadMarks` returns one torrent's marks;
  a piece with no mark is absent from the map. The table is
  `piece_completion(infohash, piece, complete)` `WITHOUT ROWID`, `user_version` 1.
- A database of a newer version, or with other tables, is `ErrUnsupportedVersion` and is never
  `ErrCorrupt`, so a rebuild of a damaged store cannot destroy a newer library. Tested, and the file
  is unchanged.
- The tests with real causes: a missing and a read-only folder (`ErrCannotOpen`), a garbage file
  (`ErrCorrupt`, kept), a full HFS+ volume (`ErrFull`, the failed batch of 20000 rows leaves none of its
  rows and earlier marks stay), a cancelled context, a table dropped and replaced by a second store
  on the same file (errors are reported, not hidden), and a schema setup on an ended transaction.

**Findings of the stage (not engine findings).**
- `Open` parses the schema, so a damaged schema page fails `Open` itself and the schema query of
  `OpenCompletion` cannot fail by corruption; its error branch is covered by a transaction that has
  ended (`sql.ErrTxDone`).
- On a volume filled to the last block, `Open` fails with `ErrCannotOpen` (no room for the WAL
  file); with 40 KiB free, `Open` works and the commit of the first schema fails with `ErrFull`.
  `volume.Fill` now fills in shrinking chunks down to one byte, so a test sets the free space
  explicitly.
- A store left open on a volume that is then force-detached makes the next test, on a new volume
  with the same device and inode numbers, fail with `database is locked`: SQLite keeps its lock
  table by (device, inode) per process. Tests must close stores before a volume goes away.

## Stage C1 — placement and the guard

**RED** (each command from the repository root, before the code existed):

| Command | Result |
|---|---|
| `go vet ./internal/torrentengine/classicio` | `fail` as expected: `undefined: Process` |
| `go vet ./internal/torrentengine/testutil/swarm` (tests of `MainClassicIO`, `Options.DefaultStorage`/`DataDir`) | `fail` as expected: `undefined: storage`, then `undefined: mainClassicIO` |
| `go vet ./internal/torrentengine/filestore` (session, completion tests) | `fail` as expected: `undefined: Options` |
| `go vet ./internal/torrentengine/filestore` (guard, `Layout`, `Refusing` tests) | `fail` as expected: `undefined: TorrentStorage` |

**GREEN:**

| Command | Result |
|---|---|
| `go test -race -count=1 -cover ./internal/torrentengine/classicio` | `pass`, 100.0% |
| `go test -race -count=1 -cover ./internal/torrentengine/testutil/swarm` | `pass`; the only uncovered block is `MainClassicIO`, which the `filestore` `TestMain` runs (the gate merges profiles with `-coverpkg=./...`) |
| `go test -race -count=3 -covermode=atomic ./internal/torrentengine/filestore` (units) | `pass`, 100.0% of statements |
| `go test -race -count=2 ./internal/torrentengine/... ./internal/library ./internal/testutil/...` | `pass` both times |

The re-exec of `MainClassicIO` works under `-race` and `-coverprofile`: the new process keeps the test
flags, and the profile it writes covers `filestore`.

**What was built.**
- `classicio.Require(Process)`: the process restarts itself with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic`
  when it is not set, and refuses any other value. `swarm.MainClassicIO` uses it. `swarm.Options` has
  `DefaultStorage` and `DataDir`.
- `filestore.Completion` (the marks as `storage.PieceCompletion`): marks read once per torrent, answered
  from memory, written in batches of at most 256 rows every 250 ms from one goroutine, a failed write
  sticky, `Persistent()` false, `Distrust` for the rehash rule, `Close` that flushes and may be repeated.
- `filestore.TorrentStorage` (the guard): the engine's file storage with part files off, inside a guard that
  checks the save folder's device and inode before every operation, counts piece reads, records every
  failure in the `Session`, refuses new operations after `Stop` and waits for those in flight.
  `Refusing()` is the client's default storage. `Layout` is the engine's path rule, shared with the storage.

**Scenarios on the real engine** (classic IO, closed loopback swarm, `-race`):

| Scenario | Result |
|---|---|
| Placement for `v1-single`, `v1-tree` (with its empty file), `hybrid-single`, `hybrid-tree` | `pass`: the files below the save folder are exactly `Layout`'s list, with the fixture's bytes; no `.pad`, no `.part`, no database there; the data folder holds only the marks database; the peer's own folder and the working directory stay empty |
| A torrent added without storage of its own | `pass`: refused with `ErrNoSaveFolder`, nothing written |
| The save folder deleted, or replaced, mid-transfer | `pass`: no byte and no folder appears at the old path, the transfer stops, every mark survives in memory and in the store, the session's first problem is `ErrRootChanged` |
| The run closed mid-transfer, three times | `pass`: every piece marked complete in the store has its bytes on disk (re-hashed against the torrent); the only problems are `ErrStopped`/`ErrClosed` |
| The same deletion with the engine's own storage (contrast) | `pass`, i.e. the engine **does** recreate the folder and goes on writing (finding 18) |

**Engine findings (numbered after 14).**

15. **`AddTorrentOpt` drops the storage error of the info it is given; `SetInfoBytes` returns it.**
    `TestTheEngineReportsAStorageErrorOnlyThroughSetInfoBytes`. The adapter adds with
    `AddTorrentOpts{InfoHash, Storage}` and `SetInfoBytes`. The engine also opens a torrent's storage
    more than once per add.
16. **The engine follows a symlink at `<save folder>/<name>`.** The payload is written to the link's target
    (`TestAnEngineWritesThroughASymlinkAtTheTorrentPath`). 020/023 must refuse such a path before the add.
17. **With classic file IO, `WriteTo` of a piece that ends inside its file returns `io.ErrShortWrite`.**
    The engine's limit writer reports the end of the range that way; `finishHash` only logs it and the hash
    is right. The guard passes it through and does not count it as a storage problem
    (`TestClassicIOReportsAShortWriteForAPieceThatEndsInsideItsFile`).
18. **Classic file IO recreates a deleted save folder** and writes on
    (`TestWithoutTheGuardTheEngineRecreatesADeletedSaveFolder`). For a detached volume that folder is the
    mount point, on the application's own disk. The guard prevents it.
19. **The engine's own constructors forget every piece when their database cannot be opened.**
    `storage.NewFile`, `NewMMap` and `NewFileOpts` (part files off, no completion) log a line and use
    an in-memory completion; when the database does open, it is `.torrent.db` **in the user's folder**
    (`internal/torrentengine/storage_test.go`).

## Stage C2 — restart, crash, mismatches

**RED** (before the code existed):

| Command | Result |
|---|---|
| `go vet ./internal/files` (fingerprint tests) | `fail` as expected: `undefined: Stat` |
| `go vet ./internal/files` (rule tests) | `fail` as expected: `undefined: Record` |
| `go vet ./internal/torrentengine/filestore` (seal tests) | `fail` as expected: `s.Seal undefined` |

**GREEN:**

| Command | Result |
|---|---|
| `go test -race -count=1 -cover ./internal/files` | `pass`, 100.0% of statements |
| `go test -race -count=1 -covermode=atomic -coverprofile=… ./internal/torrentengine/filestore`, no block with count 0 | `pass`, 100.0% of statements, 35 s |
| `go test -race -count=6 ./internal/torrentengine/filestore` | `pass` (206 s) |

**What was built.**
- `internal/files`: `Stat` (size, mtime, ctime, inode, device, regular), `FileSystem` (`statfs` type
  name), the `Probe` seam with `System`, and `Decide(record, saveRoot, pieces, probe)`.
- `TorrentStorage` records each file as the run last left it (after every write, and after the verification
  of a piece), `Baseline` carries over the files an earlier run left and the rule trusted, and `Seal`
  syncs the written files and returns the `files.Record`. `Completion.Close` fails the session when
  the marks store cannot close.

**Rule as built** (changed from the plan after the stage's own review; the reasons are in the ADR):
- Marks are trusted only for **APFS**. Any other file system is verified in full. HFS+ keeps whole
  seconds, so a fingerprint taken right after a write proves nothing; exFAT makes up inodes.
- A file whose fingerprint differs is verified; a file that was there and is gone is `Missing`; a replugged
  volume (another device) is verified in full, never `Missing`.
- A fingerprint is the file **as the run left it**, so an outside edit made while the application runs
  shows at the next start (`TestAnEditMadeWhileTheApplicationRunsIsVerifiedAtTheNextStart`).

**Scenarios on the real engine** (classic IO, `-race`):

| Scenario | Result |
|---|---|
| Clean restart after a full download (`v1-tree`) | `pass`: decision is trust; all pieces complete at once; **0 pieces read, 0 payload bytes** after the seeder connects; the record is carried over unchanged |
| Clean restart after a partial download (K of 64 pieces, stopped mid-way) | `pass`: exactly the K pieces are complete, 0 reads; the second run downloads at most the missing bytes; the file ends equal to the fixture |
| Process killed with SIGKILL (child process, ≥ 20 pieces reported) | `pass`: no record, so everything is verified (all 64 pieces read); every piece the child had reported is complete; no piece with wrong bytes is complete; nothing valid is downloaded again |
| A file truncated by a byte / rewritten with the same size / rewritten with the same size and the mtime set back / replaced by an identical copy | `pass` for each: only the file's pieces (piece 0) are verified, 1 piece read, the other four files are trusted, complete pieces equal the pieces whose bytes hash right, and the download repairs the rest |
| A file moved away | `pass`: `Missing` is that file; nothing is added |
| Trusted marks over a same-size rewrite (the contrast) | `pass`, i.e. the engine believes the mark: all pieces complete while piece 0 has wrong bytes (finding 20) |
| An outside edit while the application runs | `pass`: the record keeps the earlier fingerprint, so the next start verifies that file's piece only |
| Save folder on FAT, exFAT and APFS volumes | `pass`: FAT and exFAT are verified in full (all pieces read); APFS is trusted (0 reads) |
| APFS volume pulled and plugged again while another volume took its device number | `pass`: the device differs, the decision is "another volume", verify everything, nothing missing |

**Engine findings (numbered after 19).**

20. **The engine believes a stored mark over a file of the right size and never hashes it.** A same-size
    rewrite stays "complete" (`TestTrustedMarksAreBlindToASameSizeRewrite`). A truncated or missing file is
    re-downloaded without hashing. This is why a mark is never trusted on its own.
21. **On HFS+ the engine cannot hash any piece with classic file IO.** It seeks with `SEEK_DATA`, which
    HFS+ lacks (`seeking to start of extent: inappropriate ioctl for device`). Every piece fails its check and
    the download never completes (`TestOnHFSPlusTheEngineCannotHashAnyPiece`). FAT, exFAT and APFS work.
    `NewTorrentStorage` refuses an HFS+ save folder with `ErrFileSystem`. 020 must show this to the owner
    before an import is confirmed.
22. **A fresh transfer sometimes stands still for good.** One request sits unserved in the seeder's queue
    (the pattern of finding 14, with no upload switch used). Measured on the closed loopback swarm, 300
    fresh transfers of `v1-tree-no-empty` per row, 4 s limit each:

    | | with `-race` | without `-race` |
    |---|---|---|
    | classic file IO | 13 stalled (4.3%) | 0 |
    | mmap file IO | 5 stalled (1.7%) | 0 |

    The stock storage and our storage stall alike (2 and 3 of 80 under `-race`), so it is not the storage. The
    test helper `keepMoving` switches the seeder's upload off and on 50 ms apart when the seeder uploaded
    nothing for 1.5 s, and logs the count; the package passed six runs in a row under `-race`. The
    product's remedy is 010's.
