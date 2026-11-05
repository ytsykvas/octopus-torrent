# The library schema, its migrations and the data folder

**Status:** `accepted`  
**Date:** 2026-11-04  
**Task:** [014](../../product/tasks/014-sqlite-schema-migrations.md)  
**Register item:** O4 (014's part: schema, migrations, data folder; [baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `STACK-04`, `SET-02`

## Question

[008](sqlite-driver.md) fixed how a SQLite file is opened (WAL, full sync, one writer, never a reset)
and the data folder's contract, but opened any file it was given and switched it to WAL before
knowing whose it was; there was no schema, the marks store of [009](engine-storage.md) checked its own
interim `user_version 1`, and the product opened only `:memory:`. 015 (repositories), 016 (backup),
023–031 (import, startup, removal) and 050 (settings) need tables to write to, a migration policy
that never loses a row, and a data folder that a workspace, a test, the gate or a build never shares
with the owner. The owner decided on 2026-11-04 that 014 takes the whole data-folder duty, including
opening the library at start.

## Decision

### Every database is opened through its schema

- A schema (`internal/library/schema.go`) is an `application_id`, a name and an explicit list of
  embedded migrations, one file each: `migrations/<schema>/NNNN_<what>.sql`, embedded by name into its
  own variable (no directory read, so no unreachable error), listed in order;
  `TestTheEmbeddedMigrationsAreTheFilesInOrder` keeps the list equal to the files. Migration `i`
  brings a database from version `i` to `i+1` (`PRAGMA user_version`). A released migration never
  changes; a change is the next file.
- Two schemas: the library (`OpenLibrary`, `application_id` `0x4F544C42` "OTLB", `library.sqlite`)
  and the piece-completion marks (`OpenCompletion`, `0x4F545043` "OTPC", 009's table as migration 1).
  The raw opener is unexported; there is no way to open a file without a schema.
- **Whose file is it** (`examine`): the file's `application_id`, `user_version`, number of
  `sqlite_schema` rows and a **structural description** (`inspect`, one aggregated query): every
  table (type, `WITHOUT ROWID`, `STRICT`), column (name, declared type, `NOT NULL`, default, key
  position, hidden), foreign key (columns, parent, actions, match), index (unique, origin, partial,
  columns) and the SQL of triggers and views, sorted, from the pragmas whose output does not depend
  on how a statement was spelled. SQLite's own tables (`sqlite_stat1` after `ANALYZE` or `PRAGMA
  optimize`, `sqlite_sequence`) are left out. The reference is what migrations `1..v` build on a
  private in-memory database (`reference`).

  | File | Result |
  |---|---|
  | no `application_id`, version 0, nothing in `sqlite_schema` | new: stamped and migrated to the current version |
  | ours, `1 ≤ version ≤ current`, structure = the reference of its version | migrated from its version (nothing when current) |
  | ours, version > current | `ErrUnsupportedVersion`: a newer build wrote it |
  | anything else — another `application_id`, an unstamped file with objects, ours at version 0, ours with another structure | `ErrUnknownSchema` |

  A migration list that does not build is an error of its own, never a refusal.
- **Opening** (`open` → `prepare`, on a connection of its own, closed before the store's pools open):
  1. the 008 checks (absolute clean path, the empty-file-beside-WAL refusal, the `0600`/`O_EXCL`
     create);
  2. `examine` in a **read-only transaction** (a plain `BEGIN`, no write lock), before anything is
     written: a refused file keeps its bytes, inode and journal mode, and a read-only file of another
     application is `ErrUnknownSchema`, not `ErrReadOnly`;
  3. the switch to WAL;
  4. when the version is older: **one transaction** (`BEGIN IMMEDIATE`) that examines again under the
     write lock (another store may have migrated meanwhile; the second of two concurrent opens finds
     nothing to do), sets the `application_id`, runs every pending migration, sets the new
     `user_version` and requires `foreign_key_check` to return no row before `COMMIT`. Any error —
     `COMMIT` included — rolls everything back: the old version, the old `application_id`, every row.
     It is `ErrMigrationFailed` "the <schema> database from version k to n", joined with the cause's
     own kind (`ErrFull`, `ErrBusy`, `ErrReadOnly`, `ErrCanceled`, `ErrConstraint`, `ErrIO`, …).
     An I/O error at the migration's `COMMIT` (015) is also of unknown outcome, but it heals
     itself: the next open examines the version again and migrates only if the file is still old
     (`TestAMigrationWhoseCommitCannotWriteFailsAndTheNextOpenMigrates`). So it stays
     `ErrMigrationFailed`, never `ErrOutcomeUnknown`.
- **Foreign keys are off on the opening connection** and on in every connection of a Store. SQLite
  ignores the setting inside a transaction, and with it on, rebuilding a table (create, copy, drop,
  rename — SQLite's documented procedure for any change `ALTER TABLE` cannot make) would delete every
  row that refers to it through `ON DELETE CASCADE`; the check before `COMMIT` refuses a migration
  that leaves a reference broken. The driver sorts a DSN's `_pragma` list, so the setting is given
  once per connection (`foreignKeysOn`/`foreignKeysOff`), never overridden.
- The opening connection closes as the file's last one, so SQLite checkpoints the migration into the
  file and removes the WAL; a store's `-wal`/`-shm` appear with its first write. On a read-only file
  the `-shm` that the opening connection created has the file's mode, so `Close`'s checkpoint is
  `ErrReadOnly` (nothing was written).
- A refused file's rows are never changed. Reading it may still let SQLite roll back its own hot
  journal or checkpoint its own WAL, as any reader would.
- `ErrUnsupportedVersion` (moved from `completion.go`, narrowed to "ours, newer"),
  `ErrUnknownSchema` and `ErrMigrationFailed` are never `ErrCorrupt`, so a caller that rebuilds a
  damaged database (016, 029) cannot destroy a newer library or another application's file. 009's
  unstamped marks files are not adopted: no shipped build ever wrote one (only tests and the spike's
  temporary folders), so they are `ErrUnknownSchema` and stay unchanged.

### Library schema 1 (`migrations/library/0001_initial.sql`)

Every table is `STRICT`. Unsigned 64-bit values (inode, device, speed limits) are stored as their
int64 bit pattern; times are Unix nanoseconds, UTC. The closed enumerations (import sources, themes,
languages, operation states) are `CHECK (x IN (...))` lists equal to
`internal/app/contracts/testdata/vocabulary.json` (`TestEveryEnumerationIsTheContractsVocabulary`).
The operation kind is not (owner decision, 2026-11-04): later tasks add kinds (030, 056–061), and a
`CHECK` list cannot change without rebuilding the journal; SQLite refuses only an empty kind, and the
repository (015) checks a kind against the vocabulary in Go before it writes.

| Table | Columns and constraints |
|---|---|
| `torrents` | `id` (36 characters, lower case; Go validates the UUID) primary key; `infohash_v1` (20 bytes) and `infohash_v2` (32 bytes), each `UNIQUE`, at least one; `name`; `info` (the verified info dictionary, non-empty); `source` (the import sources); `source_metainfo` (the imported `.torrent` bytes); `source_magnet`; `save_path` (non-empty); `manual_paused` (0/1); `added_at` |
| `torrent_files` (without rowid) | `torrent_id` → `torrents` `ON DELETE CASCADE`; `file_index ≥ 0`; `path` (non-empty, the layout-relative path of `files.FileState`); `size ≥ 0`; `wanted` (0/1); primary key (`torrent_id`, `file_index`), `UNIQUE` (`torrent_id`, `path`) |
| `payload_records` (without rowid) | `torrent_id` primary key → `torrents` `ON DELETE CASCADE`; `clean` (0/1); `file_system`, `device`, both set when clean. `clean = 0` is 029's "not clean" marker, written before the first payload write; no row means the payload never ran |
| `payload_files` (without rowid) | `torrent_id` → `payload_records` and (`torrent_id`, `file_index`) → `torrent_files`, both `ON DELETE CASCADE`; `present`, `size ≥ 0`, `mtime`, `ctime`, `inode`, `device`, `regular` (`files.FileState`). The second foreign key makes `files.Decide`'s rule "a record lists a file the torrent lacks" impossible from the library, deliberately |
| `settings` | one row (`id = 1`), written by the first save (050: SET-06 takes the first language from macOS); no row means the defaults, upload allowed. `default_save_path` (null or non-empty), `theme`, `language`, `download_limit` and `upload_limit` (null = unlimited, never 0), `global_upload_disabled` (0/1) |
| `operations` | `operation_id` primary key and `runtime_id` (the run that accepted it), both 36 lower-case characters; `kind` (non-empty; the vocabulary's kinds are checked in Go); `fingerprint` (non-empty); `request` (`json_valid`); `state` (the operation states); `error` (null or `json_valid`); `accepted_at`, `finished_at`. A final state ⇔ `finished_at` set; `error` set ⇔ failed or cancelled ([api-contract](api-contract.md)) |
| `operation_torrents` (without rowid) | `operation_id` → `operations` `ON DELETE CASCADE`; `torrent_id` (36 lower-case characters, **no foreign key**: the journal outlives the record it names, for recovery and a repeated `operationID`, 031); `outcome` (null or `json_valid`); primary key (`operation_id`, `torrent_id`); index on `torrent_id` |

`internal/library/testdata/schemas/library-v1.sql` is schema 1 with rows in every table, made once
from `OpenLibrary` and never regenerated. The schema text SQLite stores after migration 1 must equal
the text frozen there (`TestTheFrozenSchemaIsWhatItsMigrationsBuild`: the structural check leaves
`CHECK` contents out, so this is what catches an edited released migration), and the fixture's rows
open at the current version (`TestTheFrozenFirstSchemaOpensAtTheCurrentVersionWithEveryRow`). A new
value of a closed enumeration is a table rebuild, which the runner applies without losing child rows;
a new operation kind needs no migration.

### The data folder (`internal/datadir`)

- `OCTOPUS_DATA_DIR`, when set, names the folder: absolute, clean, no NUL; set but empty is refused,
  never a silent fallback. Unset: `<home>/Library/Application Support/dev.tsykvas.octopustorrent`
  (`Default`; the home folder must be absolute). The library is `<folder>/library.sqlite`
  (`datadir.LibraryFile`).
- The folder is created `0700` with its parents when missing, then accepted only when its resolved
  path (symlinks followed) is a folder of the current user that its group and others cannot open
  (no permission bit for them: then no file in it is theirs to read, whatever the file's own mode —
  an older or restored `0644` library included; stricter than 008's "not writable");
  the resolved path is returned. Any failure is `ErrUnusable`. Only the folder itself is checked,
  not its parents: someone who may write a parent could swap it between the check and the open. For
  the default folder every parent is the user's; an override is documented to sit under a folder only
  its user can write.
- "Not inside a torrent save folder" cannot be checked from here (the save folders are in the library
  this folder holds): 020 refuses a save folder that is, contains or lies inside the data folder, and
  029 checks restored records.

### Build modes, start and quit (`main.go`)

- Wails runs `main()` to generate the bindings on every `wails build`, `wails generate module` and
  `wails dev`, built with `-tags bindings` (Wails v2.16.0 `pkg/commands/bindings/bindings.go:45-46`);
  `wails dev` builds the app with `-tags dev` (`pkg/commands/build/base.go:217`). Three files with no
  function decide the mode: `buildmode_bindings.go` (`bindings`: no data folder, no library; its
  unused resolver is `datadir.FromOverride`, so a reordered `run` fails closed),
  `buildmode_dev.go` (`dev && !bindings`: `datadir.FromOverride`, refusing to start without
  `OCTOPUS_DATA_DIR`), `buildmode_app.go` (`!dev && !bindings`: `datadir.FromEnvironment`). The gate
  vets all three.
- Start: the SQLite version → (bindings: the window's options only) → the data folder → the library
  (created or migrated) → the window; the library is closed after `wails.Run` returns, also when the
  window failed. Each failure is `OctopusTorrent: <step>: <error>` on stderr and exit 1; a `Close`
  error is printed and keeps the exit code. Opening in `OnStartup` was rejected: Wails runs it in a
  goroutine beside the window, which would show a window before refusing a library and race its quit.

### Isolation from the owner's library

- `workspace run` passes `OCTOPUS_DATA_DIR=<worktree>/build/workspace-data`, replacing an inherited
  value (`mergeEnv`: a process keeps the first of two equal names).
- The gate: every Wails command runs with `OCTOPUS_DATA_DIR=<root>/build/tool-data`, emptied at the
  start and required empty at the end (no build opens a library; one there means the build-mode tags
  failed); each `.app` run gets `--env OCTOPUS_DATA_DIR=<root>/build/coverage/app-data/<app>`, and
  the product's must hold `library.sqlite` and no `-wal`/`-shm` after the quit (the override was
  honoured, and the library was closed last); the last step, "data folders", fails when the owner's
  folder (the folder's own modification time, each entry's size and modification time, `.DS_Store`
  aside) differs from the start.
- `/qa` (`macos-qa`) launches with `--env OCTOPUS_DATA_DIR=$PWD/build/qa-data` and quits through the
  app's own menu, so the library is closed.
- **The owner's rule for manual runs of a branch build:** give it its own `OCTOPUS_DATA_DIR`. A branch
  with a new migration would otherwise upgrade the real library, and `main`'s build would then refuse
  it as `ErrUnsupportedVersion` until 016's backup is restored. After switching a worktree to an older
  branch, delete its `build/workspace-data`, whose library may be newer.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| A migration library (golang-migrate, goose) | Familiar, versioned files | A new dependency; no `application_id` check before converting a file; its own version table instead of `user_version` | The policy is a hundred lines over `library.Tx`, tested on real SQLite |
| One transaction per migration | A failure keeps the earlier steps | A file can stop at a version no build released as a whole; more states to test | One transaction keeps "old version or current" as the only outcomes |
| Compare the stored SQL text | Exact | Changes with whitespace, `ALTER TABLE` and SQLite versions; a rename rewrites it | Pragmas describe the structure; the frozen fixture's text test guards the released text |
| Foreign keys on during migrations | Simpler connection setup | A table rebuild cascades deletes into child rows | Off on the opening connection, with `foreign_key_check` before `COMMIT` |
| Restore foreign keys on a pinned connection after migrating | One connection | The restore's error paths cannot be reached by any test | A separate opening connection, closed before the store's own |
| A key-value settings table | No migration for a new setting | No type or value constraints | Typed columns with `CHECK`s; a new setting is one migration |
| A separate table of hashes | Room for more hash kinds | v1 and v2 are the only infohashes; joins everywhere | Two `UNIQUE` columns, at least one set |
| A settings row created with the schema | Never missing | Its language would be fixed before SET-06 reads macOS's | No row until the first save |
| The operation kinds as a `CHECK` list | SQLite refuses an unknown kind itself | Every new kind (030, 056–061) is a migration that rebuilds the journal at start, with its own frozen fixture | The owner chose Go-only kinds; `SQL only through the repository` makes Go the single writer |
| Open the library in `OnStartup` | Runs inside Wails | A window appears before a refusal; races the quit | Before `wails.Run`, closed after it returns |
| Adopt 009's unstamped marks files | Keeps marks from earlier test runs | No shipped build wrote one; an adoption path is code without a user | Refused as `ErrUnknownSchema`, unchanged |

## Verified grounds

All on Apple M4, macOS 27.0.1, Go 1.27.3, modernc.org/sqlite v1.60.1 (SQLite 3.53.4); commands and
results in [the 014 report](../verification/014-sqlite-schema-migrations.md).

- Real SQLite in temporary folders: every older version migrates with its rows; newer, foreign,
  unstamped, changed-structure, trigger and read-only foreign files are refused with bytes, inode and
  journal mode unchanged; `ANALYZE` statistics do not refuse a library; a failing migration, a broken
  foreign key, a full 8 MiB HFS+ image (in a statement and at `COMMIT`), a read-only file, another
  process holding the write lock, a cancel and a `SIGKILL` in the middle of a migration each keep the
  old version and every row (`integrity_check` ok, the next open migrates); two concurrent opens run
  the migration once; a file migrated further between the check and the write lock is refused.
- Schema 1: about 50 constraint cases are `ErrConstraint` with the rows unchanged; two stores
  inserting one `infohash_v1` at once commit exactly once; deleting a torrent keeps its operations;
  values round-trip exactly.
- Measured, not assumed: modernc sorts `_pragma` values (`sqlite.go`, "the rest in lexicographic
  order"), so an appended `foreign_keys(0)` did not apply; the `-shm` mode on a read-only file
  (scratch test, removed).
- `internal/datadir` on real folders, modes, symlinks; the owner seam (`uid`) for another user.
- The gate's `.app` run of the instrumented product with its own data folder (the library created and
  closed) and the "data folders" step: in the 014 report.

## Consequences

- **015** (done, [library-repositories](library-repositories.md)) built the repositories on
  `Library`, mapped `ErrUnknownSchema`, `ErrUnsupportedVersion` and `ErrMigrationFailed` to
  `AppError` (`library.notALibrary`, `library.newerVersion`, `library.migrationFailed`), validates
  UUIDs and paths before they reach the `CHECK`s, and refuses an operation kind outside the
  vocabulary before it writes (SQLite checks only that it is non-empty).
- **016** backs the library up before a migration (`VACUUM INTO`, 008) — the way back from a branch
  build's upgrade — and handles `ErrCorrupt`; it never rebuilds on the three schema errors. It is a
  hard dependency of **023**, the first task that writes the owner's records (owner decision,
  2026-11-04).
- **020** refuses a save folder that is, contains or lies inside the data folder; **029** checks the
  restored records the same way.
- **023** writes `torrents` (+ `source_*`) and `torrent_files`; **024** decides whether file priority
  needs a migration; **029** writes `payload_records`/`payload_files` (clean = 0 before the first
  payload write); **031** keeps `operations`/`operation_torrents` after a removal; **050** writes the
  first settings row; **053** stores the global upload stop.
- **028** shows the start failures to the user (today stderr) and adds the single-instance lock.
- **017** decides where logs live in the data folder.
- A new migration adds `library-v(N+1).sql` beside the frozen v1 fixture.
- A Wails update re-checks the two tag names (`pkg/commands/bindings/bindings.go`,
  `pkg/commands/build/base.go`). A renamed `bindings` tag fails the gate's `data folders` step (the
  tool folder gets a library); a renamed `dev` tag would not fail the gate, which never runs `wails
  dev`: the dev app would then be the application build. Every documented `wails dev` command passes
  `OCTOPUS_DATA_DIR`, so even then it opens its own folder.
