# Report 014: the SQLite schema, safe migrations and the data folder

**Task:** [014](../../product/tasks/014-sqlite-schema-migrations.md). **Requirements:** `STACK-04`, `SET-02`. **Register:** O4 (014's part). **Branch:** `task/014-sqlite-schema`.

## Session architecture plan

Approved by the owner on 2026-11-04, after an adversarial review by an architect agent (25 findings,
listed in the self-assessment at the end). The owner chose the whole data-folder scope for 014.

### Context

Task [014](../../product/tasks/014-sqlite-schema-migrations.md) (P2), requirements `STACK-04`, `SET-02`;
completes 014's part of register item **O4**. Dependencies 005 and 008 are `done`. Owner decision
(2026-11-04): 014 takes the **whole data-folder duty** — the default folder and the validated
`OCTOPUS_DATA_DIR` override, `workspace run` and the gate setting it, and the application opening
(and migrating) the library at start and closing it last at quit.

Today `internal/library` opens any file it is given and switches it to WAL before knowing whose it
is; there is no schema; the marks store (009) has an interim `user_version 1` check of its own; the
product opens only `:memory:`.

For the owner: the installed `.app` creates and opens
`~/Library/Application Support/dev.tsykvas.octopustorrent/library.sqlite` (an empty library at
schema 1) before its window appears, and closes it last at quit; nothing is shown in the window yet.
A library of a newer version, another application's SQLite file, an unusable data folder or a failed
migration stops the start with a reason on stderr (the user-visible form is 028) and never changes
the file. **Builds never open it** (binding generation skips the data folder), **`wails dev` refuses
to start without `OCTOPUS_DATA_DIR`**, and workspaces, tests, `/qa` and the gate always use their own
folders; the gate fails if the owner's folder changes during a run. The owner's own rule for manual
runs of branch builds (a branch with a new migration would upgrade the real library, and `main`'s
build would then refuse it as newer) is written in getting-started and the ADR: give such a run its
own `OCTOPUS_DATA_DIR`.

**Out of scope:** repositories, row mapping and `AppError` codes (015); backup before a migration and
corruption recovery (016); the user-visible startup error and the single-instance lock (028); the
"a save folder is not the data folder" check from the save-folder side (020, 029); tables for tags,
collections, rules and the watch folder (their tasks); a file-priority column (024, its own migration
if needed); logs (017). No new dependency, no `.npmrc` change, no binding change.

### Boundaries

- **Repository (`internal/library`):** every database is opened through a schema (an
  `application_id`, an explicit list of embedded SQL migrations, a structural check). Exported
  openers: `OpenLibrary(ctx, path)` (new; `Library` exposes only `Close` in 014, 015 adds the
  repositories) and `OpenCompletion(ctx, path)` (unchanged signature). The raw `Open` becomes
  unexported and there is no schema-less path, so no caller can convert or migrate a file outside the
  policy.
- **New `internal/datadir`** (standard library only): resolves and validates the data folder.
- **Startup (`main.go`):** the library opens after the SQLite version check and before the window,
  and closes after `wails.Run` returns. Verified in the pinned Wails v2.16.0 source: `App.Run`
  (`internal/app/app_production.go:17-25`) returns after `RunMainLoop` (`[NSApp stop]` on quit) and
  the shutdown callback; the bindings generator builds `-tags bindings` and runs `main()`
  (`pkg/commands/bindings/bindings.go:21-73`, used by every `wails build`, `generate module` and
  `wails dev`), so the build mode is decided by tags (below).
- **Tooling (`internal/devtool`), agents and docs:** `workspace run`, the gate, the `macos-qa` agent
  and the documented `wails dev` command all pass their own `OCTOPUS_DATA_DIR`.
- **No change:** facade methods, DTOs, bindings, TypeScript, the engine.

### Design

#### 1. The migration policy (`internal/library/schema.go`)

`schema{name; applicationID int32; migrations []string}`; each migration is its own embedded file
(`//go:embed migrations/<schema>/NNNN_<what>.sql` into its own variable), listed explicitly, so the
order is fixed by construction (a test checks the file names match the list). Migration `i` brings
the database to version `i+1`. Library: `application_id` 0x4F544C42 ("OTLB"); marks: 0x4F545043
("OTPC").

`inspect(tx)` is **one aggregated query** (one `QueryRow`, one error path): `application_id`,
`user_version`, the count of every `sqlite_schema` row, and a sorted structural description built
from pragmas, stable across SQLite versions: `pragma_table_list` (type, strict, without rowid),
`pragma_table_xinfo` (name, declared type, not null, default, pk, hidden), `pragma_foreign_key_list`
(table, columns, on delete), `pragma_index_list` + `pragma_index_xinfo` (unique, origin, partial,
columns), plus the SQL of triggers and views; only `schema = 'main'` and names not `sqlite\_%`
(`sqlite_stat1` after `ANALYZE` or `PRAGMA optimize` must not make a library unknown).
`reference(ctx, sch, v)` replays migrations 1..v on a private single-connection `:memory:` database
and inspects it. CHECK contents are not structural (SQLite exposes them only as SQL text); the
enumeration test guards them for the current schema.

`decide(state, sch)`:

| File | Result |
|---|---|
| id 0, version 0, no `sqlite_schema` rows | new: stamp the id, run every migration |
| our id, 1 ≤ version ≤ current, structure = reference(version) | run migrations version+1..current (none when current) |
| our id, version > current | `ErrUnsupportedVersion` (written by a newer build) |
| anything else: another id, an unstamped file with objects, our id with another structure or version 0 | `ErrUnknownSchema` |

`open(ctx, path, set, sch)`:
1. as today: path checks, the empty-file-beside-WAL refusal, the `0600`/`O_EXCL` create, the pools;
2. **before switching the journal mode:** `inspect` + `decide` in a **read-only** transaction on the
   writer (`TxOptions{ReadOnly: true}`: plain `BEGIN`, no write lock — modernc `tx.go:22-25`), so a
   file that is not ours is refused before anything converts it. Refusals return unwrapped;
3. switch to WAL (as today);
4. **one migration transaction on a pinned writer connection:** `PRAGMA foreign_keys=OFF` (outside
   the transaction, as SQLite requires for table rebuilds) → `BEGIN IMMEDIATE` → `inspect` + `decide`
   again (another process may have migrated meanwhile) → `PRAGMA application_id` → every pending
   migration → `PRAGMA user_version` → `PRAGMA foreign_key_check` must return no row → `COMMIT`.
   The connection is then discarded (`Raw` returns `driver.ErrBadConn`), so the next one opens with
   the DSN's `foreign_keys=1`; no restore branch exists. Any error rolls everything back: old
   version, old id, every row. `open` wraps a non-refusal error from this step, `COMMIT` included,
   as `ErrMigrationFailed` "from version k to n" joined with the raw error classified once
   (`ErrFull`, `ErrReadOnly`, `ErrBusy`, `ErrCanceled`, `ErrConstraint`, …).

`errors.go`: `ErrUnsupportedVersion` moves here (narrowed to "ours, newer"); new `ErrUnknownSchema`
and `ErrMigrationFailed`; none is `ErrCorrupt`. A refused file's rows are never changed; reading it
may still let SQLite roll back its own hot journal or checkpoint its own WAL (ADR states this).

#### 2. Library schema 1 (`migrations/library/0001_initial.sql`)

All tables `STRICT`. Unsigned 64-bit values (inode, device, speed limits) are stored as their int64
bit pattern; times are Unix nanoseconds UTC.

| Table | Columns and constraints |
|---|---|
| `torrents` | `id TEXT NOT NULL PRIMARY KEY` (36 chars, lower case; Go validates the UUID), `infohash_v1 BLOB UNIQUE` (20 bytes), `infohash_v2 BLOB UNIQUE` (32 bytes), CHECK at least one, `name TEXT NOT NULL`, `info BLOB NOT NULL` (the verified info dictionary, non-empty), `source` IN the import sources, `source_metainfo BLOB` (the imported `.torrent` bytes), `source_magnet TEXT`, `save_path TEXT NOT NULL` non-empty, `manual_paused` IN (0,1), `added_at INTEGER NOT NULL` |
| `torrent_files` (without rowid) | `torrent_id` → `torrents` ON DELETE CASCADE, `file_index ≥ 0`, `path TEXT NOT NULL` (the layout-relative path `files.FileState.Path` uses), `size ≥ 0`, `wanted` IN (0,1); PK (torrent_id, file_index), UNIQUE (torrent_id, path) |
| `payload_records` (without rowid) | `torrent_id` PK → `torrents` ON DELETE CASCADE, `clean` IN (0,1), `file_system TEXT`, `device INTEGER`; CHECK clean = 0 or both known. clean = 0 is 029's "not clean" marker (it may keep the earlier files); no row = never ran |
| `payload_files` (without rowid) | `torrent_id` → `payload_records` ON DELETE CASCADE; (`torrent_id`, `file_index`) → `torrent_files` ON DELETE CASCADE; `present`, `size`, `mtime`, `ctime`, `inode`, `device`, `regular` (`files.FileState`). The foreign key makes `files.Decide`'s rule 3 (a record listing a file the torrent lacks) impossible from the library — deliberate |
| `settings` | one row (`id INTEGER NOT NULL PRIMARY KEY CHECK (id = 1)`), `default_save_path` (null or non-empty), `theme` IN the themes, `language` IN the languages, `download_limit`, `upload_limit` (null or ≠ 0), `global_upload_disabled` IN (0,1). **No row until the first save**: SET-06 takes the initial language from macOS, so 050 writes the first row; no row = the defaults, upload allowed |
| `operations` | `operation_id TEXT NOT NULL PRIMARY KEY` (36, lower case), `runtime_id TEXT NOT NULL` (36, lower case: the run that accepted it), `kind` IN the operation kinds, `fingerprint` non-empty, `request` (`json_valid`), `state` IN the operation states, `error` (null or `json_valid`), `accepted_at`, `finished_at`; CHECK a final state ⇔ `finished_at` set; CHECK `error` set ⇔ state failed or cancelled (api-contract) |
| `operation_torrents` (without rowid) | `operation_id` → `operations` ON DELETE CASCADE, `torrent_id TEXT NOT NULL` (36 chars, **no foreign key**: the journal outlives the record, 031), `outcome` (null or `json_valid`); PK (operation_id, torrent_id); index on `torrent_id` |

A new value of an enumeration (e.g. an operation kind of 030 or 056–061) is a table rebuild, which
the runner supports without losing child rows (FKs off, `foreign_key_check` before commit).

#### 3. Marks store under the policy (`completion.go`)

`OpenCompletion` = `open(ctx, path, defaults, completionSchema)`; `migrations/completion/0001_piece_completion.sql`
is 009's table. `ensureCompletionSchema` is deleted (the generic `decide` is the restructuring the
009 checkpoint asked for). No shipped build ever opened a marks file (only tests and the spike's
temporary folders, `internal/enginespike/service.go:30`), so unstamped 009 files are not adopted:
they are `ErrUnknownSchema` and unchanged.

