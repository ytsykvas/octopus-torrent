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
| A — driver probes, the choice | `342b63d` | `pass`: modernc.org/sqlite v1.60.1 |
| B — `internal/library` | `342b63d` | `pass`, 100% of statements |
| C — the version from the built product | stage C+D commit | `pass`; `.app` QA 5/5 |
| D — review fixes, documents | stage C+D commit | `pass`, `GATE PASSED` (below) |

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
| **Full volume** | pass | 8 MiB HFS+ image from `hdiutil create -fs HFS+ -layout NONE`, attached `-nobrowse`: `ErrFull: database or disk is full (13)`; `Close` also `ErrFull` (asserted; 5 of 5 runs with `-count=5`); after the filler is removed, 20 rows, integrity `ok`, same inode |
| `max_page_count` limit | pass | `ErrFull`; 1 row kept |
| Not a database | pass | `ErrCorrupt`; bytes unchanged; no `-wal`/`-shm` created |
| Damaged page | pass | `ErrCorrupt` on reading the table |
| Path with a space, `?`, `#`, `%` | pass | exactly that file is created |
| Relative path; `fn`'s own error; unclassified error; Close twice; use after Close | pass | as named |
| Close blocked by another reader | pass | `ErrBusy` from `wal_checkpoint(TRUNCATE)`; both rows present after a reopen |

`TestMain` refuses to run, and fails afterwards, while an `octopus-library-full-*` disk image is
attached. No image was left after any run (`hdiutil info`).

## Stage C — the SQLite version from the built product

**RED:**
- `go vet . ./internal/app`: `too many arguments in call to New`. The tests expected
  `app.New(ledger, "3.53.4")`, `RuntimeInfo.SQLiteVersion`, and `run(start, stderr, ledger, version)`,
  which refuses to start when the version cannot be read.
- `npm --prefix frontend test -- src/bridge/runtimeInfo.test.ts src/features/runtime-check src/App.test.tsx`:
  4 failed, because the DTO had no `sqliteVersion` and the text had no `· SQLite 3.53.4`.

**GREEN:**
- `RuntimeInfo` gains `sqliteVersion` (`json:"sqliteVersion"`).
- `run` reads `library.SQLiteVersion` once, before `start`, from `:memory:`, and passes it to
  `app.New`. If it fails, the reason goes to stderr and `run` returns 1.
- `main()` stays branch-free.
- Bindings were regenerated with `go tool wails generate module`; only `models.ts` changed.
- `getRuntimeInfo` and `RuntimeCheck` carry the field and show it.
- Results: `go test -race` of `.` and `./internal/app` `ok`; the 8 TS tests pass; `typecheck` and
  `lint` clean.

**Deviations from the plan:**
- `runtimeInfo.ts` has no decoder to make stricter; it passes the field through like the other
  three.
- The planned test that "the driver appears in the product's dependency list" was not written.
  `main.go` imports `internal/library` directly, and `go version -m` below shows the module in the
  binary.

### Build evidence

Working directory: the repository root.

```sh
npm --prefix frontend run build && go tool wails build -clean -trimpath
otool -L build/bin/OctopusTorrent.app/Contents/MacOS/OctopusTorrent
go version -m build/bin/OctopusTorrent.app/Contents/MacOS/OctopusTorrent
```

- **`otool -L`:** only `/usr/lib/libresolv.9.dylib`, `/usr/lib/libobjc.A.dylib`,
  `/usr/lib/libSystem.B.dylib`, and `/System/Library/Frameworks/` UniformTypeIdentifiers (weak),
  Foundation, Cocoa, WebKit, UserNotifications (weak), AppKit, CoreFoundation, Security and
  CoreGraphics. Nothing under `/opt/homebrew` or `/usr/local`.
- **`go version -m`:** `dep modernc.org/sqlite v1.60.1`, `-tags=desktop,wv2runtime.download,production`,
  `CGO_ENABLED=1`.
- **Binary size:** 12 567 952 B.

### `.app` QA (`macos-qa`, the real `OctopusTorrent.app`; Accessibility only, no screenshots)

| # | Scenario | Result | Evidence |
|---|---|---|---|
| 1 | Before launch: no `~/Library/Application Support/dev.tsykvas.octopustorrent` | pass | `test -e` → absent; the folder listing (126 entries) saved as the baseline |
| 2 | Click "Check Go connection" and read the output | pass | `Go go1.27.2 · darwin/arm64 · SQLite 3.53.4`, checked byte by byte (`od -c`): the **`sqlite_version()` of the built program** is 3.53.4, equal to the probe's |
| 3 | No database file open while running | pass | `lsof -p <pid>`: nothing under Application Support, no `.sqlite`. WebKit's own `WebsiteData/…/EnhancedSecuritySites.db` is the system WebView's store. No network socket. |
| 4 | Quit with the Apple Event | pass | process gone in under 1 s; the Application Support listing is identical to the baseline: no folder or file created |
| 5 | `otool -L` | pass | only `/usr/lib` and `/System` paths (above) |

