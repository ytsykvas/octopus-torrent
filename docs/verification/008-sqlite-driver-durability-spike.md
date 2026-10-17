# Report 008: the SQLite driver and the durability mode

**Task:** [008](../../product/tasks/008-sqlite-driver-durability-spike.md)  
**Date:** 17 October 2026  
**Change version:** branch `task/008-sqlite-driver` (commits are listed per stage)

## Session architecture plan

The owner approved this plan on 17 October 2026, after self-review passes (7 → 8 → 9 of 10).

- **Goal:**
  - choose the SQLite driver by measurement on this Mac;
  - fix the durability mode, the pool and the write order;
  - prove committed-means-kept, cancellation, timeouts, a read-only directory and a full disk on
    real SQLite;
  - show `sqlite_version()` from the built product.
- **Facts before coding:**
  - Wails on macOS needs cgo, so the pinned engine compiles its own C SQLite: `go-llsqlite/crawshaw`,
    SQLite 3.49.1, for its default piece completion.
  - The WAL-reset bug affects SQLite 3.7.0–3.51.2 and is fixed in 3.51.3
    ([wal.html §11](https://www.sqlite.org/wal.html)).
- **Choice rules, ordered and declared before measuring.** Rules 1–4 are hard.
  1. Links into one cgo binary with `anacrolix/torrent/storage`, and runs both.
  2. `otool -L` lists only system libraries.
  3. Bundled `sqlite_version()` ≥ 3.51.3.
  4. Smoke test passes:
     - WAL and checkpoints during writes;
     - context cancel;
     - `_txlock=immediate`;
     - per-connection PRAGMAs;
     - `VACUUM INTO`;
     - `F_FULLFSYNC` really issued.
  5. A stable v1 API wins over v0.
  6. Fewer modules win.

  Stop rule: if no candidate passes 1–4, or the results contradict the expectation in a way the
  rules do not settle, the owner decides.
- **Boundaries:**
  - the new package `internal/library`, a prototype that 014/015 build on;
  - the product's runtime info gains the SQLite version: `internal/app`, `main.go`, bindings,
    `frontend/src/bridge/runtimeInfo.ts`, `RuntimeCheck`.
  - The product opens only `:memory:`, so it gets no data directory in 008.
- **Stages:**
  - A — driver probes and the choice;
  - B — `internal/library`;
  - C — the version from the built product;
  - D — documents, review, `done`.
- **Coverage:** `go tool devtool check`, with no inventory or threshold change.

Decisions: ADR [sqlite-driver](../decisions/sqlite-driver.md).

## Stage log

| Stage | Commit | Result |
|---|---|---|
| A — driver probes, the choice | stage A+B commit | `pass`: modernc.org/sqlite v1.60.1 |
| B — `internal/library` | stage A+B commit | `pass`, 100% of statements |

## Stage A — driver probes and the choice

**Environment:**
- Apple M4, macOS 27.0.1 arm64, Go 1.27.2, `CGO_ENABLED=1`;
- every probe and timing ran in a fresh `os.MkdirTemp` folder, removed afterwards.

**Probes:**
- One throwaway module per candidate, in the session scratchpad and not committed.
- Each requires `github.com/anacrolix/torrent v1.61.0` and the candidate, at the newest tag that
  is at least 14 days old (cutoff 2026-10-03).

**What each probe's `main` does:**
1. calls `storage.NewDefaultPieceCompletionForDir`, so the engine's C SQLite is linked and used;
2. opens the candidate through `database/sql` with
   `_pragma=busy_timeout(5000)&_pragma=synchronous(FULL)&_pragma=fullfsync(1)&_pragma=foreign_keys(1)&_txlock=immediate`
   (mattn uses its own key names);
3. prints `sqlite_version()`;
4. sets WAL;
5. reads the four PRAGMAs back on three pooled connections held at once;
6. commits 200 single-row transactions while a second `*sql.DB` loops on
   `wal_checkpoint(TRUNCATE)`;
7. reopens and checks the count and `integrity_check`;
8. cancels a recursive CTE with a 50 ms deadline, then uses the connection again;
9. runs `VACUUM INTO` and checks the copy's count and integrity;
10. commits one `BEGIN IMMEDIATE` transaction.

**Commands** (working directory: each probe module):

```sh
CGO_ENABLED=1 go build -trimpath -o probe .
./probe
otool -L probe
```

| Rule | mattn/go-sqlite3 v1.14.52 (2026-09-05) | modernc.org/sqlite v1.60.1 (2026-09-29) | ncruces/go-sqlite3 v0.35.6 (2026-09-23) |
|---|---|---|---|
| 1. links with the engine | **fail**: build exit 1, `ld: 266 duplicate symbols` (e.g. `_sqlite3_serialize`, `_sqlite3_mutex_try`): two C amalgamations | pass: engine piece completion `*storage.sqlitePieceCompletion` and the driver in one binary | pass: same |
| 2. system libraries only | not built | pass: `libresolv.9`, CoreFoundation, Security, `libSystem.B` | pass: same four |
| 3. `sqlite_version()` | not built | **3.53.4** | **3.53.4** |
| 4. WAL + checkpoints | — | pass: count 200, integrity ok; 1 concurrent checkpoint completed | pass: count 200, integrity ok; 210 checkpoints completed |
| 4. per-connection PRAGMAs | — | pass: `synchronous=2 fullfsync=1 busy_timeout=5000 foreign_keys=1` on all 3 | pass: same |
| 4. cancel | — | pass: returned after 51 ms, `context deadline exceeded`; connection usable | pass: 50 ms, `sqlite3: interrupted`; usable |
| 4. `VACUUM INTO` | — | pass: count 200, integrity ok | pass: same |
| 4. `F_FULLFSYNC` (source) | — | pass: `_full_fsync` calls `fcntl(fd, F_FULLFSYNC)` (`lib/sqlite_g_0000000000000003.go:30955`, compiled for darwin/arm64) | pass: `vfs/os_darwin.go` `osSync` uses `F_FULLFSYNC`, and `F_BARRIERFSYNC` for rollback journals |
| 4. `F_FULLFSYNC` (timing) | — | 300 WAL commits, `synchronous=FULL`: 0.09–0.12 ms/commit without, 3.85–4.50 ms with `fullfsync` | 0.05–0.07 ms without, 3.70–3.90 ms with |
| Binary size (probe, with the engine) | — | 18 253 682 B | 21 256 498 B |
| 5. API stability | — | v1 | **v0** |
| New linked modules (beyond the engine's) | — | `modernc.org/{sqlite,libc,memory,mathutil}`, `ncruces/go-strftime`; bumps `dustin/go-humanize`, `mattn/go-isatty` | `ncruces/{go-sqlite3,go-sqlite3-wasm/v6,julianday}` |

**What the timing shows.** Turning `fullfsync` on makes each commit about 40 times slower. That
is the cost of a real `F_FULLFSYNC`, which reaches the drive and waits for its cache. Both drivers
therefore issue it, not just accept the PRAGMA.

**The choice: `modernc.org/sqlite` v1.60.1.**
- mattn fails rule 1.
- modernc and ncruces both pass rules 2–4.
- Rule 5 is the first to separate them: modernc's v1 against ncruces' v0. The result matches the
  expectation declared before measuring, so the stop rule does not apply.
- ncruces would have won rule 6, with fewer new modules, but rule 5 comes first.
- The concurrent-checkpoint count differs: 1 for modernc, 210 for ncruces. A checkpoint that cannot
  run immediately returns busy, and the probe did not count those. Integrity and counts were
  correct in both. The design has only the writer checkpoint, so this is recorded and not used in
  the choice.

**Not considered further:**
- mattn with `-tags libsqlite3`, which links the system `libsqlite3`. The SQLite version would then
  be Apple's, not pinned by `go.mod`, so the build would no longer be reproducible (AI-07).
- `zombiezen.com/go/sqlite`: built on modernc, with no `database/sql` API.
- `crawshaw.io/sqlite`: last released 2020.

## Stage B — `internal/library`

**Module change:**
- Command: `go get modernc.org/sqlite@v1.60.1`, then `go mod tidy`.
- Indirect modules moved: `dustin/go-humanize` v1.0.0→v1.0.1, `mattn/go-isatty` v0.0.20→v0.0.24,
  `modernc.org/libc` v1.22.3→v1.77.1, `mathutil` v1.5.0→v1.7.1, `memory` v1.5.0→v1.12.1.
- New: `ncruces/go-strftime` v1.0.0 (2025-10-08).
- The planned `google/pprof` and `modernc.org/fileutil` did not enter the build list: graph
  pruning.
- After the bump, the engine packages are green: `go test -race -count=1 ./...` passed every package
  in 33 s.

**RED** (`go test -race -count=1 ./internal/library/`, working directory: the repository root):
- First run: build failed, `undefined: Store`. The tests existed before the package did.
- **Second RED, after the first implementation: the package hung for 600 s.**
  - Cause: the cancelled-read test ran its query through `tx.QueryRow`, which in database/sql uses
    `context.Background()`. The driver interrupts a statement only through the statement's own
    context, so the infinite recursive CTE never ended, and the transaction's watcher waited on it
    forever.
  - Fix: `fn` receives `library.Tx`, whose `Exec`, `Query` and `QueryRow` always use the operation's
    context. A forgotten context can no longer make a cancel ineffective.
- **Third RED: 2 failures, where the measured behaviour differed from the prediction.**
  - An existing WAL database in a read-only folder: predicted `ErrCannotOpen`, measured
    `ErrReadOnly`, `attempt to write a readonly database (1544)`, which is
    `SQLITE_READONLY_DIRECTORY`.
  - A read-only file in a writable folder: predicted that `Open` fails, measured that it succeeds,
    because the file is already in WAL mode and nothing has to be written.

  As the plan required, each test now asserts the measured result, and the ADR records it. A
  separate test covers a read-only file that is not yet WAL: `Open` gives `ErrReadOnly`.
- **Coverage RED:** 96.6%. `Tx.Query` was not called, and the guard for a journal mode other than
  WAL was not reached; SQLite reports a read-only file as an error before it.
  - A test now reads rows through `Query`.
  - The guard is reached through the `walPragma` seam, which reads the mode instead of setting it.

**GREEN:**
- `go test -race -count=1 ./internal/library/`: `ok`, 8.9 s and then 7.9 s.
- `-covermode=atomic -coverprofile`: **100.0% of statements**, with no zero block.

| Test | Result | Measured |
|---|---|---|
| Every connection has the durability settings | pass | writer + 4 readers: `wal`, `synchronous=2`, `fullfsync=1`, `busy_timeout=5000`, `foreign_keys=1`, `query_only` 0/1 |
| Bundled SQLite has the WAL-reset fix | pass | `SQLite 3.53.4` ≥ 3.51.3 |
| A failed commit is an error and keeps nothing | pass | deferred foreign key: `ErrConstraint`; the writer is clean; 0 rows after a reopen |
| Cancelled read / write / context | pass | read returns in < 1 s; write keeps 0 rows; a cancelled context never calls `fn` |
| Locked database, in-process | pass | `ErrBusy` after ≥ 200 ms (lowered timeout) |
| Locked database, **another process** | pass | the child holds `BEGIN IMMEDIATE`: `ErrBusy`; after it commits, both rows are present |
| Readers vs the writer | pass | 300 two-row commits in 1.9 s (6.4 ms each under `-race`); 4 readers made about 6 300 reads each; never busy, never an odd count |
| Writes during checkpoints | pass | 300 commits while another connection ran 41 724 `wal_checkpoint(PASSIVE)`; after `Close` the WAL is empty; 300 rows and integrity `ok` after a reopen |
| **Acknowledged commits survive SIGKILL** | pass | the child printed `committed N` after each `Write`; killed after 40; all 40 present, integrity `ok` |
| New database, read-only folder | pass | `ErrCannotOpen`; the folder stays empty |
| Existing WAL database, read-only folder | pass | `ErrReadOnly` (`SQLITE_READONLY_DIRECTORY`); bytes and inode unchanged |
| Read-only WAL file | pass | opens and reads; the write gives `ErrReadOnly`; bytes and inode unchanged |
| Read-only non-WAL file | pass | `ErrReadOnly` from the WAL switch; unchanged |
| **Full volume** | pass | 8 MiB HFS+ image from `hdiutil create -fs HFS+ -layout NONE`, attached `-nobrowse`: `ErrFull: database or disk is full (13)`; `Close` also `ErrFull`; after the filler is removed, 20 rows, integrity `ok`, same inode |
| `max_page_count` limit | pass | `ErrFull`; 1 row kept |
| Not a database | pass | `ErrCorrupt`; bytes unchanged; no `-wal`/`-shm` created |
| Damaged page | pass | `ErrCorrupt` on reading the table |
| Path with a space, `?`, `#`, `%` | pass | exactly that file is created |
| Relative path; `fn`'s own error; unclassified error; Close twice; use after Close | pass | as named |
| Close blocked by another reader | pass | `ErrBusy` from `wal_checkpoint(TRUNCATE)`; both rows present after a reopen |

`TestMain` refuses to run, and fails afterwards, while an `octopus-library-full-*` disk image is
attached. No image was left after any run (`hdiutil info`).