#### 4. The data folder (`internal/datadir`)

`Resolve(lookupEnv func(string) (string, bool), home func() (string, error))`, `FromEnvironment()`
(the process's), `FromOverride()` (the variable is required: the `dev` build), `Default(home) string`
(also used by devtool), `Override = "OCTOPUS_DATA_DIR"`, `ErrUnusable`.
1. Set: absolute, clean, no NUL; set but empty is refused; `home` is never called. Unset:
   `<home>/Library/Application Support/dev.tsykvas.octopustorrent` (`home` must be absolute).
2. `os.MkdirAll(dir, 0o700)`, then one check of the resolved path (`EvalSymlinks` + `Stat`, one
   refusal path): a directory, owned by the current user (`currentUID` seam), not writable by group
   or others. Returns the resolved path (`/var/…` → `/private/var/…`).
"Not inside a torrent save folder" cannot be checked here (the save folders are in the library this
folder holds): 020 refuses a save folder that is, contains or lies inside the data folder; 029
checks restored records.

#### 5. Build modes, startup and quit (`main.go`)

Three const/var-only files (no function, so no coverage inventory; not an escape hatch):
`buildmode_bindings.go` (`//go:build bindings`), `buildmode_dev.go` (`//go:build dev && !bindings`),
`buildmode_app.go` (`//go:build !dev && !bindings`) define `generatingBindings` and `resolveDataDir`
(`FromEnvironment`, or `FromOverride` in `dev`). `main()` stays branch-free.

`run(sys system, ledger)`, `system{start, stderr, sqliteVersion, bindingsOnly, dataDir, openLibrary}`:
SQLite version (as today) → when `bindingsOnly`, `start` and return (no folder, no library) →
`dataDir()` → `openLibrary(ctx, dir)` (`library.OpenLibrary(ctx, filepath.Join(dir,
library.LibraryFile))`; the adapter returns a nil `io.Closer` on error, never a typed nil) → `start`
(`OnShutdown` stops the ledger as today) → `Close` after `start` returns, also when it failed. Each
failure prints `OctopusTorrent: <step>: <error>` and exits 1; a `Close` error (`ErrBusy`/`ErrFull`:
the WAL stays for the next start) is printed and keeps the exit code. `main_test.go` is split as
028's "From the 009 checkpoint" note asks (`startedOptions(t)`, the formatter's exact JSON in its own
test).