## Review (`/branch-review`) and what changed

Three reviewers read `git diff 552e486` (code-reviewer, security-reviewer, tech-lead). There were
no critical findings. Each finding was fixed or answered as follows.

| Finding | Lens | Resolution |
|---|---|---|
| **M1:** a new reader connection (the driver opens `READWRITE|CREATE`) recreates a 0-byte `library.sqlite` if the file is deleted while open; the writer still commits into the deleted inode and returns nil; a reader could `PRAGMA query_only=0` and write | security | Readers open with `mode=ro`. `Write`/`Read` first compare the path's device and inode with the file `Open` opened, and return the new kind `ErrReplaced`. Tests: deleted file, replaced file, reader trying to write (RED: all three `nil`). |
| **M2:** `Open` over a 0-byte main file silently discards a WAL holding commits (SQLite deletes it) | security | `Open` refuses an empty or missing main file beside a non-empty WAL with `ErrCorrupt` and touches nothing. Test with both variants (RED: `nil`). |
| L1: a NUL byte truncates the path; `.`/`..` accepted | security | `Open` requires `filepath.Clean(path) == path` and no NUL. Test with four paths (RED: all opened). |
| L2: the database is created `0644`; the override folder is not specified | security | `Open` creates the file itself, `0600` with `O_EXCL`; SQLite copies the mode to `-wal`/`-shm`. Test (RED: `-rw-r--r--`). The ADR now specifies the `OCTOPUS_DATA_DIR` validation, for 014/028. A dangling symlink is refused and its target is not created (test). |
| L3: a valid SQLite file of another application would be switched to WAL | security | Handed to 014 (`application_id`) and 016 (`_defensive`, `trusted_schema=OFF` for files the owner picks). |
| L4: the gate's isolation of the owner's library is a note, not a test | security | The 014 note now asks for a failing test as soon as the default path exists. |
| L5: the child-process marker accepted any path; tests' raw `sql.Open` skipped escaping; `hdiutil` came from `PATH` | security | The child refuses a path outside `os.TempDir()` (checked with `OCTOPUS_LIBRARY_COMMIT_CHILD=/etc/passwd`: the test fails before opening it); tests use the escaped DSN; `/usr/bin/hdiutil`. |
| Writes were described as ordered; database/sql hands a freed connection to a random waiter | tech-lead | Corrected everywhere to "serialized, not FIFO". Per-torrent order (STACK-03) belongs to the service's queue (015 note, architecture §6, ADR, `Write` doc). |
| "On any error nothing is kept" overclaims: an I/O error from `COMMIT` has an unknown outcome | tech-lead | Reworded in code and docs; `SQLITE_IOERR` stays unclassified, and 015 maps it and re-reads. |
| The read-only and full-disk checks' "the application shows the reason" | tech-lead | `not_run` in 008: the app opens no file database. Handed to 014/028 explicitly. |
| modernc's rollback after a failed commit keeps the single writer usable | tech-lead | Added to the ADR's "recheck on a driver update"; the failed-commit test guards it. |
| 028: one `Store` per process; quit order; `Close` has no context; probe a read-only WAL file at start | tech-lead, code-reviewer | Added to the 028 note. |
| Package-variable seams (`busyTimeout`, `walPragma`) | code-reviewer | Replaced by an unexported `settings` passed to `open`; `Open` uses `defaults`. |
| `kinds` was a production variable for the tests | code-reviewer | Moved to `errors_test.go`. |
| `Close` on a full disk and on a read-only file was only logged | code-reviewer | Asserted: `ErrFull`, and `nil` respectively. |
| `TestMain` ran in the child processes, and failed a concurrent run of the package | code-reviewer | Only the parent checks. An image whose name carries a live pid is another run's; only an image of a dead pid is a leftover, and the run's own image at exit fails it. |
| One test file with many roles | code-reviewer | Split into `helpers_test.go`, `errors_test.go`, `version_test.go`, `safety_test.go`, plus the store, process and disk tests. |
| `fn`'s error "unchanged" | code-reviewer | Docs say exactly what happens: an error of `fn`'s own comes back unchanged; one from SQLite or the context comes back with its kind. If both cancel and commit fail, cancellation is the kind (commented). |
| `Connect` and `Close` take no context; `Tx` lifetime | code-reviewer | Documented: both are bounded by `busy_timeout` (5 s); a `Tx` is valid only inside `fn`. |
| Wails flips runtime file modes | code-reviewer | Restored to `644`; no mode change is committed. |

After the fixes:
- `go test -race -count=1 ./internal/library`: `ok`, 9.7 s, **100.0% of statements**, no zero block.
- `-run FullDisk -count=5`: `ok`.
- `hdiutil info` shows no image left.

## Verified, not yet proven, read in the source only

