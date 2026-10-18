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
`modernc.org/sqlite` ([sqlite-driver](sqlite-driver.md)). The facts were read in the pinned source; the ones
marked "test" below are also asserted by a test, so an engine update that changes them turns it red, and
the ones marked "source" were read and not executed.

## Decision

### Storage

- **One storage per torrent**, made for the folder the user chose and given to the engine with
  `AddTorrentOpts.Storage`. It is the engine's `storage.NewFileOpts` with `ClientBaseDir = saveRoot`,
  **part files off** and our completion, inside a guard (below). Package
  `internal/torrentengine/filestore` is the prototype 018 builds on. A storage serves one torrent.
- **The engine client's default storage refuses** (`filestore.Refusing`), so a torrent added without a
  chosen folder writes nothing. `DataDir` is always set, to the application's data folder.
- **Add with `AddTorrentOpts{InfoHash, Storage}` and then `SetInfoBytes`.** `AddTorrentOpt` drops the
  storage error: with the info in its options it neither sets the info nor reports anything (test,
  finding 15). `SetInfoBytes` returns the error. How often the engine opens a storage is not something to
  count on: once when it works, twice when it refuses (test).
- **Paths.** One rule, `filestore.Layout`, shared with the storage itself: `<saveRoot>/<name>` for a v1
  single-file torrent; `<saveRoot>/<name>/<path…>` for a v1 multi-file torrent; `<saveRoot>/<name>/<name>`
  for a single-file v2 or hybrid torrent; no pad files. A path that leaves `saveRoot`, and a torrent
  whose name makes its folder the save folder itself (`""`, `.`, `a/..`), are errors. Tests prove the
  files on disk equal `Layout` for `v1-single`, `v1-tree`, `hybrid-single` and `hybrid-tree`, with the
  fixture's bytes, and that nothing else is written (no database, no `.part`, no `.pad`). The engine's
  other constructors place files differently (`NewFileByInfoHash` adds an infohash folder, `.part` names
  and a `.torrent.db` in the user's folder); a test records it.

### File IO

- **The process must run with `TORRENT_STORAGE_DEFAULT_FILE_IO=classic`.** The engine reads it in an `init`
  function, before any of our code. Its default, mmap, fails a chunk that spans a zero-length file
  ([006](controlled-swarm.md)) and never unmaps payload files ([007](engine-lifecycle.md)).
- `internal/torrentengine/classicio.Require` restarts the process with the variable set when it is not,
  adds a marker so that a launcher that drops the variable cannot make it restart for ever, and refuses any
  other value. It imports no engine package, so any `main` can call it first. **This replaces 007's rule
  "refuse to start when the variable is set".** The re-exec in the `.app` is provisional (018 proves it in the
  bundle); the recorded alternative is `LSEnvironment`, and `Require` stays as the backstop for `wails dev`
  and a terminal launch. The engine's `init` functions have run before `Require` does: the refusal of
  `GOPPROF` must be the first statement of 018's `main`, before `Require`. `swarm.MainClassicIO` uses it for
  tests.

### Piece completion

- **Our own store**, `<dataDir>/piece-completion.sqlite`, opened by `library.Open` (WAL, `synchronous=FULL`,
  `fullfsync`, identity checks). The engine's bolt and crawshaw stores are not used, and with the product
  build tags **`nosqlite,noboltdb`** neither is linked (checked on `filestore` and its tests). `disable_libutp`
  removes the last cgo package of the engine; whether uTP stays is 018/026's decision.
- **Why not the engine's stores:**
  - their constructors fall back silently to an in-memory store when the database cannot be opened, so
    a restart forgets every piece (test, finding 19);
  - they put `.torrent.db` into the user's folder (test);
  - bbolt runs with `NoSync` and does not sync at `Close` (source); crawshaw opens in WAL, not in the
    rollback journal the [sqlite-driver ADR](sqlite-driver.md) assumed (source; that ADR is corrected).
- `filestore.Completion` implements `storage.PieceCompletion` over the library's `CompletionStore`
  (`LoadMarks`, `SaveMarks`):
  - A torrent's marks are read once, with no lock held, so a slow read holds up no other torrent; every `Get`
    is answered from memory. A failed read is the same error for every later call.
  - `Set` changes memory at once and queues the row; one goroutine writes the queue in batches of at
    most 256 rows every 250 ms. A durable commit costs 4–11 ms whatever its size.
  - **A failed write is sticky.** `Err`, every later `Set` and `Close` return it, and nothing is written
    after it. The engine only logs a failed `Set`, so this is how the failure reaches the application.
  - An open failure is an error. There is no fallback.
  - `Close` flushes, waits and may be repeated. A call after `Close` returns `ErrClosed` and makes the
    session unclean. What a crash can lose is what was queued (under 250 ms or 256 rows) and the batch being
    written; that is irrelevant, because after a crash the marks are not read.
  - **`Persistent()` is false.** Measured: with `true` the engine fsyncs the whole payload file for every
    completed piece, and a 64 MiB loopback transfer takes 1.0–1.2 s instead of 0.23 s. The payload is
    synced once at a clean quit, and a crash is verified anyway.
  - An unreadable marks store disallows the engine's download (test, finding 23): the failure is explicit
    and the transfer does not start.
- A store of another version, or with other tables, is `ErrUnsupportedVersion` and never `ErrCorrupt`, so
  a "rebuild" of a damaged store (029) cannot destroy a newer library. `Open` converts a foreign database
  that is in the rollback journal to WAL before it looks at the schema (008's behaviour); the refusal
  leaves a WAL database unchanged, and a foreign non-WAL database with its journal mode changed.
- `internal/torrentengine/filestore` imports `internal/library` for this store. No SQL is written there.

### The guard

`filestore.TorrentStorage` wraps the engine's storage. Before every operation it checks that the save folder
is still the one recorded at the start (device, inode **and creation time**) and refuses otherwise, without
calling the engine:

- **Why:** classic IO opens the file on every chunk write with `O_CREATE` and, on `ENOENT`, creates the
  missing folders (test, finding 18). A deleted folder, or the user-writable mount point of a pulled
  volume, is recreated on the application's own disk and the download goes on. (`/Volumes` is not
  writable for the user, so a normal mount point is not recreated; a folder the user deleted is.)
  Worse, when the engine cannot read a file it marks the pieces of that file incomplete in our store.
  The creation time is there because two volumes mounted one after the other at the same place can have
  the same device and inode numbers (every APFS root is inode 2).
- `Completion()` returns the error without delegating, so marks are not wiped.
- **The window.** The check and the engine's own open are two steps. If the folder disappears between
  them, one chunk write or one folder creation can still reach the old path. The guard narrows the window
  to one operation; it cannot close it. Opening files relative to a held directory descriptor would, and
  the engine does not offer it.
- A torrent whose path leads through a **symlink**, or whose zero-length file sits on an existing file
  (the engine truncates it when it opens the torrent), is refused with `ErrUnsafePath` on every open
  (tests, findings 8 and 16). The same race applies: a symlink put in place after the check is not seen.
- Every failure is recorded in the `Session` (the first 100 are kept, the rest counted); a session with a
  problem is not a clean quit.
- **`Stop`** refuses new operations and waits for those in flight. It does not wait while holding the lock the
  operations need, so a stalled volume cannot make it hang past its context. If its context ends first, the
  operations still running make the session unclean and `Seal` refuses (`ErrNotStopped`) until they have
  finished. The engine's own `Close` does not wait for storage calls.
- Piece reads are counted (`Reads`), which is how tests prove that trusted marks cause no hashing.
- A storage that offers more than `Piece` and `Close` is refused, so an engine update cannot add a path the
  guard does not cover.
- **A save folder on HFS+ is refused** (`ErrFileSystem`): classic IO seeks with `SEEK_DATA`, which HFS+
  lacks, so every piece fails its check and the download never completes (test, finding 21). FAT, exFAT and
  APFS work.
- The engine's file storage logs absolute paths. `NewTorrentStorage` discards its log unless the caller
  passes a logger; 017 decides what may be logged. Errors that carry paths (`ErrRootChanged`,
  `ErrUnsafePath`, `Decision.Reason`) are for the UI and must not be stored or logged by 018.

### The rehash rule

`files.Decide(record, saveRoot, expected, pieces, probe)` (`internal/files`, no engine import) decides at
startup, per torrent, which marks to believe. `expected` is the torrent's own layout (`filestore.Ranges`:
paths and piece ranges), so a record never decides what is verified. A mark is never trusted on its own: the
engine believes a mark over a file of the right size without hashing it (test, finding 20).