#### 6. Workspaces, the gate and QA

- `System.Exec(path, argv, env ...string)` through a pure `mergeEnv(base, extra)` that **replaces** an
  inherited key (Go keeps the first duplicate, `syscall/env_unix.go:31-47`). `System` gains `Lstat`
  and `UserHomeDir`.
- `workspaceRun`: `OCTOPUS_DATA_DIR=<repo root>/build/workspace-data`.
- The gate's `go vet` step also runs with `-tags dev` and `-tags bindings`, so every build-mode file
  compiles in the gate (only the bindings build is otherwise compiled there, and the `dev` file never).
- Gate: a first step snapshots the owner's default folder (`datadir.Default`; exists, its own
  modification time, each entry's name, size and modification time; `.DS_Store` ignored); a last
  step, beside "single code version", fails on any difference and names the likely causes (a step of
  this run, or the owner's app started meanwhile). Every Wails command of the gate (bindings,
  release build, instrumented builds) runs with `OCTOPUS_DATA_DIR=<root>/build/coverage/tool-data`,
  which must still be empty at the end (builds never open a library — a renamed Wails tag fails
  here). Each `.app` run gets `--env OCTOPUS_DATA_DIR=<root>/build/coverage/app-data/<app>` (fresh);
  for the product (`wailsApp.OpensLibrary`) the folder must hold `library.sqlite` and no
  `library.sqlite-wal`/`-shm` after the quit (the override was honoured and `Close` ran last).
- `.gitignore`: `/build/workspace-data/`, `/build/qa-data/`.
- `.claude/agents/macos-qa.md` (+ `go tool devtool codex-agents`): launch with
  `open -n --env OCTOPUS_DATA_DIR=$PWD/build/qa-data`, quit with the Apple Event, not SIGTERM.
- Docs: every `go tool wails dev` becomes `OCTOPUS_DATA_DIR="$PWD/build/workspace-data" go tool
  wails dev` or `workspace run`; after switching to an older branch, delete `build/workspace-data`
  (its library may be newer).

### Files

| File | Change |
|---|---|
| `internal/library/schema.go`, `schema_test.go` | new: schema, inspect, reference, decide, migrate |
| `internal/library/library.go`, `library_test.go` | new: `LibraryFile`, `Library`, `OpenLibrary`, `Close` |
| `internal/library/migrations/library/0001_initial.sql`, `migrations/completion/0001_piece_completion.sql` | new (embedded) |
| `internal/library/testdata/schemas/library-v1.sql`, `README.md` | new: frozen text fixture (schema + one row per table; header with `user_version`, `application_id`); made once from `OpenLibrary` + rows by a scratch test, never regenerated from `0001` — so editing a released migration fails a test. Text, not binary, for reviewable diffs |
| `internal/library/store.go` | edit: `Open` unexported; `open` takes the schema; steps 2 and 4 |
| `internal/library/errors.go`, `errors_test.go`, `completion.go`, `completion_test.go`, `doc.go` | edit |
| `internal/library/helpers_test.go`, `process_test.go`, `main_test.go`, other tests | edit: a test schema with the `rows` table for the store tests; a raw-driver `foreignDB` helper for files of other applications; a child that migrates forever; its marker in `TestMain`'s check |
| `internal/datadir/datadir.go`, `datadir_test.go` | new |
| `main.go`, `main_test.go`, `buildmode_app.go`, `buildmode_dev.go`, `buildmode_bindings.go` | edit / new |
| `internal/devtool/devtool.go`, `system.go`, `system_test.go`, `workspace.go`, `check.go`, `check_test.go`, `appcov.go`, `appcov_test.go`, the test `System` fake | edit |
| `internal/torrentengine/filestore/helpers_test.go` | edit: `guardedLibrary` creates the library with `OpenLibrary`; `intact` = byte-identical + opens |
| `.gitignore`, `.claude/agents/macos-qa.md`, `.codex/agents/macos-qa.toml` (generated) | edit |
| `docs/decisions/library-schema-and-data-folder.md`, `docs/app/data-folder.md`, `docs/verification/014-sqlite-schema-migrations.md` | new |
| `docs/app/library-store.md`, `desktop-shell.md`, `docs/tooling/quality-gate.md`, `devtool.md`, `docs/development/getting-started.md`, `docs/decisions/sqlite-driver.md`, `engine-storage.md`, `quality-gate.md`, `agent-tooling.md`, `project-baseline.md` (O4), `docs/README.md`, `product/APPLICATION_ARCHITECTURE.md` §3/§6, `README.md`, `CLAUDE.md` | edit |
| `product/tasks/014-*.md`; "From 014" notes in 015, 016, 017, 020, 023, 024, 028, 029, 031, 050, 053 | edit; `plan-nav` |

### Tests first (RED → GREEN), in this order (each step leaves `go vet ./...` green)