**Verified by execution** (stages A–C and the gate):
- the driver links next to the engine's C SQLite, and mattn does not;
- the bundled SQLite is 3.53.4, in a probe, in the tests and in the built `.app`;
- the `.app` needs only system libraries;
- per-connection PRAGMAs;
- a failed commit is an error;
- cancellation;
- busy across processes;
- readers during writes;
- checkpoints during writes;
- SIGKILL keeps acknowledged commits;
- read-only folders and files;
- a real full volume;
- damaged files;
- a deleted or replaced file;
- an empty file beside a WAL;
- private file modes;
- a hostile-character path.

**Not yet proven:**
- **Power-loss durability.** Not executable here.
- **The reason shown to the user** for a read-only folder or a full disk. `not_run`: the app opens no
  file database until 014/028.
- **Two application instances on one library.** The single-instance lock belongs to 028.
- **The backup procedure.** 016.
- **The `OCTOPUS_DATA_DIR` override and its validation.** 014/028.

**Read in the source only:**
- `F_FULLFSYNC` in modernc's `_full_fsync`. The 40× commit cost supports it.
- modernc's rollback after a failed `COMMIT` (`tx.go`). Guarded by a test of its effect.
- database/sql's random hand-over of a freed connection (`connRequestSet.TakeRandom`).
- The engine's `.torrent.db` uses one connection in the default rollback journal
  (`sqlite-piece-completion.go`).

## Acceptance criteria

| Criterion | Evidence | Status |
|---|---|---|
| A failed transaction commit returns an error, not success | `TestAFailedCommitIsAnErrorAndKeepsNothing` (deferred foreign key at `COMMIT` → `ErrConstraint`, 0 rows after a reopen, the writer usable); `ErrReplaced` stops a commit into a deleted file | pass |
| The built program launches without Homebrew libraries | `otool -L` of `OctopusTorrent.app`; `.app` QA scenario 2 | pass |
| The WAL decision rests on the verified version, concurrent writes and checkpointing | 3.53.4 ≥ 3.51.3 (test and `.app`); readers-vs-writer and writes-during-checkpoints tests; writer-only checkpoint | pass |
| Checks performed; not-run ones not marked passed | this report: the application-level reason is `not_run` with a handoff, and power loss is not executed | pass |
| Check: concurrent writes, checkpoints, cancellation, timeouts | stage B table | pass (package level, real SQLite) |
| Check: a read-only directory and a full disk; the existing database kept | stage B table, file bytes and inode compared | pass at package level; the application showing the reason is `not_run` (014/028) |

## Refactoring and debt check

- **Refactoring done during the task:**
  - `library.Tx` instead of `*sql.Tx` (the hang in RED);
  - `settings` instead of package-variable seams;
  - the test files split by role;
  - `TestMain` made safe for child processes and concurrent runs;
  - the reader pool made read-only at the SQLite level;
  - one `check` for file identity, used by `Write` and `Read`.
- **Debt check:** no ignore directive, lint disable, skipped test or TODO
  (`grep -rn "TODO\|nolint\|t.Skip" internal/library` is empty).
- **Deferred items, each a named, owned handoff and not debt:**
  - classifying `SQLITE_IOERR` (015);
  - the data directory (014/028);
  - `application_id` (014);
  - backup (016).

## Final gate

`go tool devtool check` (working directory: the repository root), run on the final tree of this
change: **`GATE PASSED`**, exit 0.

| Step | Result |
|---|---|
| npm ci, npm cooldown, go env, typecheck, lint | pass |
| frontend tests | pass: 22 own files at 100% on lines, statements, functions and branches |
| frontend build, gofmt, go vet | pass |
| bindings | pass: OctopusTorrent `frontend/wailsjs/` (6 files), EngineSpike `frontend/spike/wailsjs/` (6 files) |
| go tests | pass: `go test -race -count=1 -covermode=atomic -coverpkg=./... ./...` |
| `.app` coverage run | pass: both apps were launched and quit (OctopusTorrent pid 7541, EngineSpike pid 7683) |
| go coverage | pass: every block of every own Go file is covered, `internal/library` included, by the unit run and the `.app` run |
| escape hatches, scripts | pass |
| product isolation | pass: 314 packages; the product links the driver but none of the spike, test support or engine |
| release build, docs (151 Markdown files), plan | pass |
| release age | pass: 186 npm versions and 359 Go modules are at least 14 days old; `golang.org/x/net@v0.60.0` is accepted as younger (owner's exception, until 2026-10-22) |
| npm audit, govulncheck | pass |
| updates | pass, informational: `anacrolix/chansync` v0.7.0 → v0.8.0 is out of quarantine; not taken (the engine requires v0.7.0) |
| engine integration, test-bridge UI tests | `not_run` (not implemented yet: 018, 035) |
| single code version | pass |

**The first full run, before the review fixes, failed only on `docs`.** The cause was 9 dead links in
the untracked `.agents/skills/source-command-*`: skills that Codex had generated locally from
`.claude/commands`, whose relative links point nowhere from there. On the owner's decision,
`/.agents/` is now in `.gitignore`.