1. A file the record lists as present that is gone is **`Missing`**, whatever else is true, also after a
   crash. Nothing is added and nothing is downloaded in its place; the owner is told and chooses a
   location (030). A file never written (a selective or partial download) is not missing.
2. No record, or the earlier run did not end cleanly (a crash, a storage problem, a late operation, a
   failed close or sync): **verify everything.**
3. A record that lists a file the torrent does not have is another torrent's: **verify everything.**
4. The file system is not **APFS**, or is not the type recorded, or the save folder is on another device:
   **verify everything.** HFS+ keeps whole seconds, so a fingerprint taken right after a write proves
   nothing; exFAT makes up inodes and has no change time. A replugged volume is not a missing file.
5. A file whose fingerprint (size, mtime, ctime, inode) differs, that the record does not know or says was
   absent, that is no regular file, or that cannot be read: **verify that file's pieces.**
6. Otherwise **trust.**

`Completion.Distrust` makes the pieces unknown, so the engine hashes exactly those and nothing else; its
verdict clears it. A fingerprint is the file **as the run last left it**: taken after every write and after the
verification of a piece. A file the run never touched keeps the fingerprint the earlier run left
(`Baseline`, which also keeps a file reported missing, so that it is reported again).

What keeps a modified file from being blessed:
- **A file with a piece still waiting to be verified** (distrusted, no verdict yet) is recorded as absent by
  `Seal`, so the next start verifies it. Its marks in the store are stale and must not be believed. (A clean
  quit in the middle of the verification after a crash would otherwise have trusted them.)