| Step | Test written first | Expected RED |
|---|---|---|
| 1 | `schema_test.go`, test schemas of three row-bearing migrations: empty → current (id, version, structure = reference); each older version → current with its rows; newer → `ErrUnsupportedVersion`; another id, an unstamped file with objects, an extra table, a changed column, a missing index, our id at version 0 → `ErrUnknownSchema`; a read-only foreign file → `ErrUnknownSchema` (no write lock taken); every refusal leaves bytes, inode and **journal mode** unchanged (a rollback-mode file stays rollback) and no `-wal`/`-shm` behind; `ANALYZE` on a current file still opens; a migration whose second statement fails → `ErrMigrationFailed`, and the bundled SQLite kept the old `application_id`, `user_version` and rows; a migration that rebuilds a parent table keeps its children's rows; one that leaves a dangling reference fails on `foreign_key_check` and keeps nothing; after a migration every new connection has `foreign_keys=1`; the embedded lists match the files | `undefined: schema` |
| 2 | same file, real boundaries: an 8 MiB HFS+ image filled by a migration → `ErrFull` + `ErrMigrationFailed` (including when `SQLITE_FULL` comes from `COMMIT`), old version and rows, integrity `ok`; a read-only file at an old version → `ErrReadOnly`, unchanged; another process holding `BEGIN IMMEDIATE` → `ErrBusy` (short busy), unchanged; two stores migrating one old file at once → both succeed, the migration ran once; a cancel and a **SIGKILL mid-migration**: the test migration first writes ~4 MiB (`randomblob` CTE, beyond the page cache) then loops forever at constant memory; "mid-migration" is detected by the `-wal` file growing, not by a sleep → `ErrCanceled` / after the kill: old version, rows, integrity `ok`, and the next open migrates | the new tests fail: `open` has no schema step |
| 3 | `library_test.go`: `OpenLibrary` on empty → version 1, id OTLB, `foreign_key_check` empty, every table STRICT; each constraint is `ErrConstraint` with the earlier row unchanged (duplicate v1, duplicate v2, no hash, wrong lengths, bad source/theme/language/kind/state, a flag of 2, a limit of 0, a second settings row, a file of a missing torrent, a duplicate path, `finished_at` and `error` against the state, invalid JSON, a bad runtime ID); **two stores inserting the same `infohash_v1` concurrently: exactly one commits, the other `ErrConstraint`**; deleting a torrent removes its files and payload rows and keeps its operations and their torrent rows (real transaction); values round-trip unchanged (max int64 sizes, `MaxUint64` limits and inodes as bit patterns, Unicode paths, binary info with zero bytes, nanosecond times); every enumeration CHECK equals `vocabulary.json` (parsed from the stored SQL); the frozen `library-v1.sql` opens at the current version with every row; a library file opened as marks and a marks file opened as a library → `ErrUnknownSchema`, unchanged. The filestore helper moves to `OpenLibrary` in this step | `undefined: OpenLibrary` |
| 4 | `completion_test.go`: a new store is stamped OTPC; reopen changes nothing; an unstamped 009-shaped file, a newer version and another table → `ErrUnknownSchema`/`ErrUnsupportedVersion`, never `ErrCorrupt`, never changed; the existing full-disk and changed-table tests keep passing | `ErrUnknownSchema` undefined; the 009 file is still accepted |
| 5 | `datadir_test.go` (`HOME`, the override, everything under `t.TempDir()`): default created `0700`, returned resolved; override wins and `home` is never called; set-but-empty, relative, unclean, NUL → refused, nothing created; a file, a symlink to a directory (resolved), group- or world-writable, another owner (seam) → refused; a missing override is created `0700` with its parents; a `home` error or a relative home → refused; `FromOverride` refuses an unset variable; `FromEnvironment` reads the process (variable set via `t.Setenv`, and unset with `HOME` in a temp folder) | `undefined: Resolve` |
| 6 | `main_test.go`: order (version → folder → library → window), each failure exits 1 with its line and calls nothing after it; `bindingsOnly` calls neither folder nor library; the library is closed after `start` returns (also when `start` fails); a close error is printed and keeps exit 0; `openLibrary` with a real temp folder, and with a folder that cannot hold a library (the closer is a nil interface) | `too many arguments in call to run` |
| 7 | `devtool`: `mergeEnv` replaces an inherited `OCTOPUS_DATA_DIR`; `workspaceRun` passes `<root>/build/workspace-data`; the vet step runs for no tags, `dev` and `bindings`; the gate's Wails commands carry the tool folder; the snapshot step fails on a changed default folder (entry added, entry rewritten, folder created) and passes when unchanged; a non-empty tool folder fails; the product's `.app` run fails without `library.sqlite` and with a leftover `-wal`; the spike passes without a library; `OSSystem` `Exec`/`Lstat`/`UserHomeDir` | the new behaviour is missing from the fakes' records |

**Mutation checks (one run each, reverted; in the report):** inspect after the WAL switch; migration
without the re-inspection; `decide` ignoring the structure; foreign keys left on during a migration;
the migration connection returned to the pool; `mergeEnv` appending instead of replacing; the gate
without the snapshot step; `openLibrary` returning a typed nil — each must fail a named test.

No `time.Sleep` decides a result: a lock holder is detected by a busy probe, a running migration by
its WAL growing, children report on stdout; deadlines only bound waits.

### Coverage

Go raw counters through `go tool devtool check`: `internal/library`, `internal/datadir`,
`internal/devtool` 100% from unit tests on real SQLite and real folders; `main.go` from
`main_test.go`, plus `main()` and the success path from the gate's `.app` run (which now opens and
closes a real library in its own folder). The build-mode files hold no function. SQL migrations are
verified by the schema scenarios (not a line metric). No TypeScript, no native code.

### Real boundaries

Real: SQLite (modernc 3.53.4) on APFS temp folders, an 8 MiB HFS+ image, a second process holding
the lock, a child killed mid-migration, read-only files, real folder modes and symlinks, the real
`.app` started by the gate with `OCTOPUS_DATA_DIR`, real Wails builds checked not to open a library.
Seams: `currentUID` (another owner needs root), the busy timeout, test schemas. `/qa` (macos-qa):
with an isolated folder the `.app` starts, creates `library.sqlite` and quits cleanly; with a
newer-version library, a group-writable folder or a read-only folder it refuses to start, prints the
reason, and leaves the files unchanged; the owner's folder is untouched. `not_run` with reason:
power loss (as 008); a full disk at application level (proven at library level; the user-visible
form is 028).

### Documents

