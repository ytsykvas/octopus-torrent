# Where the engine writes, what it remembers, and what the application believes

**Status:** `accepted`  
**Date:** 2026-10-17  
**Task:** [009](../../product/tasks/009-storage-and-completion-spike.md)  
**Register item:** O5 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `SET-02`, `TR-05`, `ADD-04`, `DEL-01`, `AI-05`

## Question

The adapter (018) and everything after it need to know four things that the engine's documentation
does not settle:

- Which storage puts the payload exactly in the folder the user chose?
- Where do the verified-piece marks live, and what happens when that store cannot be opened or written?
- After a restart or a crash, what may the application believe about the bytes on disk, so that valid
  data is not downloaded again and changed data is never shown as 100%?
- What does the engine do when the folder or the volume disappears?

The pinned engine is anacrolix/torrent v1.61.0 ([engine-lifecycle](engine-lifecycle.md)). The library uses
`modernc.org/sqlite` ([sqlite-driver](sqlite-driver.md)). The facts below were read in the pinned source,
and each one is asserted by a test, so an engine update that changes it turns a test red.

## Decision

### Storage

- **One storage per torrent**, made for the folder the user chose and given to the engine with
  `AddTorrentOpts.Storage`. It is the engine's `storage.NewFileOpts` with `ClientBaseDir = saveRoot`,
  **part files off** and our completion, inside a guard (below). Package
  `internal/torrentengine/filestore` is the prototype 018 builds on.
- **The engine client's default storage refuses** (`filestore.Refusing`), so a torrent added without a
  chosen folder writes nothing. `DataDir` is always set, to the application's data folder.
- **Add with `AddTorrentOpts{InfoHash, Storage}` and then `SetInfoBytes`.** `AddTorrentOpt` drops the
  storage error of the info it is given; `SetInfoBytes` returns it (finding 15). The engine opens a
  torrent's storage more than once per add.
- **Paths.** One rule, `filestore.Layout`, shared with the storage itself: `<saveRoot>/<name>` for a v1
  single-file torrent; `<saveRoot>/<name>/<path…>` for a v1 multi-file torrent; `<saveRoot>/<name>/<name>`
  for a single-file v2 or hybrid torrent; no pad files; a path that leaves `saveRoot` is an error. Tests
  prove the files on disk equal `Layout` for `v1-single`, `v1-tree`, `hybrid-single` and `hybrid-tree`, with
  the fixture's bytes, and that nothing else is written (no database, no `.part`, no `.pad`).

### File IO

