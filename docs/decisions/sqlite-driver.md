# The SQLite driver and how every library database is opened

**Status:** `accepted`  
**Date:** 2026-10-17  
**Task:** [008](../../product/tasks/008-sqlite-driver-durability-spike.md)  
**Register item:** O4 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `STACK-04`, `SET-02`, `AI-07`

## Question

The library (014–016, 023–031) keeps the user's torrents, file selection, pause and settings in
SQLite (SET-02). Until 008, five things were open:

- which Go driver, with its bundled SQLite;
- the journal mode;
- what "committed" guarantees on this Mac;
- how connections are pooled and writes ordered;
- where the database lives.

Two constraints shape the answer:
- Wails on macOS builds with cgo. The pinned engine then compiles its own C SQLite
  (`go-llsqlite/crawshaw`, SQLite 3.49.1) for its default piece completion.
- The WAL-reset bug ([wal.html §11](https://www.sqlite.org/wal.html)) affects SQLite 3.7.0–3.51.2
  and is fixed in 3.51.3.

## Decision

- **Driver: `modernc.org/sqlite` v1.60.1** (2026-09-29).
  - It bundles **SQLite 3.53.4**.
  - It is C transpiled to Go, so it adds no C code and no dynamic library.
  - It is used only through `database/sql`, inside `internal/library`.
- **Every database is opened by `library.Open(ctx, absolutePath)`** with these settings. Each is read
  back by a test on every pooled connection:

  | Setting | Value | Why |
  |---|---|---|
  | `journal_mode` | `WAL`, set once, kept in the file | Readers never wait for the writer, and the writer never waits for readers |
  | `synchronous` | `FULL`, every connection | In WAL a commit syncs the WAL; `NORMAL` may lose the last commits on power loss |
  | `fullfsync` | `ON`, every connection | On macOS `fsync` does not reach stable storage; `F_FULLFSYNC` does. A commit costs about 4 ms with it and 0.1 ms without |
  | `busy_timeout` | 5000 ms, every connection | Another connection's lock is waited for; then the error is `ErrBusy` |
  | `foreign_keys` | `ON`, every connection | Constraints hold for every write |
  | `query_only`, `mode=ro` | on readers | A reader cannot write, even if it turns `query_only` off, and cannot create a file |

- **Pool and write order:**
  - **Writer:** one `*sql.DB` with `MaxOpenConns=1`. Its transactions are `BEGIN IMMEDIATE`
    (`_txlock=immediate`), so every write takes the lock at `BEGIN` and a read lock is never
    upgraded into a deadlock. Writes are **serialized, not FIFO**: database/sql hands a freed
    connection to a random waiter, so two concurrent `Write`s may commit in either order. The order
    of commands for one torrent (STACK-03) comes from the service's per-torrent queue (013/025), not
    from the store.
  - **Readers:** a separate pool of 4 read-only connections. Each `Read` is one consistent snapshot.
  - **Checkpoints:** only the writer checkpoints, automatically and with `wal_checkpoint(TRUNCATE)`
    at `Close`. This is a structural guard against the WAL-reset race, in addition to the fixed
    version.
  - **No network or engine wait inside a transaction.** 015 enforces this for repositories.
- **API** (014/015 build on it, and add schema and repositories):
  - `Write(ctx, fn func(Tx) error)` and `Read(ctx, fn)`.
  - `library.Tx` runs every statement under the operation's context, so a cancel interrupts the
    running statement. database/sql's context-free methods would let it run to the end; the spike
    hung on exactly that.
  - `Write` returns nil only when the commit succeeded. An error before `COMMIT`, or a busy, full or
    constraint error at `COMMIT`, keeps nothing. An I/O error from `COMMIT` (`SQLITE_IOERR`, not
    classified yet; 015 maps it) has an unknown outcome: the next open may recover that transaction,
    so the caller reads the state again. An error of
    `fn`'s own comes back unchanged; one that SQLite or the context caused comes back with its kind,
    wrapping the original.
  - A `Tx` is valid only inside `fn`. Opening a connection runs its PRAGMAs without a context,
    bounded by `busy_timeout`, and so does `Close`'s checkpoint: both wait at most 5 s.
- **Errors are classified once**, by SQLite's primary result code:
  `ErrBusy`, `ErrFull`, `ErrCannotOpen`, `ErrReadOnly`, `ErrCanceled`, `ErrConstraint`,
  `ErrCorrupt` (not a database, or a damaged page), and `ErrReplaced`, which is not SQLite's: the path no
  longer names the file `Open` opened. Each wraps the driver's error, so SQLite's own
  message is kept for diagnostics. Mapping to `AppError` belongs to 015.
- **Never reset a database.**
  - `Open` takes only an absolute, clean path (no `.`/`..`, no doubled separator, no NUL byte).
  - It creates the file itself, `0600` and with `O_EXCL`, only where nothing exists, so an existing file
    or a symlink's target is never created or truncated; SQLite gives `-wal` and `-shm` the same mode.
    It never deletes or recreates a file.
  - It refuses an empty or missing main file beside a WAL that holds commits (`ErrCorrupt`): SQLite
    would delete that WAL, and with it the commits.
  - `Write` and `Read` first check that the path still names the file `Open` opened (device and
    inode). Otherwise the writer would commit into a deleted inode and report success, and a new
    reader would find another file; both get `ErrReplaced`.
  - Every refusal leaves the file's bytes and inode unchanged: a file that is not a database, a
    read-only folder or file, a full disk, an empty file beside a WAL. Tests prove each case.
- **The data directory contract.** It is recorded here and in
  [architecture §6](../../product/APPLICATION_ARCHITECTURE.md#6-sqlite-and-restore). Its first reader,
  014 or 028, implements it.
  - **Default:** `~/Library/Application Support/dev.tsykvas.octopustorrent/library.sqlite`. The
    folder is named by the bundle ID.
  - **Override:** `OCTOPUS_DATA_DIR`. Go validates the folder: absolute and clean, symlinks resolved,
    a directory owned by the current user and not writable by group or others, not inside a torrent
    save folder. The application creates it `0700`. The library is
    `$OCTOPUS_DATA_DIR/library.sqlite`.
  - **Who sets the override:** `go tool devtool workspace run` sets it per workspace, the gate's
    `.app` run sets it to a temporary folder, and tests always use `t.TempDir()`. A workspace, a test
    or the gate therefore never opens the owner's library.
  - **What 008 does not do:** it gives the product no data directory. The product opens only
    `:memory:`, to report its SQLite version.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| `github.com/mattn/go-sqlite3` v1.14.52 | The common cgo driver | Compiles a C amalgamation: with the engine's crawshaw copy the link fails (`ld: 266 duplicate symbols`) | Cannot link into the product once 018 adds the engine |
| mattn with `-tags libsqlite3` | No duplicate symbols | Links Apple's system SQLite, whose version `go.mod` does not pin | Not reproducible (AI-07) |
| `github.com/ncruces/go-sqlite3` v0.35.6 | No cgo, SQLite 3.53.4, fewer new modules, issues `F_FULLFSYNC` | v0 API | Lost on rule 5 (stable v1 API) after passing rules 1–4 |
| `zombiezen.com/go/sqlite`, `crawshaw.io/sqlite` | Already in the engine's module graph | No `database/sql`; crawshaw last released in 2020 | Not candidates |
| Rollback journal (`DELETE`) | Simpler, one file | Readers block during a write | WAL measured: 4 readers made about 6 300 reads each during 300 commits, never busy |
| `synchronous=NORMAL`, no `fullfsync` | About 40 times faster commits | The last commits can be lost on power loss | SET-02 forbids losing committed changes; 4 ms per commit is acceptable for library writes |

## Verified grounds

[Report 008](../verification/008-sqlite-driver-durability-spike.md). Apple M4, macOS 27.0.1, Go 1.27.2,
`CGO_ENABLED=1`.

- **Probes of the three candidates**, each linked with `anacrolix/torrent/storage`:
  link, `otool -L`, `sqlite_version()`, PRAGMAs, checkpoints, cancel, `VACUUM INTO`, `F_FULLFSYNC`
  in the source and its measured cost.
- **`internal/library` tests on real SQLite** (`go test -race ./internal/library`), all `pass`:
  - PRAGMAs on all 5 connections;
  - version ≥ 3.51.3;
  - a failed commit is `ErrConstraint` and keeps nothing;
  - a cancelled read returns within 1 s and a cancelled write keeps nothing;
  - `ErrBusy` after the timeout, in-process and from **another process**;
  - readers never busy and never see half a transaction;
  - 300 commits during about 40 000 concurrent checkpoints;
  - **SIGKILL of a committing process keeps every acknowledged commit**;
  - a new database in a read-only folder → `ErrCannotOpen`, nothing created;
  - an existing database in a read-only folder → `ErrReadOnly` (`SQLITE_READONLY_DIRECTORY`);
  - a read-only WAL file opens, reads, and refuses the write with `ErrReadOnly`;
  - a read-only non-WAL file → `ErrReadOnly`;
  - **a real full volume** (8 MiB HFS+ disk image) → `ErrFull`, and the 20 committed rows survive
    with integrity `ok` and the same inode;
  - `max_page_count` → `ErrFull`;
  - not a database / a damaged page → `ErrCorrupt`, the file unchanged;
  - a path with a space, `?`, `#` and `%` opens that exact file.
- **Not executed: power-loss durability.** It rests on SQLite's documented `synchronous=FULL`
  behaviour and on `F_FULLFSYNC` being issued. The latter is read in the driver's source
  (`_full_fsync`) and supported by the 40× commit-cost difference.

## Consequences

- `go.mod`:
  - `modernc.org/sqlite` v1.60.1 becomes a direct requirement;
  - indirect modules move: `modernc.org/libc` v1.77.1, `memory` v1.12.1, `mathutil` v1.7.1,
    `mattn/go-isatty` v0.0.24, `dustin/go-humanize` v1.0.1, and a new `ncruces/go-strftime`
    v1.0.0. All are older than 14 days.
  - The product binary now links the driver.
- **Two SQLite engines in one process are expected** once 018 links the engine: crawshaw's for the
  engine's piece completion, unless 009 chooses otherwise, and modernc's for the library.
  - They must never open the same file.
  - crawshaw's 3.49.1 is within the WAL-reset range, but the engine opens its `.torrent.db` with
    one connection in the default rollback journal, which the bug does not affect. 009 keeps it
    that way or replaces it.
- **The gate's data-directory duty**, from [quality-gate](quality-gate.md#consequences), moves to the
  first reader of the data directory, 014 or 028. It now has a concrete form: `open --env
  OCTOPUS_DATA_DIR=<temporary folder>`, and the gate fails if the folder stays empty while the
  default folder changes.
- **`fullfsync` is a deliberate cost.** Bulk writes must batch rows into one transaction (014/015),
  not commit row by row.
- **Close may report `ErrBusy`** while another process reads the WAL, and `ErrFull` on a full disk.
  In both cases nothing is lost: the WAL stays and the next `Open` uses it.
- **016 builds backup on what 008 verified:**
  - `VACUUM INTO` gives a consistent copy that passes `integrity_check`;
  - the driver also exposes SQLite's online backup API (`NewBackup`, `NewRestore` on its connection).
  - Copying the active database file alone is never a backup.
- **Recheck on a driver update:** modernc's `tx.Commit` rolls the connection back when SQLite leaves
  a transaction open after a failed `COMMIT`; without that, one failed commit would break the single
  writer. `TestAFailedCommitIsAnErrorAndKeepsNothing` guards it (the next write must succeed). Also: `TestTheBundledSQLiteHasTheWALResetFix` fails if the bundled
  SQLite drops below 3.51.3. The `F_FULLFSYNC` source check and the commit-cost measurement in the
  report are repeated for a new major driver version.