ADR `library-schema-and-data-folder.md` (`accepted`): schema 1, table by table; the migration policy
(ids, the decision table, one transaction, foreign keys during rebuilds, the structural check and why
not SQL text, no adoption of unstamped files, refused files' rows untouched); errors; the data
folder (resolution, validation, the save-folder side for 020/029); build modes; start and quit
order; workspace, gate and QA isolation; the rule for manual branch builds and the newer-schema
consequence (016's backup before a migration is the way back); alternatives (a migration library —
a new dependency without id checks; per-migration transactions; comparing stored SQL text; a
key-value settings table; a hashes table; a default settings row; opening in `OnStartup`, which
would show a window before refusing and race its quit); consequences per task. Feature doc
`docs/app/data-folder.md`; `library-store.md` gains the schema, migrations and openers. Report with
the session plan, RED/GREEN, mutation checks, coverage, debt. Task 014 → `in_progress` now, `done`
with its execution result after the gate.

### Verification

1. `go test -race -count=1 ./internal/library ./internal/datadir ./internal/devtool ./internal/torrentengine/filestore .`
2. `go tool devtool check-docs`, `plan-nav`, `check-plan`, `codex-agents`
3. `/qa` with the scenarios above
4. `go tool devtool check` → `GATE PASSED` (`not_run` only for 018 and 035)
5. `/branch-review`; fix findings; gate again
6. Commit/PR only on the owner's `/pushit` (the branch starts from `task/013-transfer-policy`, so it
   sits on #19 until #18 and #19 merge)

### Self-assessment: 5/10 → 8/10

An adversarial review (Plan agent, 25 findings) of the first draft found, and this plan fixes:
- **critical:** Wails' binding generator runs `main()` on every build, so opening in `run` would
  have opened and migrated the owner's library on each build → build-mode tags, the gate's tool
  folder that must stay empty, and a whole-run snapshot of the owner's folder;
- `wails dev` and manual runs would use the real library → the `dev` build requires the override;
  QA agent and docs pass their own folder (also the owner's note);
- an inherited variable would beat an appended one → `mergeEnv`; set-but-empty fell back silently →
  refused;
- foreign keys on during a migration would cascade-delete child rows in any future table rebuild →
  FKs off on a pinned, then discarded, connection, `foreign_key_check` before commit;
- the migration error was classified twice and missed `COMMIT` failures → wrapped once in `open`;
- `operations` lacked `runtime_id` and the error/state rule; limits could not hold `uint64` →
  fixed;
- the kill and cancel tests could pass before any page reached the WAL → a 4 MiB first statement
  and WAL growth as the signal;
- the gate could not prove `Close` ran or that the owner's folder was untouched → no `-wal`/`-shm`
  after quit, folder mtime in the snapshot;
- `sqlite_stat1` would have made a library unknown → internal tables excluded; triggers and views
  described;
- the pre-WAL inspection took a write lock → read-only transaction;
- unreachable error branches (embedded directory read, scans) → explicit embeds, one aggregated
  query;
- staging broke filestore mid-way; a schema-less production branch; unneeded 009 adoption; a missing
  concurrent-duplicate test; 028's `main_test` split → all handled above.

What remains weak, and why it is acceptable:
- The build-mode split relies on Wails' `bindings` and `dev` tag names; a rename in a Wails update
  is caught by the gate (the tool folder stops being empty, or the snapshot changes) before it can
  reach the owner.
- A manually launched branch build (`wails build` + Finder) still opens the default folder: it is
  indistinguishable from the installed app at run time. The rule is documented; 016 adds the backup
  that makes such an upgrade reversible.
- CHECK constraints are outside the structural comparison, so a hand-edited CHECK in a library is
  not detected; editing a released migration is (the frozen fixture).
- Schema 1 is designed ahead of its writers (015, 023, 029, 031, 050); a later column is one
  migration, which this runner is built and tested to apply safely.

## Implementation

### Steps 1–4: the migration policy, library schema 1, the marks store

**RED** (2026-11-04, repository root): `go vet ./internal/library` → `fail`:
`internal/library/helpers_test.go:24:18: undefined: schema` — the tests of `schema_test.go`,
`library_test.go` and the adapted store, safety, process and completion tests were written first;
nothing of `schema`, `inspect`, `reference`, `examine`, `OpenLibrary`, `ErrUnknownSchema` or
`ErrMigrationFailed` existed.

**First GREEN attempt** (`go test -race -count=1 ./internal/library`) → `fail`, and each failure
changed the design or a test precondition:

| Failure | Cause | Change |
|---|---|---|
| `TestATableRebuildKeepsTheRowsThatReferToIt`: 0 children after the rebuild; `TestAMigrationThatBreaksAForeignKeyKeepsNothing`: `FOREIGN KEY constraint failed (787)` | modernc sorts the `_pragma` list of a DSN (`sqlite.go`, "the rest in lexicographic order"), so the appended `foreign_keys(0)` ran before the default `foreign_keys(1)` | `dsn` takes the foreign-key setting once (`foreignKeysOn`/`foreignKeysOff`); no setting is ever given twice |
| `TestANewDatabaseIsPrivateToItsOwner`: no `-wal` after open; the 008/009 full-disk tests: `Close` on a full disk = nil | the migration's own connection closed as the file's last one (the writer had not opened the WAL yet), so SQLite checkpointed the new tables into the file and removed the WAL | redesigned: one **opening connection** (`prepare`, foreign keys off) reads whose file it is, switches to WAL and migrates, then closes before the store's pools open — the same for a new and an existing file, and no Store connection ever runs with foreign keys off. The tests now state the precondition they relied on silently: the WAL files appear with the first write; the full-disk tests write rows that need pages past the end of the file (4 KiB rows; 2000 earlier marks) |
| `TestAReadOnlyWALFileOpensAndRefusesTheWrite`: `Close` = `ErrReadOnly` | measured with a scratch test (removed): SQLite creates `-shm` with the database file's mode, so only the connection that created it may write it; that is now the opening connection, and the writer's closing checkpoint on a `0444` file cannot run | `Close`'s contract names it: on a read-only file the checkpoint is `ErrReadOnly`, and nothing was written; the test asserts it |
| `TestAFullDiskDuringAMigrationKeepsTheOldVersion/in_a_statement`: `ErrCannotOpen` | with no byte free, opening the file could not create the WAL files, before any migration ran | the test holds a reader that keeps the WAL files across the fill, so the migration is what fails |
| `TestTwoStoresMigratingOneFileAtOnceRunTheMigrationOnce` (once): `Close` = `ErrBusy`, "copied -1 of -1" | two closing checkpoints at once — Close's documented `ErrBusy`, not the test's subject; `TestTwoWritersOfTheSameHashCommitExactlyOnce` had the same race | both close after the concurrent part; `-count=100` of the concurrent and rebuild tests → `pass` |
| coverage: `migrate`'s re-examination error not reached | the file must change between the check and the write lock | `settings.afterCheck` (the tests' settings seam, like `walPragma`): `TestAFileThatChangedBeforeTheWriteLockIsCheckedAgain` migrates the file to a newer version there → `ErrUnsupportedVersion` + `ErrMigrationFailed` |

The frozen fixture `internal/library/testdata/schemas/library-v1.sql` was made once by a scratch test
(removed) from `OpenLibrary` and synthetic rows, as its README says.

**GREEN:** `go test -race -count=1 ./internal/library` → `pass` (31 s);
`go test -race -count=1 -covermode=atomic ./internal/library` → `coverage: 100.0% of statements`;
`go test -race -count=1 ./internal/torrentengine/filestore` → `pass` (48 s) with the helper on
`OpenLibrary`; `go vet ./...`, `go vet -tags dev ./...`, `go vet -tags bindings ./...` → `pass`.

### Step 5: the data folder (`internal/datadir`)

**RED:** `go vet ./internal/datadir` → `fail`: `datadir_test.go:17:13: undefined: Override`.
**GREEN:** `go test -race -count=1 -covermode=atomic ./internal/datadir` → `pass`, `coverage: 100.0%
of statements`. Real folders under `t.TempDir()`, real modes and symlinks; another owner through the
`uid` parameter (changing an owner needs root). `LibraryFile` moved here from `internal/library`
during step 7, so devtool can name the file without linking SQLite.

### Step 6: start and quit (`main.go`, build modes)

**RED:** `go vet .` → `fail`: `main_test.go:40:25: undefined: system`.
**GREEN:** `go test -race -count=1 -covermode=atomic .` → `pass`, 93.9%: the only zero block is
`main.go:42.2,51.1`, the body of `main()`, which the gate's `.app` run covers. `go vet -tags dev .` and
`go vet -tags bindings .` → `pass`. The start test was split as 028's note asked (`startedOptions`,
`TestErrorsCrossTheBridgeAsAppErrorJSONWithTheCauseOnStderr` on the exact JSON text).