- **The process must run with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic`.** The engine reads it in an `init`
  function, before any of our code. Its default, mmap, fails a chunk that spans a zero-length file
  ([006](controlled-swarm.md)) and never unmaps payload files ([007](engine-lifecycle.md)).
- `internal/torrentengine/classicio.Require` restarts the process with the variable set when it is not,
  and refuses any other value. It imports no engine package, so any `main` can call it first. **This
  replaces 007's rule "refuse to start when the variable is set".** The re-exec in the `.app` is
  provisional (018 proves it in the bundle); the recorded alternative is `LSEnvironment` in `Info.plist`.
  `swarm.MainClassicIO` uses it for tests.

### Piece completion

- **Our own store**, `<dataDir>/piece-completion.sqlite`, opened by `library.Open` (WAL, `synchronous=FULL`,
  `fullfsync`, identity checks). The engine's bolt and crawshaw stores are not used, and with the product
  build tags **`nosqlite,noboltdb`** neither is linked. `disable_libutp` removes the last cgo package of
  the engine; whether uTP stays is 018/026's decision.
- **Why not the engine's stores:**
  - their constructors fall back silently to an in-memory store when the database cannot be opened, so
    a restart forgets every piece (finding 19);
  - they put `.torrent.db` into the user's folder;
  - bbolt runs with `NoSync` and does not sync at `Close`; crawshaw opens in WAL, not in the rollback
    journal the [sqlite-driver ADR](sqlite-driver.md) assumed (that ADR is corrected).
- `filestore.Completion` implements `storage.PieceCompletion` over the library's `CompletionStore`
  (`LoadMarks`, `SaveMarks`):
  - A torrent's marks are read once; every `Get` is answered from memory.
  - `Set` changes memory at once and queues the row; one goroutine writes the queue in batches of at
    most 256 rows every 250 ms. A durable commit costs 4–11 ms whatever its size.
  - **A failed write is sticky.** `Err`, every later `Set` and `Close` return it, and nothing is written
    after it. The engine only logs a failed `Set`, so this is how the failure reaches the application.
  - An open failure is an error. There is no fallback.
  - `Close` flushes, waits and may be repeated. A call after `Close` returns `ErrClosed` and makes the
    session unclean.
  - **`Persistent()` is false.** Measured: with `true` the engine fsyncs the whole payload file for every
    completed piece, and a 64 MiB loopback transfer takes 1.0–1.2 s instead of 0.23 s. The payload is
    synced once at a clean quit, and a crash is verified anyway.
- A store of another version, or with other tables, is `ErrUnsupportedVersion` and never `ErrCorrupt`, so
  a "rebuild" of a damaged store (029) cannot destroy a newer library.

### The guard

`filestore.TorrentStorage` wraps the engine's storage. Before every operation it checks that the save folder
is still the one recorded at the start (device and inode) and refuses otherwise, without calling the engine:

- **Why:** classic IO opens the file on every chunk write with `O_CREATE` and, on `ENOENT`, creates the
  missing folders (finding 18). A deleted folder, or the mount point of a pulled volume, is recreated on
  the application's own disk and the download goes on. Worse, when the engine cannot read a file it
  marks the pieces of that file incomplete in our store.
- `Completion()` returns the error without delegating, so marks are not wiped.
- Every failure is recorded in the `Session`; a session with a problem is not a clean quit.
- After `Stop`, new operations are refused and those in flight are awaited. The engine's own `Close`
  does not wait for them.
- Piece reads are counted (`Reads`), which is how tests prove that trusted marks cause no hashing.
- A storage that offers more than `Piece` and `Close` is refused, so an engine update cannot add a path the
  guard does not cover.
- **A save folder on HFS+ is refused** (`ErrFileSystem`): classic IO seeks with `SEEK_DATA`, which HFS+
  lacks, so every piece fails its check and the download never completes (finding 21). FAT, exFAT and
  APFS work.

### The rehash rule

`files.Decide(record, saveRoot, pieces, probe)` (`internal/files`, no engine import) decides at startup,
per torrent, which marks to believe. A mark is never trusted on its own: the engine believes a mark over a
file of the right size without hashing it (finding 20).

1. No record, or the earlier run did not end cleanly (a crash, a storage problem, a late operation, a
   failed close or sync): **verify everything.**
2. The file system is not **APFS**, or is not the type recorded, or the save folder is on another device:
   **verify everything.** HFS+ keeps whole seconds, so a fingerprint taken right after a write proves
   nothing; exFAT makes up inodes and has no change time. A replugged volume is not a missing file.
3. A file the record lists as present that is gone: **`Missing`.** Nothing is added and nothing is
   downloaded; the owner is told and chooses a location (030). A file never written (a selective or
   partial download) is not missing.
4. A file whose fingerprint (size, mtime, ctime, inode) differs, that appeared, that is no regular file, or
   that cannot be read: **verify that file's pieces.**
5. Otherwise **trust.**

`Completion.Distrust` makes the pieces unknown, so the engine hashes exactly those and nothing else; a
verified piece clears it. A fingerprint is the file **as the run last left it**: taken after every write and
after the verification of a piece. A file the run never touched keeps the fingerprint the earlier run
left (`Baseline`). So an outside edit made while the application runs shows at the next start. The
limit that remains: an outside edit made during a run to a file that is still being written, between two
of the run's own writes, is not distinguished from them; the pieces of such a file are hashed when they
are next verified.

### Shutdown and start (018, 028 and 029 implement this order)

```
start  write "not clean" for the library record (029)  →  Decide  →  Completion.Distrust  →  Baseline
       →  AddTorrentOpt + SetInfoBytes  →  allow payload
quit   stop asking for data and let the writes in flight finish  →  Client.Close  →  TorrentStorage.Stop
       →  Completion.Close  →  TorrentStorage.Seal (syncs the written files, returns the record)
       →  store the record and "clean" in one library transaction