- **A file changed by someone else between the run's own writes** is tainted and recorded as absent: before
  a write, the file is compared with what the run left, and a difference with no other write to the file
  running is foreign.

Limits that remain, recorded and not hidden:
- An outside edit to a file the run has not written yet and the earlier run left trusted is caught at the
  first write to it, or at the next start; until then the file shows as complete.
- While writes to one file overlap, the comparison is skipped, so an outside edit in that time is taken for
  the run's own.
- An edit to a completed file that the run never touches again stays "complete" for the rest of that run; the
  next start catches it.
- Silent corruption of the bytes by the disk (bit rot) is never caught by a trusted fingerprint. Only a
  verification of the file would find it.
- Power loss: the rule treats any unclean end as unverified, but the order in which the system writes data and
  marks cannot be forced or tested here.

### Shutdown and start (018, 028 and 029 implement this order)

```
start  write "not clean" for the library record (029)  →  Decide  →  Completion.Distrust  →  Baseline
       →  AddTorrentOpt + SetInfoBytes  →  allow payload
quit   stop asking for data, wait until the writes in flight have landed  →  Client.Close
       →  TorrentStorage.Stop  →  Completion.Close  →  TorrentStorage.Seal (syncs the written files)
       →  store the record and "clean" in one library transaction
```

- **The first quit step is an open primitive.** The engine gives no signal that the writes in flight have
  landed. The tests disallow download and wait until no payload has arrived for 200 ms. A late operation
  only makes the run unclean (every piece is then hashed at the next start); it never makes it unsafe.
  018 and 028 own a better condition (for example, no guard operation in flight and no piece queued, hashing
  or marking within a bounded wait).
- `Seal` refuses before `Stop` has drained (`ErrNotStopped`) and before the marks are closed
  (`ErrNotClosed`). It fsyncs every written file one by one; the quit time for a torrent with many files is
  not measured (028).
- The "not clean" write at start is what makes a crash visible: a killed process never reaches the last
  step. 009 proves the rule with the record kept in memory; persisting it is 014/028/029's.

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
| Open files relative to a held directory descriptor | Closes the window between check and open | The engine does not offer it | The window is narrowed to one operation and documented |

## Verified grounds