The build-mode tags were checked against the pinned Wails source: `pkg/commands/build/base.go:217`
adds the output type (`dev` for `wails dev`) as a tag; `pkg/commands/bindings/bindings.go:45-46`
adds `bindings` and removes `dev`, `desktop`, `production` and `debug`.

### Step 7: devtool

**RED:** `go vet ./internal/devtool` → `fail`: `*fakeSystem does not implement System (wrong type
for method Exec)` — the fake, the workspace, gate, `.app`-run and owner-folder tests were written
first. `TestBindingsRegenerateEveryWailsApp` then failed on its expectation of the old command and a
checker without a root; both were updated.
**GREEN:** `go test -race -count=1 -covermode=atomic ./internal/devtool` → `pass`, 100.0% (the
`tool data stuck` case covers the start's removal failure); `go test -race -count=1 ./cmd/devtool` →
`pass`: the real binary replaces itself with a fake `wails` that records the same PID and
`OCTOPUS_DATA_DIR=<git toplevel>/build/workspace-data`, with an inherited `OCTOPUS_DATA_DIR=/Users/me/real`
replaced.

**The tool-folder tripwire on the real Wails** (scratch folders, reverted): with
`buildmode_bindings.go` changed to `generatingBindings = false`,
`env OCTOPUS_DATA_DIR=<scratch>/tool-mut go tool wails generate module` → exit 0 and
`<scratch>/tool-mut/library.sqlite` (`-rw-------`, folder `drwx------`) — the generator opened a
library; with the file restored, the same command with `<scratch>/tool-real` → exit 0 and the folder
does not exist. Wails flipped the modes of `frontend/wailsjs/runtime/*` (content unchanged, as
getting-started says); restored with `git checkout`.

### Whole suite

`go test -race -count=1 ./...` → `pass` (exit 0; `internal/torrentengine` 102 s, `internal/library`
31 s, `internal/torrentengine/filestore` 45 s).

### Mutation checks (one run each, reverted by a script that restored every file)

| Mutation | Test that failed |
|---|---|
| `examine` after the WAL switch | `TestANewerVersionIsRefusedUnchanged`, `TestAFileThatIsNotOursIsRefusedBeforeItIsConverted` (journal mode or files changed) |
| the migration transaction without the re-examination (only `inspect`) | `TestAFileThatChangedBeforeTheWriteLockIsCheckedAgain`. `TestTwoStoresMigratingOneFileAtOnceRunTheMigrationOnce` passed in that run: the two opens did not overlap, so it is not the deterministic guard of this branch |
| `examine` ignoring the structure | `TestAFileThatIsNotOursIsRefusedBeforeItIsConverted` (an extra table, a changed column, a trigger) |
| foreign keys on in the opening connection | `TestATableRebuildKeepsTheRowsThatReferToIt`, `TestAMigrationThatBreaksAForeignKeyKeepsNothing` |
| the store's writer without foreign keys | `TestTheLibraryEnforcesItsConstraints` (every foreign-key case) |
| `mergeEnv` appending instead of replacing | `TestMergeEnvReplacesAnInheritedVariable` |
| the gate without the `data folders` step | `TestCheckPassesTheGateWhenEveryStepPasses`, `TestCheckStepsReportTheirOwnFailures` |
| `openLibrary` returning a typed nil | `TestOpenLibraryOfAFolderThatCannotHoldOneIsAnErrorWithoutACloser` |

A scratch measurement for 028 (removed): on a `0444` WAL file `BEGIN IMMEDIATE` succeeds and the
first `INSERT` fails with `attempt to write a readonly database (8)`, so a read-only probe must write.

## Gate

`go tool devtool check` (2026-11-04, branch `task/014-sqlite-schema`, uncommitted tree) → **`GATE PASSED`**, exit 0.

| Step | Result |
|---|---|
| npm ci, npm cooldown, go env, typecheck, lint | `pass` |
| frontend tests | `pass`: 23 own files at 100% lines, statements, functions and branches |
| frontend build, gofmt | `pass` |
| go vet | `pass`: no build tags, `-tags dev` and `-tags bindings` |
| bindings | `pass`: `frontend/wailsjs/` and `frontend/spike/wailsjs/` match the facades (6 files each) |
| go tests | `pass`: `go test -race -count=1 -covermode=atomic -coverpkg=./... ./...` |
| .app coverage run | `pass`: the instrumented OctopusTorrent and EngineSpike launched and quit with an Apple Event |
| go coverage | `pass`: every block of every own Go file is covered (unit tests + `.app` run) |
| native coverage | `pass`: every region of `internal/platform/trash_darwin.m` |
| escape hatches, scripts, product isolation | `pass` (316 packages in the product, none of the spike, test support or the engine) |
| release build | `pass`: `env OCTOPUS_DATA_DIR=<root>/build/tool-data go tool wails build -clean -trimpath` |
| docs, plan | `pass`: 174 Markdown files; 69 tasks, 198 dependencies |
| release age, npm audit, govulncheck | `pass` |
| updates | `pass`, informational: `github.com/anacrolix/chansync` v0.7.0 → v0.8.0 is out of quarantine (the engine's dependency; taken with 018's pin review, not here) |
| engine integration | `not_run`: task 018 |
| test-bridge UI tests | `not_run`: task 035 |
| single code version | `pass` |
| data folders | `pass`: the owner's data folder did not change (it does not exist on this Mac), and no build opened a library |

After the run, `build/coverage/app-data/OctopusTorrent/` held only `library.sqlite` (`0600`, folder
`0700`) and `build/tool-data` did not exist.

Run 2, after the review fixes, and run 3, after the owner's decisions (both below): `GATE PASSED`,
exit 0, every step as in run 1 (`not_run` only for 018 and 035); run 3's plan step counts 199
dependencies (016 → 023 added).

Run 4, after the docs and agent-rule updates of `/pushit` (no code change since run 3): `GATE FAILED`
at `go tests`, once, in `internal/torrentengine` (108 s). The gate prints only the output's tail: the
anacrolix log of a `v1-long` seeder (all 256 pieces hashed, a peer connection accepted, dropped 10 s
later on a read timeout), so the test is one of the `v1-long` upload scenarios of 010
(`upload_test.go`), whose time windows can flake on a CPU-starved run, as report 010 said. Not
reproduced: `go test -race -count=1 ./internal/torrentengine` → `ok` (103 s), and the whole suite in
parallel, `go test -race -count=1 -json ./...` → every package `pass`, no failed test. These tests do
not open a library: `internal/library` reaches the test binary only through `filestore`. Run 5, on
the committed tree, is the acceptance run; its result is in the commit message and the pull request.

## Manual QA (`/qa`, the `macos-qa` agent, 2026-11-04)

The release build of this tree (`go tool wails build -clean -trimpath`), driven through Accessibility;
every launch with its own `OCTOPUS_DATA_DIR`; refusals run the executable directly to capture stderr.
The owner's folder was checked only for existence: `absent` before and after.

| # | Scenario | Expected | Observed | Result |
|---|---|---|---|---|
| 0 | `wails build` with `OCTOPUS_DATA_DIR=build/qa-tool` | exit 0, `build/qa-tool` not created | exit 0, absent | `pass` |
| 1 | start with `build/qa-data`, **Check Go connection**, quit through the app's menu | `Go go1.27.3 · darwin/arm64 · SQLite 3.53.4`; folder `700`, `library.sqlite` `600`; after the quit only `library.sqlite`, header `0202`, `application_id` 1330924610, `user_version` 1, 7 tables | exactly that; the process gone 2 s after the menu quit | `pass` |
| 2 | start again on the same folder | starts, quits, the file unchanged | the same text; sha256 `42680341…cc233` before and after; only `library.sqlite` | `pass` |
| 3 | a copy at `user_version = 2` | exit 1, no window, `written by a newer version`, the file unchanged | `OctopusTorrent: open the library: the database was written by a newer version of the application: the library database is at version 2, this build reads versions up to 1`; sha256 unchanged | `pass` |
| 4 | another application's SQLite file (`notes` table, rollback journal) | exit 1, no window, `not one this application made`, unchanged, still rollback | `… is not one this application made for this purpose: application_id 0x0, user_version 0, 1 schema objects; …`; sha256 and header `0101` unchanged; no `-wal`/`-shm` | `pass` |
| 5 | a folder of mode `775` | exit 1, `find the data folder:`, nothing created | `… is a drwxrwxr-x of uid 501; want a folder of uid 501 that its group and others cannot write`; folder empty, still `775` | `pass` |
| 6 | `OCTOPUS_DATA_DIR=relative/data` | exit 1, `not an absolute, clean path`, nothing created | exactly that; no `relative` folder | `pass` |
| 7 | `wails dev` without the variable | the dev app stops with `OCTOPUS_DATA_DIR is not set`, no window | after the bindings and the compile: `OctopusTorrent: find the data folder: the data folder cannot be used: OCTOPUS_DATA_DIR is not set, and a development build never opens the owner's data folder`; no window; after SIGINT no wails, vite or app process left; `frontend/wailsjs`, `go.mod`, `go.sum` and the npm files unchanged | `pass` |
| 8 | the owner's folder | never created | `absent` at the end | `pass` |
| 9 | a read-only data folder (`chmod 500`, empty), run after the review | exit 1, no window, `open the library:`, nothing created, mode kept | `OctopusTorrent: open the library: the library database cannot be opened: open …/library.sqlite: permission denied`; folder empty, still `500` | `pass` |

| 1′ | the final build (gate run 3), a fresh `build/qa-data` | as 1, and `operations` stores `kind TEXT NOT NULL CHECK (kind <> '')` | exactly that; the process gone 2 s after the menu quit | `pass` |
| 5′ | the final build, a folder of mode `775` | exit 1, `only its owner can open (chmod 700)`, folder empty | `… is a drwxrwxr-x of uid 501; want a folder of uid 501 that only its owner can open (chmod 700)`; folder empty | `pass` |
| 5b | the final build, a folder of mode `755` (accepted before the review) | exit 1, the same message, folder empty | `… is a drwxr-xr-x of uid 501; want a folder of uid 501 that only its owner can open (chmod 700)`; folder empty | `pass` |
| 10 | the final build, `library.sqlite` a symlink to an empty `important.db` | exit 1, `not a regular file`, the target unchanged | `OctopusTorrent: open the library: the library database cannot be opened: …/library.sqlite is a L---------, not a regular file`; `important.db` still 0 bytes, same sha256; no `-wal`/`-shm` | `pass` |

Every process the agent started (two app instances, wails dev, its vite) exited; its temporary folders
were removed. Scenario 9 needed a rebuild first (same command, `build/qa-tool` again not created):
`wails dev` (scenario 7) rebuilds `build/bin/OctopusTorrent.app` as its dev app and leaves no
executable there when it exits, so a `.app` must be rebuilt after `wails dev`. The rebuild included the
review fixes, none of which touches the start path of the application build.

One observation, consistent with the ADR: in scenario 3 the copy was prepared with macOS's
`/usr/bin/sqlite3`, whose build keeps the WAL file persistent, so it left an empty `-wal` and a
`-shm` beside the file. The refusing start (read-only examination, then close as the last
connection) removed those two empty files; the main file's bytes were unchanged. This is the
"SQLite may checkpoint a refused file's own WAL" case the ADR names — no row and no byte of the
database changed.

## Deviations from the plan

- The gate's tool folder is `build/tool-data`, not `build/coverage/tool-data`: the `go tests` step
  removes `build/coverage` after the bindings step, so a library a bindings run opened there would be
  gone before the last step looks.
- The other-owner seam is a `uid` parameter of `datadir.resolve`, not a `currentUID` variable: the two
  exported resolvers pass `os.Getuid()`, the tests pass another number.
- The pinned, then discarded, migration connection became a separate opening connection (`prepare`,
  closed before the store's pools), for the reason in the [library store](../app/library-store.md#refactoring-and-debt-check)
  document: restoring foreign keys on a pinned connection left error paths no test reaches.

## Refactoring

- The exported raw `library.Open` is gone: every database opens through its schema.
- 009's `ensureCompletionSchema` became the generic `examine`/`migrate`; `ErrUnsupportedVersion`
  moved to `errors.go` with its meaning narrowed to "ours, newer".
- `run` took four arguments; it takes `system`, one struct of its outside world. The start test was
  split as 028's note asked (`startedOptions`, the formatter's exact JSON in its own test).
- `datadir.LibraryFile` lives with the folder's layout, so devtool checks the gate's folders without
  linking SQLite.
- `System.Exec` takes extra environment through `mergeEnv`, which replaces an inherited variable;
  `System` gained `Lstat` and `UserHomeDir`.

## Not run

| Check | Why | Where it is covered instead |
|---|---|---|
| Power loss during a migration | cannot be produced on this Mac (as in 008) | SIGKILL mid-migration, `synchronous=FULL` + `fullfsync` on every connection |
| A full disk at the application start | the user-visible form is 028's | `ErrFull` + `ErrMigrationFailed` on a real full HFS+ image, in a statement and at `COMMIT` |
| An automated run of the `dev` build without `OCTOPUS_DATA_DIR` | `go test -tags dev .` does not link (Wails' `dev` frontend needs the `UniformTypeIdentifiers` framework that only the Wails CLI adds); the gate vets the file | QA scenario 7 on the real `wails dev`; `datadir.FromOverride`'s own tests |
| Engine integration, test-bridge UI tests | tasks 018, 035 | — |

## Debt

None left by this task: no ignore directive, no skipped test, no TODO, no stub. The limits are
recorded, not deferred work: CHECK contents are outside the structural comparison (the frozen
`library-v1.sql` guards the released schema); a manually launched branch build is the application
build and opens the owner's folder unless given its own (documented; 016's backup makes such an
upgrade reversible); the build modes rely on Wails' `bindings` and `dev` tag names (a rename of
`bindings` fails the gate's `data folders` step; a rename of `dev` would make `wails dev` open the
owner's folder, which only the documented `OCTOPUS_DATA_DIR` in every `wails dev` command and QA
scenario 7 guard).