```

The "not clean" write at start is what makes a crash visible: a killed process never reaches the last
step. 009 proves the rule with the record kept in memory; persisting it is 014/029's.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| The engine's default storage and completion (`storage.NewFile`) | No code | Falls back to memory silently, database in the user's folder, part files rename the user's files (finding 13), mmap IO | Breaks "no hidden fallback" and DEL-01 |
| The engine's bolt completion | Small, no cgo | `NoSync`, no sync at `Close`, the same silent fallback in the constructors | Marks are not durable at a clean quit |
| The engine's crawshaw completion | Default under cgo | A second C SQLite (3.49.1) in WAL, in the user's folder | Contradicts the SQLite decision; no explicit failure |
| Per-row durable commits in our store | Simplest | 4–11 ms per mark on the hasher goroutine; one writer for all torrents caps marks near 250 per second | A verify of a large torrent would be minutes of commits |
| `Persistent()` true | The engine syncs data with each mark | 4–5 times slower transfer (measured) | A crash is verified anyway |
| mmap file IO | The engine's default | Fails over zero-length files, never unmaps | Not usable (006, 007) |
| Part files on | Completed files are told by name | Renames the user's files, makes them read-only, overwrites a file at a torrent path | The owner chose them off |
| Trust marks after a clean quit alone | Simple | Misses an outside edit during the run and a same-size rewrite | Criterion: modified files are never shown as verified |
| Trust HFS+ with a settle wait at quit | HFS+ volumes keep fast restarts | Waits up to 2 s at every quit; HFS+ cannot hash with the pinned engine anyway | Refused instead |

## Verified grounds

[Report 009](../verification/009-storage-and-completion-spike.md). Apple M4, macOS 27.0.1, Go 1.27.2,
anacrolix/torrent v1.61.0, classic IO, closed loopback swarm, `-race`.

- **Pass:** placement for four formats; a torrent without storage refused; a save folder deleted or
  replaced mid-transfer (nothing recreated, marks intact, record unclean); the run closed mid-transfer
  three times (no mark without its bytes); clean restart (0 pieces read, 0 payload bytes); partial resume
  (exactly the completed pieces, 0 reads, only the missing bytes downloaded); a process killed with
  SIGKILL (everything verified, nothing valid downloaded again); truncated, same-size rewritten, mtime
  restored and replaced files (only that file's pieces read); a moved file reported missing; an edit made
  while the application runs; FAT, exFAT and APFS volumes; a replugged APFS volume; refused mark writes
  (reported, engine goes on, next start verifies); a full volume at open; a volume pulled while leeching
  and while seeding (nothing written on the internal disk, marks intact).
- **Findings 15–22** (numbered after 007's 14) are in the report. In short: 15 `AddTorrentOpt` drops the
  storage error; 16 a symlink at the torrent path is followed; 17 classic `WriteTo` returns
  `io.ErrShortWrite` for a piece inside its file; 18 classic IO recreates a deleted save folder; 19 the
  engine's constructors forget every piece silently; 20 a mark is believed over a same-size rewrite; 21
  HFS+ cannot be hashed on; 22 a fresh transfer sometimes stands still (4.3% with classic IO and 1.7% with
  mmap under `-race`, none without).
- **Not proven:** power loss (the rule treats any unclean end as unverified; the flush order cannot be
  forced here); the `.app` restart, crash and quit (`not_run`, 029 and 052); shutdown with discovery on
  (018); the re-exec of `classicio` inside the bundle (018); that the "not clean" marker is written before
  the first payload write (029).

## Consequences

- **018** builds the adapter on `filestore`: adds with `SetInfoBytes`, calls `classicio.Require` first in
  `main`, builds with `nosqlite,noboltdb`, makes those tags a gate step, and keeps the shutdown order
  above. The `GOPPROF` refusal stays.
- **020** refuses a save folder on HFS+ before an import is confirmed, refuses a path whose torrent folder is
  a symlink (finding 16), and checks every torrent path for collisions before the add (findings 8, 13).
- **023/028** persist the record and the "not clean" marker with the library record.
- **029** applies `Decide` at startup and reports `Missing` to 030; an unreadable marks store is an explicit
  error whose rebuild is a separate action.
- **030** receives `Missing`. **031/032** include `.part` names in deletion lists (finding 13), and the data
  folder's marks of a removed torrent are removed with its record.
- **010** owns the product remedy for finding 22; the test helper `keepMoving` is not product code.
- **014** inherits `piece_completion` (`user_version` 1) as a schema that precedes its migration policy.
- An engine update re-runs `internal/torrentengine/...`; every finding test that turns red is decided again,
  and this ADR is updated.