[Report 009](../verification/009-storage-and-completion-spike.md). Apple M4, macOS 27.0.1, Go 1.27.2,
anacrolix/torrent v1.61.0, classic IO, closed loopback swarm, `-race`.

- **Pass (tests on the real engine, real disks and real volumes):** placement for four formats; a torrent
  without storage refused; a symlink or a user's file in the way refused; a save folder deleted or replaced
  mid-transfer (nothing recreated, marks intact, record unclean); the run closed mid-transfer three times (no
  mark without its bytes); clean restart (0 pieces read, 0 payload bytes); partial resume (exactly the
  completed pieces, 0 reads, only the missing bytes downloaded); a process killed with SIGKILL (everything
  verified, nothing valid downloaded again); truncated, same-size rewritten, mtime restored and replaced files
  (only that file's pieces read); a moved file reported missing, also after a crash; an edit made while the
  application runs; FAT, exFAT and APFS volumes; a replugged APFS volume; refused mark writes (reported, engine
  goes on, next start verifies); a full volume at open; a volume pulled while leeching and while seeding
  (nothing written on the internal disk, marks intact). In every failure scenario a stand-in library
  database in the data folder is byte for byte unchanged and opens.
- **Mutation checks:** with the save-folder check removed, the pulled-volume, vanished-folder and
  volume-swap tests fail; with the unverified-piece rule of `Seal` removed, its test fails and shows the
  file trusted.
- **Findings 15–24** (numbered after 007's 14) are in the report; each has a test except where marked
  "source": 15 `AddTorrentOpt` drops the storage error; 16 a symlink at the torrent path is followed; 17
  classic `WriteTo` returns `io.ErrShortWrite` for a piece inside its file; 18 classic IO recreates a deleted
  save folder; 19 the engine's constructors forget every piece silently, put a database in the user's folder
  and place files differently; 20 a mark is believed over a same-size rewrite; 21 HFS+ cannot be hashed on; 22
  a fresh transfer sometimes stands still (measured with temporary scratch tests that were deleted: 4.3%
  with classic IO and 1.7% with mmap under `-race`, none without; cause and remedy in 010: a lost writer wake-up, bounded by a 1 s keep-alive); 23 an unreadable completion disallows download; 24 with part files off the engine still warns
  about a missing `.part` file, naming the local path.
- **Source only, not executed:** bbolt `NoSync`; crawshaw opening in WAL; `Client.Close` not waiting for
  storage calls; `Set(true)` running before the payload flush.
- **Not proven:** power loss; the `.app` restart, crash and quit (`not_run`, 029 and 052); shutdown with
  discovery on (018); the re-exec of `classicio` inside the bundle (018); that the "not clean" marker is
  written before the first payload write (029); the quit time with many files (028).

## Consequences

- **018** builds the adapter on `filestore`: adds with `SetInfoBytes`, refuses `GOPPROF` and then calls
  `classicio.Require` first in `main`, builds with `nosqlite,noboltdb`, makes those tags a gate step, and
  keeps the shutdown order above. It owns the quit drain.
- **020** refuses a save folder on HFS+ before an import is confirmed, checks every torrent path for
  collisions with the user's files before the add (findings 8, 13), and handles the guard's refusals
  (`ErrUnsafePath`, `ErrRootChanged`) as messages, never as stored text.
- **023/028** persist the record and the "not clean" marker with the library record.
- **029** applies `Decide` at startup and reports `Missing` to 030; an unreadable marks store is an explicit
  error whose rebuild is a separate action.
- **030** receives `Missing`. **031/032** include `.part` names in deletion lists (finding 13), and the marks of
  a removed torrent are removed with its record (`DeleteMarks` is not built in 009).
- **010** found the cause of finding 22 (a lost writer wake-up) and its remedy: every client sets
  `KeepAliveTimeout = transferswitch.WakeInterval` (1 s); the helper `keepMoving` is gone and the harness peers
  use the interval ([global-upload-stop](global-upload-stop.md)).
- **014** inherits `piece_completion` (`user_version` 1) as a schema that precedes its migration policy.
- An engine update re-runs `internal/torrentengine/...`; every finding test that turns red is decided again,
  and this ADR is updated.