## Review (`/branch-review`, 2026-11-04)

Three agents (code, security, tech lead); no critical finding. Each fix below started from a failing
test where it changed behaviour.

| Finding | Severity | Outcome |
|---|---|---|
| A symlink at the database path was followed: an empty file elsewhere became a library (security L1, reproduced) | low | fixed. RED `TestASymlinkToAnEmptyFileIsRefusedAndItsTargetKept` (`Open through a symlink = <nil>`); `open` takes `Lstat` and requires a regular file (`ErrCannotOpen`); `Store.check` uses `Lstat` too (`TestADatabaseReplacedByASymlinkToItselfIsNotWrittenTo`, which fails with `Stat`). `TestAFolderAtTheDatabasePathIsRefusedAndKeptEmpty` pins that SQLite already refused a folder |
| An existing `0644` library keeps its mode, and a `0755` data folder was accepted (security L2) | low | fixed at the folder: the data folder must have no permission bit for group or others, so no file in it is readable by them whatever its own mode. RED: `readable by the group` (`0750`), `readable by others` (`0755`), `searchable by others` (`0711`) were accepted; the message now says `only its owner can open (chmod 700)` |
| `dataFolders` discarded the `ReadDir` error of `build/tool-data` (code W2, security L5) | warning | fixed. RED case `the tool folder unreadable` passed the step; a missing folder is the expected answer, any other error fails it. The fake now answers a missing tool folder with `fs.ErrNotExist`, as the real system does |
| The marks table was not `STRICT` (tech lead) | optional | fixed; no shipped marks file exists. RED: `piece_completion strict = 0` |
| `buildmode_bindings.go` assigned `FromEnvironment` (tech lead) | nit | now `FromOverride`, so a reordered `run` fails closed |
| `Close`'s doc comment wrapped badly (code W1) | warning | re-wrapped |
| "Refused unchanged" / "changes no file" overstated (security L4) | low | `doc.go` and the data-folder doc now say "rows unchanged", with the ADR's WAL and hot-journal caveat |
| The ADR said the gate catches a renamed `dev` tag (found while answering code W3) | — | corrected: only a renamed `bindings` tag fails the gate; the toolchain ADR now asks a Wails update to re-check both tags |
| No automated run of the `dev` build without the variable (code W3) | warning | `not_run` with reason (see Not run): `go test -tags dev .` does not link; QA scenario 7 is the evidence |
| Only the data folder, not its parents, is checked (security L3) | low | documented in the ADR and the data-folder doc: an override sits under a folder only its user can write |
| The owner-folder snapshot uses `Lstat` on the folder (security L5) | low | kept: through a symlinked folder every entry is still compared; only a file created and removed during the run would be missed. The app creates a real folder there |
| `info.Sys().(*syscall.Stat_t)` is unchecked (code W4) | warning | kept: macOS is the only target ([macos-target](../decisions/macos-target.md)), and the two-value form adds a branch no test on it can reach |
| `settings.afterCheck` is a test hook in `defaults` (code W5) | info | kept, like `walPragma`: unexported, a no-op in the product |
| A hand-run branch build opens the owner's folder (security M1) | medium | the ADR's accepted residual; noted in 023 (the first task that writes the owner's records) and raised with the owner: 016's backup should exist before then |
| `.gitignore` un-ignores everything under `testdata/` (security L7) | low, older than 014 | not changed: committed `.torrent` fixtures live there; 014 adds only `.sql` and a README |
| Handoff, refactoring, debt, `not_run`, deviations, QA of a read-only folder (tech lead) | must | done: the sections above, QA scenario 9, the task's execution result |

Notes for later tasks were added from the review: 015 (`openLibrary` returns `*library.Library`), 016
(a `VACUUM INTO`-style backup through the WAL; `ErrCorrupt` files moved aside, never deleted), 017
(start errors carry full paths), 023 (the first owner records before 016), 028 (the engine's quit
before the library's `Close`; the quit budget), 029 (restored paths are untrusted).

The QA scenario 5 row above quotes the message before the review; the rule and its message changed
with L2, and rows 1′, 5′, 5b and 10 are the re-run on the final build. The symlink is printed as Go's
mode string (`L---------`): readable enough on stderr, and the user-visible wording of start failures
is 028's.

## Owner decisions after the review (2026-11-04)

1. **The operation kind is checked in Go, not by a `CHECK` list.** The tech lead noted that every
   kind added later (030, 056–061) would be a migration rebuilding the journal, and that migration 1
   can change cheaply only before it is released. The owner chose Go-only kinds:
   - `operations.kind` is now `CHECK (kind <> '')`, and 015's repository refuses a kind outside the
     vocabulary;
   - the closed lists keep their `CHECK`s: import sources, themes, languages, operation states.

   RED:
   - `TestEveryEnumerationIsTheContractsVocabulary`: the kind was still a list;
   - `TestAnOperationKindOfALaterTaskNeedsNoMigration`: `relocateData` was refused by the CHECK.

   The constraint case `an unknown operation kind` became `an empty operation kind`. The frozen
   `library-v1.sql` changed with the migration, which is allowed only because neither is released
   yet.
2. **016 is a hard dependency of 023** (security M1): the owner's first records land only after the
   backup before a migration exists. `plan-nav` regenerated; `check-plan` → `pass` (199
   dependencies).

Found while making change 1: the frozen fixture did not guard a released migration's `CHECK` text.
- A mutation of the migration's `CHECK` passed `TestTheFrozenFirstSchemaOpensAtTheCurrentVersionWithEveryRow`,
  because the structural comparison leaves `CHECK` contents out. The plan's self-assessment ("editing
  a released migration is [detected] (the frozen fixture)") was wrong until this change.
- New: `TestTheFrozenSchemaIsWhatItsMigrationsBuild` compares the schema text SQLite stores after
  migration 1 with the fixture's `CREATE` statements. The same mutation now fails it, and the
  restored migration passes.
