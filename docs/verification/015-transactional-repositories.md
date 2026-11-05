# Report 015: repositories and transactional writes

**Task:** [015](../../product/tasks/015-transactional-repositories.md). **Requirements:** `STACK-04`, `SET-02`, `DATA-01`. **Branch:** `task/015-transactional-repositories` (stacked on `task/014-sqlite-schema`, PR #20).

## Session architecture plan

Approved by the owner on 2026-11-04, after an adversarial review by an architect agent (14 findings,
listed in the self-assessment at the end).

### Context

Task [015](../../product/tasks/015-transactional-repositories.md) (P2, critical path), requirements
`STACK-04` (SQLite only through the repository; a write error is never a success), `SET-02` (records,
hashes, metainfo/magnet, paths, wanted, manual pause, settings stored locally), `DATA-01`. Hard
dependencies 013 and 014 are `done` on this branch stack: PRs #18 → #19 → #20 are still open, and the
new branch `task/015-transactional-repositories` starts from `task/014-sqlite-schema`. No open
register item is owned by 015.

Today `internal/library` has schema 1 and `Library` exposes only `Close`; nothing writes a torrent,
a file selection, the pause, the settings or the journal. The SQL runner (`Store`, `Tx`) is exported
(unreachable from outside since 014, but nothing enforces it). The error kinds have no `AppError`
codes, an I/O error from `COMMIT` is unclassified, and a failed `COMMIT` that races a cancel is
reported as `ErrCanceled` even though `COMMIT` ran.

**For the owner:** nothing changes in the window. 015 gives 021, 023, 024, 025, 026, 029, 031, 050
and 053 typed, transactional ways to store and read the library, and the error codes the UI will
show for a library that is busy, full, read-only, gone, damaged, newer, not a library, failed to
migrate, or whose last save has an unknown outcome.

**Out of scope:**
- payload records: 029 writes `clean = 0` before the first payload write, and no row already means
  "never ran, verify everything";
- removing a record (031); reserving an identity before the metadata exists (021); tags, rules and
  the watch folder (056+);
- facade methods and `main.go` wiring: the services that call the repositories come in 023, 024,
  025, 034 and 050, and `system.openLibrary` stays an `io.Closer` until a facade dependency needs
  `*library.Library`;
- logging a failed operation's cause (017); persisting the in-memory ledger.

No new dependency, no `.npmrc` change, no binding change, no migration.

### Boundaries and contracts

- **Repository (`internal/library`).**
  - The repositories are methods of `Library`. Each one is **one transaction over values prepared in
    memory**, with no callback, so no caller can wait for the engine or the network inside a
    transaction (008's rule, made structural). Each returns nil only after a durable commit.
  - It imports `internal/app/contracts` for identifiers, enumerations, `Settings`, `Request` and
    `AppError`, as `commandorder` and `transfer` already do: one vocabulary, no mapping layer.
    `contracts` imports only the standard library, so there is no cycle.
- **The SQL runner becomes unexported** (gopls rename `Store` → `store`, `Tx` → `txn`; a pure
  refactor), so SQL outside `internal/library` cannot compile.
- **The gate's `product isolation` step** also fails:
  - when any own package other than `internal/library` (its test files included) directly imports
    `database/sql…` or a path containing `sqlite`, under each build mode (default, `dev`,
    `bindings`);
  - when a frontend dependency's name contains `sql`.

  Together these cover the criterion "React and the engine adapter do not execute SQL".
- **Contracts (`internal/app/contracts`):**
  - 11 new error codes;
  - `AppError` gets an unexported cause (`WithCause`, `Unwrap`) that is never marshalled;
  - `Settings.Validate()` is exported (the existing check);
  - `checkFolderPath` is exported as `CheckFolderPath`;
  - `Valid()` on `ImportSource` and `OperationKind`.
- **Facade (`internal/app`).** `ErrorFormatter` also writes a coded error's cause to diagnostics,
  quoted like the uncoded line already is.
- **Frontend.** Only the 11 English fallbacks in `bridge/contract/errorMessages.ts`.

### Design

#### 1. The commit outcome rule: writes only (`store.go`)

Verified in the pinned sources:
- `database/sql` `Tx.Commit` returns the context's error or `ErrTxDone` *before* its compare-and-swap,
  without running `COMMIT` (`sql.go:2292-2306`); `awaitDone` races on the same CAS (`sql.go:2214-2227`).
- After the CAS, the driver's error comes back unchanged.
- modernc commits under `context.Background()` and rolls back the transaction itself if SQLite left
  it open (`tx.go:35-54`).

`store.Write` (not `run`; `Read` and `prepare` keep today's classification) classifies a failed
commit like this:

| `Commit` returned | Kinds | Outcome |
|---|---|---|
| a ctx error or `ErrTxDone` | `ErrCanceled` | `COMMIT` never ran; nothing kept |
| busy, full or constraint | its kind | rolled back; nothing kept |
| anything else (I/O, corrupt, …) | its kind + `ErrOutcomeUnknown` | durable or not, **unknown until the next open** (SQLite may recover it from the WAL then); 029 reconciles at start |

This replaces 008's "a cancel plus a failed commit is `ErrCanceled`", which reported as cancelled a
commit that ran. The migration keeps `ErrMigrationFailed` for any commit error. The ADRs gain one
precision: an I/O error at the migration's `COMMIT` is also of unknown outcome, and it heals itself,
because the next open examines the version again. New kind: `ErrIO` (`SQLITE_IOERR`).

**Test seam:** `settings.beforeCommit func()`, a no-op by default, which `store.Write` runs after
`fn` succeeded and right before `Commit`. Tests use it to cancel, to fill the disk or to cap the
file size at exactly that point (work order 5: controlled error injection).

#### 2. Kinds (`errors.go`)

New kinds:
- `ErrIO`, `ErrOutcomeUnknown`;
- `ErrInvalid`: a value the caller should have validated; a bug, never a user path;
- `ErrNotFound`, as `*NotFoundError{Torrent}`;
- `ErrDuplicate`, as `*DuplicateError{Existing contracts.TorrentID}`;
- `ErrOperationExists`, as `*OperationExistsError{ID, SameRequest bool}`.

#### 3. Repositories

Every method is one transaction.

| Method | What it does | Errors |
|---|---|---|
| `AddTorrent(ctx, Torrent, OperationIntent)` | One lookup query under the write lock: the record with its v1 hash, else with its v2 hash, and the operation's stored fingerprint. Then the inserts: `torrents`, `torrent_files`, `operations` (state `running`, the fingerprint computed with `contracts.Fingerprint`, the request as its JSON) and `operation_torrents(op, id)`. This is the import's record, identities, file selection and operation intent as one change (work order 2). | `ErrDuplicate` naming that record; `ErrOperationExists` (`SameRequest` when the fingerprint is equal); `ErrInvalid` |
| `SetManualPaused(ctx, id, paused)` | `UPDATE torrents` | `ErrNotFound` |
| `SetFilesWanted(ctx, id, indexes, wanted)` | `UPDATE torrent_files … WHERE file_index IN (SELECT value FROM json_each(?))` (no limit of 32766 parameters). Fewer rows than distinct indexes rolls back. | `ErrNotFound` for the torrent; `ErrInvalid` for an index it lacks (024's service checks the count first) |
| `SaveSettings(ctx, contracts.Settings)` | Upsert of row 1, including the global upload stop and the limits | `ErrInvalid` (`Settings.Validate`) |
| `FinishOperation(ctx, id, OperationEnd)` | Sets `state`, `error` and `finished_at` only `WHERE finished_at IS NULL`, plus each `operation_torrents.outcome` | `ErrInvalid` (unknown or already finished operation; not a final state; error ⇔ failed/cancelled broken; an outcome for a torrent it does not name; an error without a code) |
| `Torrents(ctx)` | Every record with its files, in one snapshot, ordered by `added_at`, `id` | — |
| `Settings(ctx)` | `(contracts.Settings, found, error)`; no row means the caller's defaults (SET-06) | — |
| `Operation(ctx, id)` | `(Operation, found, error)`, with its torrents and outcomes | — |

Types:
- `Torrent{ID, InfohashV1 *[20]byte, InfohashV2 *[32]byte, Name, Info []byte, Source, SourceMetainfo
  []byte, SourceMagnet string, SavePath, ManualPaused, AddedAt time.Time, Files []TorrentFile}`. A
  file's index is its position, so indexes are contiguous by construction.
- `TorrentFile{Path, Size int64, Wanted}`.
- `OperationIntent{ID, RuntimeID, Kind, Request contracts.Request, AcceptedAt}`.
- `Operation{ID, RuntimeID, Kind, Fingerprint, Request json.RawMessage, State, Error *AppError,
  AcceptedAt, FinishedAt, Torrents []OperationTorrent{ID, Outcome *AppError}}`.
- `OperationEnd{State, Error, Outcomes map[TorrentID]*AppError, FinishedAt}`.

**Validation** (`ErrInvalid`, nothing written). Every value a schema `CHECK` sees is checked in Go
first, so a constraint error stays a bug:
- UUIDs, with `contracts.IsUUID`;
- at least one hash, at least one file, a non-empty `Info`, `Source.Valid()`;
- `SavePath` with `contracts.CheckFolderPath` (absolute, clean, no control characters, ≤ 1024 bytes);
- file paths non-empty, without a NUL (SQLite's `length()` stops at a NUL) and unique; sizes ≥ 0;
- `Kind.Valid()` (the owner's Go-only kind check);
- every time storable exactly as Unix nanoseconds (years 1678–2262).

The fingerprint and the JSON are produced by the repository from typed values, so they are always
valid and shallow: no `json.Valid`, no depth check.

**Encoding:**
- hashes as BLOB;
- JSON (`request`, `error`, `outcome`) bound as a **string**: a `[]byte` binds as a BLOB, which a
  STRICT `TEXT` column refuses (modernc `conn.go:521`);
- an empty `SourceMetainfo` and an empty `SourceMagnet` as an explicit nil (NULL): modernc binds
  `[]byte{}` as a zero-length BLOB (`conn.go:1366`);
- times as `UnixNano`, read back in UTC;
- `uint64` limits as their int64 bit pattern;
- a journal `AppError` without its cause, with `{}` for nil params: no paths or SQLite messages are
  stored in the library.

**Code shape that keeps every error branch reachable:**
- one `execAll(statements)` loop serves all inserts and updates;
- one `queryEach(scan, query, args)` serves the reads, and ends with `return rows.Err()`, which is
  not a branch;
- each method runs one lookup `QueryRow` at most;
- `RowsAffected` is read with `n, _ :=`, because modernc's never fails (`result.go:36-42`, noted in a
  comment).

#### 4. One mapping to `AppError` (`problem.go`)

`library.Problem(err) *contracts.AppError` returns nil for nil and attaches the cause. It is for the
library only: the marks store's errors are 018/028's. The first match wins:

| Kind | Code |
|---|---|
| `ErrOutcomeUnknown` | `library.outcomeUnknown` |
| `ErrMigrationFailed` | `library.migrationFailed` |
| `ErrUnsupportedVersion` | `library.newerVersion` |
| `ErrUnknownSchema` | `library.notALibrary` |
| `ErrCanceled` | `operation.cancelled` |
| `ErrBusy` | `library.busy` |
| `ErrFull` | `library.diskFull` |
| `ErrReadOnly` | `library.readOnly` |
| `ErrCorrupt` | `library.damaged` |
| `ErrCannotOpen`, `ErrReplaced`, `ErrIO` | `library.unavailable` |
| `ErrDuplicate` | `torrent.duplicate`, with `TorrentID` = the existing record |
| `ErrOperationExists`, a different request | `operation.idConflict` |
| `ErrNotFound` | `torrent.notFound`, with `TorrentID` set |
| anything else: `ErrInvalid`, `ErrConstraint`, a repeated identical request, a write after `Close` | `internal.unexpected` |

A repeated identical request reaches `Problem` only when a service failed to answer it from the
journal first. That is 023's job, per api-contract: same ID and same fingerprint return the stored
status.

#### 5. The two stages stay separate (work order 3)

The repository answers only for the commit. Applying the policy to the engine is the service's next
step under the torrent's key (013: `CheckIntentChange` → repository → registry → `Reconcile`):
- a failed commit is the operation's `AppError` (through `Problem`) and never reaches the registry;
- a failed apply is the registry's recorded outcome (`View.Problem`).

The ADR documents this; 023 and 025 implement it.

### Files

| File | Change |
|---|---|
| `internal/library/store.go` | edit: rename; `Write`'s commit rule and `beforeCommit` |
| `internal/library/errors.go`, `errors_test.go` | edit: new kinds, `SQLITE_IOERR` |
| `internal/library/repository.go` | new: `execAll`, `queryEach`, the encoders (times, unsigned values, nullable values, JSON) |
| `internal/library/torrents.go`, `torrents_test.go` | new: `Torrent`, `AddTorrent`, `Torrents`, `SetManualPaused`, `SetFilesWanted` |
| `internal/library/library_settings.go`, `library_settings_test.go` | new: `Settings`, `SaveSettings` |
| `internal/library/operations.go`, `operations_test.go` | new: the journal types, `FinishOperation`, `Operation`, the intent insert |
| `internal/library/problem.go`, `problem_test.go` | new |
| `internal/library/repository_test.go` | new: concurrency, reopen, failure injection, two libraries on one file, behind-the-store changes |
| `internal/library/*_test.go`, `completion.go`, `schema.go`, `library.go`, `doc.go` | edit: the rename; `doc.go` names the repositories |
| `internal/app/contracts/errors.go`, `enums.go`, `settings.go`, `requests.go`, `values_test.go`, `testdata/vocabulary.json` | edit |
| `internal/app/app.go`, `app_test.go` | edit: `ErrorFormatter` writes a coded error's cause |
| `frontend/src/bridge/contract/errorMessages.ts` | edit: 11 fallbacks |
| `internal/devtool/isolation.go`, `isolation_test.go` (+ fakes in `check_test.go`) | edit: the SQL rule inside `product isolation` |
| `docs/decisions/library-repositories.md` | new ADR |
| `docs/decisions/api-contract.md`, `sqlite-driver.md`, `library-schema-and-data-folder.md` | edit: codes; the commit rule and the unexported runner; the migration-commit precision |
| `docs/app/library-repositories.md` | new feature doc |
| `docs/app/library-store.md`, `facade.md`, `docs/tooling/quality-gate.md`, `docs/README.md`, `product/APPLICATION_ARCHITECTURE.md` §3/§6, `README.md` | edit |
| `docs/verification/015-transactional-repositories.md` | new |
| `product/tasks/015-*.md` | edit: status and result; `plan-nav` |
| "From 015" notes in 017, 021, 023, 024, 025, 026, 028, 029, 031, 050, 053 | edit. 023's note: `ErrDuplicate` replaces 014's `ErrConstraint`; no payload row at import; answer a repeat from the journal |

`CLAUDE.md` (`library.Tx` and `Store` in its big-picture section) changes only through
`/update-rules`, with the owner's confirmation at `/pushit`.

### Tests first (RED → GREEN), in order

**Step 0. Pure refactor, no RED.** The rename, with `go test -race ./internal/library` green before
and after it.

**Step 1. Contracts.**
- Tests first:
  - `values_test.go`: `WithCause`/`Unwrap`, and no cause in the JSON; `Settings.Validate`;
    `CheckFolderPath`; the two `Valid()` methods;
  - the new codes in `vocabulary.json`, checked by the Go and TS vocabulary tests.
- Expected RED: `undefined: WithCause`; the vocabulary differs in Go and in TS.

**Step 2. The commit rule** (`errors_test.go`, `store_test.go`).
- Tests first:
  - An I/O error is `ErrIO`.
  - **A commit hook on the writer** (`sql.Conn.Raw` → modernc `RegisterCommitHook`,
    `pre_update_hook.go:55`) cancels the context and refuses the commit: the result is
    `ErrConstraint` (`COMMIT` ran and was refused), and no row is kept. With the hook allowing the
    commit, the result is nil and the row is kept.
  - `RLIMIT_FSIZE` set to 0 at `beforeCommit`, after a `wal_checkpoint(TRUNCATE)`, so that the
    commit must extend the WAL. It runs in-process, so it counts for coverage: SIGXFSZ is
    notify-only in Go on darwin (`runtime/signal_darwin.go:33`), so it is ignored, and the write
    fails with EFBIG, which SQLite reports as `SQLITE_IOERR_WRITE`. The result is `ErrOutcomeUnknown`
    + `ErrIO`, never `ErrCanceled`. With the limit restored, the next write succeeds, and after a
    reopen `integrity_check` passes and the state is either unchanged or the whole change.
  - The disk filled at `beforeCommit`, after a `wal_checkpoint(TRUNCATE)`: `ErrFull` without
    `ErrOutcomeUnknown`, nothing kept. The kind the test got is logged.
- Expected RED:
  - the hook case is `ErrCanceled` today;
  - `undefined: ErrIO`, `ErrOutcomeUnknown`.

**Step 3. Torrents** (`torrents_test.go`; the duplicate tests are written before the lookup exists).
- Tests first:
  - Add, reopen, and `Torrents` equals what was added: maximum sizes, Unicode, binary info with zero
    bytes, a non-UTC time that comes back in UTC.
  - An absent metainfo or magnet is `typeof(...) = 'null'`, asserted directly in SQL.
  - Every invalid field is `ErrInvalid`, and nothing is written.
  - A duplicate: the same v1, the same v2, or a hybrid matching two records is `ErrDuplicate` naming
    the right record. That includes **8 concurrent adds of one hash** (exactly one commits, 7 get
    `ErrDuplicate`) and **two `Library` values on one file**.
  - A reused operation ID is `ErrOperationExists`, with `SameRequest` correct.
  - Pause and wanted round-trip. An unknown torrent is `ErrNotFound`. An index the torrent lacks is
    `ErrInvalid`, and no file changes.
- Expected RED: `undefined: (*Library).AddTorrent`; then, for the duplicate tests, `ErrConstraint`
  until the lookup exists.

**Step 4. Settings** (`library_settings_test.go`).
- Tests first: no row is "not found"; a save, a reopen and a read give equal settings (`MaxUint64`
  limits, null limits, a null path); a second save replaces the first; invalid settings are
  `ErrInvalid`.
- Expected RED: `undefined: (*Library).SaveSettings`.

**Step 5. The journal** (`operations_test.go`).
- Tests first:
  - The intent that `AddTorrent` wrote reads back as `running`, with the request and fingerprint
    exact.
  - A finish as succeeded, failed or cancelled, with outcomes, reads back equal after a reopen.
  - Every invalid end is `ErrInvalid` and changes nothing. Finishing twice is `ErrInvalid`.
- Expected RED: `undefined: (*Library).FinishOperation`.

**Step 6. The mapping** (`problem_test.go`).
- Tests first: every kind gets its code; joined kinds follow the precedence; IDs are set;
  `errors.Is(problem, kind)` works through the cause; nil gives nil.
- Expected RED: `undefined: Problem`.

**Step 7. Error branches and regressions** (`repository_test.go`). These are regression tests,
expected to pass on the code of steps 3–6 (`TDD_AND_QUALITY.md:26`); any failure here is a RED to
fix first.
- **Concurrently:**
  - 8 adds of one hash and 8 of distinct hashes;
  - 8 pause togglers, each on its own torrent;
  - 8 wanted togglers on disjoint files of one torrent;
  - one settings writer;
  - readers checking that no snapshot has a record without its files.

  After a reopen, every write that returned nil is present and none is lost.
- **Failure injection** for each write method, at `beforeCommit`:
  - a cancel gives `operation.cancelled`, a full disk `library.diskFull`, and the file-size cap
    `library.outcomeUnknown`;
  - after a reopen the state is unchanged (for the cap: unchanged, or the whole change).
- **Lost library:**
  - the file deleted, or its folder moved, between writes gives `library.unavailable`, and nothing
    is created;
  - a write after `Close` is `internal.unexpected` (a quit-order bug).
- **Behind-the-store changes** (the technique of `completion_test.go:258-275`):
  - a dropped or renamed table, or a renamed column, fails each method's lookup, each `execAll`
    statement and each `queryEach` query;
  - a table rebuilt without STRICT, holding a value of the wrong type, fails each scan.

**Step 8. The facade** (`app_test.go`).
- Test first: a coded error with a cause writes `OctopusTorrent: library.diskFull: "…"` to
  diagnostics; one without a cause writes nothing.
- Expected RED: the line is missing.

**Step 9. The gate step** (`isolation_test.go`).
- Tests first: `CheckSQLIsolation` flags an own package, or its test imports, that imports
  `database/sql`, `database/sql/driver`, `modernc.org/sqlite…` or `zombiezen.com/go/sqlite`, and
  passes `internal/library`. A frontend dependency named `sql.js` fails. The step runs `go list`
  once per build mode, and a `go list` failure fails the step.
- Expected RED: `undefined: CheckSQLIsolation`.

**Mutation checks** (one run each, reverted; recorded in the report). Each must fail a named test:
- `Write` without the commit rule;
- `ErrCanceled` checked before `ErrOutcomeUnknown` in `Problem`;
- `AddTorrent` without the duplicate lookup;
- `SetFilesWanted` without the row-count check;
- JSON bound as `[]byte`;
- an empty `SourceMetainfo` bound as given;
- `FinishOperation` without `finished_at IS NULL`;
- the isolation step ignoring test imports;
- the rule applied in `run`, so a read's failed commit becomes outcome-unknown.

No `time.Sleep` decides a result. The concurrency test asserts invariants and the final state, never
an interleaving.

### Coverage

- **Go:** raw counters through the gate. `internal/library`, `internal/app/contracts`,
  `internal/app` and `internal/devtool` reach 100% from in-process unit tests on real SQLite, real
  APFS temp folders and an HFS+ image. Every error branch is reached by a named technique in step 2
  or step 7, never by a fake driver.
- **The `.app`:** no repository method runs there yet, because nothing calls one. The gate's `.app`
  run still covers `main()` opening and closing the library.
- **TS:** `errorMessages.ts` is data executed on import, so it is at 100% on all four metrics through
  the existing tests.

### Real boundaries

- **Real:**
  - SQLite (modernc 3.53.4);
  - a full HFS+ image;
  - the process's file-size limit (real EFBIG from the kernel, at `COMMIT`);
  - SQLite's commit hook;
  - two stores on one file;
  - `-race` concurrency;
  - a deleted or moved library between writes.
- **The seam:** `beforeCommit` only chooses *when* the real failure happens.
- **Not run, with reasons:**
  - **A volume force-detached under an open library:** modernc maps `-shm` with `MAP_SHARED`, and a
    fault there would abort the test binary. 009 also saw a store left on a detached volume block a
    later open. The default library is on the boot volume, so this is covered by the deleted-file
    and I/O-error cases instead.
  - **Power loss**, as in 008.
  - **Engine integration (018) and test-bridge UI tests (035).**
- **`/qa`:** a regression run of the `.app` (an isolated start, a quit, the 014 library reopening
  unchanged). Nothing new is visible.

### Documents

- **ADR `library-repositories.md`** (`accepted`). It records:
  - one transaction per method, with no callbacks;
  - the contracts types;
  - validation before the `CHECK`s;
  - the commit outcome rule (writes only);
  - the unexported runner and the isolation rule;
  - the codes and their precedence;
  - the two stages;
  - the cause kept out of the library and the UI;
  - the alternatives:
    - a unit-of-work callback over typed statements (it lets a caller wait inside a transaction);
    - types owned by the repository (a second vocabulary);
    - a mapping package in `internal/app` (services there would import it, so it would create a
      cycle);
    - a separate gate step;
    - a fake driver for error branches.
- **Feature doc** `docs/app/library-repositories.md`: the API, the errors, and recipes for 023, 024,
  025 and 050; its index row.
- **ADR edits:** `api-contract.md` gets the codes; `sqlite-driver.md` the commit rule;
  `library-schema-and-data-folder.md` the precision about the migration's commit.
- **The report:** session plan, RED/GREEN, mutation checks, coverage, debt.

### Verification

1. `go test -race -count=1 ./internal/library ./internal/app/... ./internal/devtool`
2. `npm --prefix frontend test -- src/bridge/contract`
3. `go tool devtool check-docs`, `plan-nav`, `check-plan`
4. `/qa` regression; `go tool devtool check` → `GATE PASSED` (`not_run` only for 018 and 035)
5. `/branch-review`; fixes; the gate again; a commit only on `/pushit`, stacked on #20

### Self-assessment: 5/10 → 8/10

An adversarial review (Plan agent, 14 findings) of the first draft found the following, and this
plan fixes it:
- **Critical:** the draft force-detached a volume under an open store in the test process. modernc's
  mmapped `-shm` could abort the whole test binary, and 009 had already seen a later open blocked.
  It is replaced by a real EFBIG at `COMMIT` through `RLIMIT_FSIZE`, verified as safe for a Go
  process on darwin.
- The outcome rule sat in the shared `run`, so it would have turned a read's or a migration's failed
  commit into "unknown". It now applies to writes only, and the migration case is documented.
- No RED reproduced the bug being fixed. The commit hook now does: today it gives `ErrCanceled`, and
  the right answer is `ErrConstraint`.
- A small write on a "full" volume could have succeeded in already-allocated WAL space. A WAL
  truncation before the fill fixes that.
- Step 7's duplicate RED was impossible, so those tests moved to step 3, before the lookup.
- The draft had no way to reach every error branch. It now has shared helpers plus behind-the-store
  changes and the commit hook.
- Values could still hit a `CHECK`:
  - a NUL in a path (fixed: refused);
  - JSON deeper than SQLite accepts (fixed: the repository makes the JSON itself);
  - a save path weaker than the contract's (fixed: `CheckFolderPath`).
- More than 32766 indexes would have broken `IN (…)`. The query now uses `json_each`.
- A mutation that no test could catch: the NULL is now asserted with `typeof`.
- A repeated identical operation would have been reported as `idConflict`, against api-contract. It
  is now told apart (`SameRequest`).
- The isolation check skipped the other build modes and drivers that do not use `database/sql`. Both
  are now covered, inside the existing step.
- The "read again" recipe was wrong: the outcome is unknown until the next open.
- Task 023's notes were stale. A "From 015" note now corrects them.

Remaining weak points, and why they are acceptable:
- **The API is designed ahead of its callers** (023, 024, 025, 029, 050). It is the smallest set the
  task's checks name, and each later task may add a method. The "From 015" notes say what each one
  gets.
- **`torrent.duplicate`'s shape** (`TorrentID` = the existing record) is chosen before 021, which may
  refine it. The repository already returns the existing ID that 021 needs.
- **`ErrOutcomeUnknown` is conservative:** the EFBIG case really rolls back. The rule cannot tell
  that apart from an fsync failure after the commit frame, so it never claims either.
- **A real pulled volume under an open library is not exercised** (see "Real boundaries").
- **A repeated identical operation from another runtime becomes `internal.unexpected`** if the
  service does not answer it from the journal first. 023 owns that answer, and its note says so.

## Implementation


All commands ran from the repository root on 2026-11-04 (Apple M4, macOS 27, Go 1.27.3,
modernc.org/sqlite v1.60.1 / SQLite 3.53.4), on the uncommitted tree of this branch.

### Step 0: the runner unexported (a pure refactor, no RED)

`Store` → `store` and `Tx` → `txn` in every file of `internal/library`, by a scripted regex
(`(?<![.\w])Store\b`, `(?<![.\w])Tx\b`; `sql.Tx` untouched). `gopls` is not installed and a global
install is not allowed, so no rename tool was used. Before and after: `go vet ./...` → `pass`;
`go test -race ./internal/library` → `pass` (51.3 s before, 36.8 s after);
`./internal/torrentengine/filestore` → `pass` (44.7 s); the root package → `pass`.

### Step 1: contracts

- **RED:** `go test ./internal/app/contracts` → `fail` (build): `undefined: CheckFolderPath`;
  `s.Valid undefined` (`ImportSource`); `k.Valid undefined` (`OperationKind`). Then
  `npm --prefix frontend test -- src/bridge/contract` → `fail`: `vocabulary.test.ts` "every Go and
  bridge error code has a message" (1 failed, 8 passed).
- **GREEN:** `go test ./internal/app/...` → `pass`; the TS contract tests → `pass` (7 files, 124
  tests).

### Step 2: the commit outcome rule

- **RED (behavior):** `TestACommitThatRanIsReportedByItsOwnResultWhenTheContextEnds/the_commit_refused`
  → `fail`: `the library operation was cancelled: context canceled / constraint failed (531)`. The
  commit hook refused a `COMMIT` that ran (`SQLITE_CONSTRAINT_COMMITHOOK`), and 008's rule reported
  it as cancelled. Then the compile RED of the injection tests: `set.beforeCommit undefined`.
- **GREEN:**

  | Case | Result |
  |---|---|
  | the hook refused | `ErrConstraint` |
  | the hook allowed | nil, and the row is kept |
  | `RLIMIT_FSIZE = 0` at `beforeCommit`, after `wal_checkpoint(TRUNCATE)` | `it is not known whether the library change was stored: … disk I/O error (778)`, i.e. `SQLITE_IOERR_WRITE`; the next write is fine; after a reopen 2 rows, `integrity_check` ok |
  | the full HFS+ image at `beforeCommit` | `ErrFull` (13) without `ErrOutcomeUnknown`; 1 row after the reopen |
  | a migration's `COMMIT` capped | `ErrMigrationFailed` + `ErrIO` (778) only, and the next open migrates |

### Step 3: torrents

- **RED:**
  - `go vet ./internal/library` → `undefined: Torrent`.
  - With `AddTorrent` written without its lookup, the duplicate tests failed as planned.
    `TestADuplicateNamesTheExistingRecordAndAddsNothing` failed in 5 cases with
    `UNIQUE constraint failed: torrents.infohash_v1/v2 (2067)`, want `ErrDuplicate`, and so did
    `TestConcurrentAddsOfOneHashCommitExactlyOnce` (7 of 8) and `TestTwoLibrariesOnOneFileAddOneHashOnce`.
    `TestAReusedOperationIDIsRefusedAndToldApart` failed with
    `UNIQUE constraint failed: operations.operation_id (1555)`, want `ErrOperationExists`.
  - `TestAFolderOfManyFilesIsOneChange` (then 40000 files) hit its 5 s deadline: one prepare per
    `Exec`.
- **GREEN:** all 11 torrent tests → `pass`. A NUL inside a torrent's *name* round-trips; only file
  paths refuse a NUL.

### Steps 4–6: settings, the journal, the mapping

- **Settings.**
  - RED: `go vet` → `lib.Settings undefined`.
  - GREEN: 3 tests → `pass`. A `MaxUint64` limit round-trips as its int64 bit pattern.
- **The journal.**
  - RED: `go vet` → `undefined: Operation`.
  - The first GREEN run failed in a test of its own: a hash byte taken from `len(state)` gave
    `succeeded` and `cancelled` the same hash, so the second add was `ErrDuplicate`. The test now
    uses the index.
  - GREEN: 5 tests → `pass`.
- **The mapping.**
  - RED: `go vet` → `undefined: Problem`.
  - GREEN: `TestEveryKindHasItsCodeAndKeepsItsCause` → `pass`.

### Step 7: error branches and regressions (expected green)

These all passed on their first run:
- every write method failed at its commit point: a cancel → `operation.cancelled`; the file-size
  cap → `library.outcomeUnknown`; the full HFS+ image → `library.diskFull`. Each is unchanged after
  a reopen.
- the library deleted, or its folder moved → `library.unavailable`, and nothing is created.
- a write after `Close` → `internal.unexpected`.
- `TestConcurrentChangesNeitherDuplicateNorLoseAnything`.

One older test failed, `completion_test.go`: it built `&store{}` without `beforeCommit`, so `Write`
called a nil function. It now uses the shared `behind(t, s)` helper.

**Coverage.** The first measurement was 94.5%. Not reached:
- the `Error()` methods of the three typed errors;
- `problemJSON`'s nil-params branch;
- the SQLite and decoder error branches.

Added: `TestATableChangedBehindTheLibrarysBackIsReportedNotHidden` (17 cases), which a raw
connection drives behind the store:
- tables and columns dropped or renamed;
- a trigger that refuses;
- a NULL where a scan needs a value;
- a 2-byte hash;
- `'[1]'` as an error's JSON.

Also added: `TestTheRepositoriesErrorsNameWhatTheyAreAbout`, and an error without params in the
journal test. Result: `go test -race -count=1 -covermode=atomic ./internal/library` →
`coverage: 100.0% of statements` (60.6 s).

### Steps 8–9: the facade and the gate step

- **The facade.**
  - RED: `TestErrorFormatterLogsTheCauseOfACodedError` → `fail`:
    `diagnostics = "", want "OctopusTorrent: library.diskFull: \"database or disk is full\\nOctopusTorrent: forged\"\n"`.
    The cause carries a newline, and quoting keeps it from forging a line.
  - GREEN: `go test ./internal/app/...` → `pass`.
  - Withdrawn after the review by the owner's decision of 2026-11-05: a coded error's cause is now
    written nowhere ([below](#owner-decision-after-the-review-2026-11-05)).
- **The gate step.**
  - RED: `go vet ./internal/devtool` → `check_test.go:26:40: undefined: sqlImportsFormat`. The fakes
    and the tests of `CheckSQLIsolation` were written first.
  - GREEN: `go test ./internal/devtool` → `pass`.

On the real tree, `go list -tags=<mode> -f <format> ./...` for `""`, `dev` and `bindings` finds
`database/sql`/modernc only in `internal/library`, and `frontend/package-lock.json` has no package
named `sql`.

### Mutation checks (one run each)

A scratch script applied each mutation, ran the named tests with `go test -count=1 -run …`, restored
the file and compared its SHA-256 with the original.

| Mutation | Result |
|---|---|
| `Write` without the commit rule (008's classification) | killed: `TestACommitThatRanIsReportedByItsOwnResultWhenTheContextEnds/the_commit_refused` (cancelled), `TestACommitThatCannotWriteHasAnUnknownOutcome` (778 without `ErrOutcomeUnknown`) |
| `ErrCanceled` before `ErrOutcomeUnknown` in `Problem` | killed: `TestEveryKindHasItsCodeAndKeepsItsCause` (outcome-unknown + cancelled and migration-failed + cancelled became `operation.cancelled`) |
| `AddTorrent` without the duplicate lookup | killed: `TestADuplicateNamesTheExistingRecordAndAddsNothing` (5 cases, `UNIQUE … (2067)`) |
| `SetFilesWanted` without the row-count check | killed: `TestChangesOfAnUnknownTorrentAreNotFound` (nil), `TestAnInvalidChangeIsRefusedAndChangesNoFile` (nil, files changed) |
| the request JSON bound as `[]byte` | killed: `TestAnImportIsRecordedAsARunningOperation` (`cannot store BLOB value in TEXT column operations.request (3091)`) |
| an empty `SourceMetainfo` bound as given | killed: `TestAnAbsentSourceIsStoredAsNull` (`stored as blob and null`) |
| `FinishOperation` without `finished_at IS NULL` | killed: `TestAFinishedOperationNeverFinishesAgain` (nil, want `ErrInvalid`) |
| the write rule applied in `run` | killed: `TestAMigrationWhoseCommitCannotWriteFailsAndTheNextOpenMigrates` (`ErrMigrationFailed` + "it is not known whether…") |
| the isolation listing without test imports | **survived** at first: `go test ./internal/devtool` passed, because no test ran the real format. Added `TestTheSQLListingIncludesTestImports`, which runs the real `go list` on a module of its own (a `_test.go` importing `database/sql`, an external test importing `database/sql/driver`). RED with the mutation: `CheckSQLIsolation of the real listing = [] want [… database/sql … database/sql/driver]`. GREEN after the restore. |

After every restore: `go vet ./...` → `pass`.
`go test -race -count=1 ./internal/library ./internal/app/... ./internal/devtool` → `pass`
(library 53.6 s).
`go test -count=1 -covermode=atomic -cover ./internal/app/... ./internal/devtool` → 100.0% each.
`npm --prefix frontend test -- src/bridge/contract` → 124 passed.

## Gate

`go tool devtool check` (2026-11-04, branch `task/015-transactional-repositories`, uncommitted tree).

- **Run 1 → `GATE FAILED`, exit 1**, in `go tests` only. The failure was in
  `internal/torrentengine/testutil/swarm`, `TestACorruptedPieceReceivedOverTheWireIsNeverAccepted`:
  `WaitComplete = 0 of 75000 bytes complete, missing pieces [0 1 2]: context deadline exceeded`.
  - Nothing moved in 20 s. This is the engine stall of finding 22 under the gate's load: the test's
    peers keep the engine's default keep-alive, so nothing wakes a writer that missed its wake-up.
  - The same failure, with the same text, is recorded in
    [the refactor report](refactor-after-009.md) (`9f7aa0a`, first gate).
  - `swarm` is unchanged by this task. The test passed `-count=5` alone (4.7 s).
  - Every other step passed, including `go coverage`, `product isolation` (with the new SQL rule) and
    `data folders`.
- **Run 2, with no edits in between → `GATE PASSED`, exit 0.**
- **Run 3, after the review's fixes and the task's closing edits → `GATE PASSED`, exit 0.** Every step
  is as in the table below. After it, only this line and the task file's gate line were written,
  checked with `check-docs` and `check-plan`.
- **Run 4, after the owner's decision of 2026-11-05 → `GATE PASSED`, exit 0.** 26 steps `pass`;
  `not_run` only for engine integration (018) and test-bridge UI tests (035). After it, only this
  line and the task file's gate line were written.
- **Run 5, at `/pushit`, after the agent instructions were updated (`CLAUDE.md`, `review.md`,
  `review-security.md`; owner-confirmed) → `GATE PASSED`, exit 0**, with the same 26 steps `pass`. The
  review's fixes do not touch the start path
  (`begin`'s panic rollback runs only on a panic), so the `/qa` regression run below stands. The
  gate's `.app` run started and quit the final build.

| Step | Result |
|---|---|
| npm ci, npm cooldown, go env, typecheck, lint | `pass` |
| frontend tests | `pass`: 23 own files at 100% lines, statements, functions and branches |
| frontend build, gofmt | `pass` |
| go vet | `pass`: no tags, `-tags dev` and `-tags bindings` |
| bindings | `pass`: unchanged (no facade method changed) |
| go tests | `pass` |
| .app coverage run | `pass`: OctopusTorrent and EngineSpike launched and quit |
| go coverage | `pass`: every block of every own Go file covered (unit tests + `.app` run) |
| native coverage | `pass`: `internal/platform/trash_darwin.m` |
| escape hatches, scripts | `pass` |
| product isolation | `pass`: 316 packages, none of the spike, test support or the engine; SQL only in `internal/library` |
| release build, docs, plan | `pass` |
| release age, npm audit, govulncheck | `pass` |
| updates | `pass`: 2 updates out of quarantine (vite 8.3.4 → 8.3.5, anacrolix/chansync v0.7.0 → v0.8.0); not taken here, they belong to a dependency change of their own |
| engine integration | `not_run` (018) |
| test-bridge UI tests | `not_run` (035) |
| single code version, data folders | `pass` |

## Manual QA (`/qa`, the `macos-qa` agent, 2026-11-04)

This is a regression run: nothing new is visible in the window. The release build of this tree was
built with `OCTOPUS_DATA_DIR=build/qa-tool go tool wails build -clean -trimpath` and driven through
Accessibility. Every launch had its own `OCTOPUS_DATA_DIR`. The owner's folder was checked only for
existence, and was `absent` before and after.

| # | Scenario | Expected | Observed | Result |
|---|---|---|---|---|
| 0 | the build | exit 0, `build/qa-tool` not created | exit 0, absent | `pass` |
| 1 | start on an empty `build/qa-data` (`700`), **Check Go connection**, quit through the menu | `Go go1.27.3 · darwin/arm64 · SQLite 3.53.4`; `library.sqlite` `600`, `application_id` 1330924610, `user_version` 1, 7 tables, `integrity_check` ok | as expected; the process gone after the menu quit. The folder was listed only after a read-only `sqlite3` check, so the clean state at quit is proven by 2 | `pass` |
| 2 | start again on the same folder | the same text, the file unchanged, only `library.sqlite` after the quit | sha256 `6f3510f3…c4173` before and after; the `-wal`/`-shm` that the agent's `sqlite3` read had left were removed by the app's quit | `pass` |
| 3 | a settings row seeded with `/usr/bin/sqlite3` (`1, NULL, 'system', 'en', NULL, 1048576, 1`), start, quit | the app starts and quits; the row, `user_version` and integrity unchanged | the row `1\|\|system\|en\|\|1048576\|1` and sha256 `55fb1643…e3da` unchanged; `integrity_check` ok; `user_version` 1; mode `600` | `pass` |
| 4 | a copy at `user_version = 2`, the executable run directly | exit 1, no window, `written by a newer version`, the file unchanged | exit 1: `OctopusTorrent: open the library: the database was written by a newer version of the application: the library database is at version 2, this build reads versions up to 1`; sha256 `c4efe7d6…8794` unchanged | `pass` |
| 5 | the owner's folder | never created | `absent` before and after | `pass` |

Every app process the agent started (4 PIDs) exited, and `pgrep` found no instance afterwards. The
agent removed `build/qa-newer`. `build/qa-data` stays as the QA folder.

## Deviations from the plan

- **The migration's commit test.** The first version capped the file size at `afterCheck` on a new
  file. It failed at `-shm` creation (`SQLITE_IOERR_SHMOPEN`, 4618, from `BEGIN`), not at `COMMIT`.
  It now creates the file at v1 first, because the read-only check creates the `-shm` before
  `afterCheck`. The migration to v2 then fails at `COMMIT` with 778, as the test pins.
- **Many files.** The many-files test uses 32767 files, one past SQLite's 32766 parameters, not
  40000, with 1-minute bounds; it takes 8.6–13.4 s under `-race`. To make it fit:
  - `execAll` now prepares each distinct statement once per call;
  - `SetFilesWanted` makes one `UPDATE … json_each` and checks `EXISTS` only on a count mismatch.
- **The SQL listing.** The plan's isolation tests used fakes only. A mutation showed that the
  listing format itself was untested, so a test now runs the real `go list` (above).
- **The 016 note.** A "From 015" note was also added to 016, the next task on this path, which the
  plan's list did not name.
- **The review's fixes** (below) added to the plan's design:
  - a second identity check after `COMMIT`;
  - a rollback on panic;
  - the infohash-to-info and file-path rules in `Torrent.check`;
  - the index checks in `Torrents`;
  - the release tags as a fourth build mode of the SQL rule.

## Refactoring

- The runner is unexported (`store`, `txn`). `run` and `Write` share `begin`, and only `Write`
  carries the commit rule.
- One `execAll` and one `queryEach` serve every repository, and one `behind(t, s)` helper serves
  every test that changes tables behind the store, the marks store's included.
- `checkFolderPath` became `CheckFolderPath`, and `Settings`' check became the exported `Validate`,
  so the repository reuses the contract's rules instead of copying them.

## Not run

| Check | Why | Where it is covered instead |
|---|---|---|
| A volume force-detached under an open library | modernc maps `-shm` with `MAP_SHARED`, so a fault there would abort the test binary, and 009 saw a store left on a detached volume block a later open; the library lives on the boot volume | the library deleted or its folder moved between writes (`library.unavailable`); a real I/O error at `COMMIT` (EFBIG) |
| Power loss at `COMMIT` | cannot be produced on this Mac (as in 008) | `synchronous=FULL` + `fullfsync`; SIGKILL mid-commit (008, 014); `ErrOutcomeUnknown` for an I/O error at `COMMIT` |
| A repository method in the `.app` | no facade method calls one before 023/024/025/050 | the `.app` run still opens and closes the library (014's check); `/qa` regression run |
| Engine integration, test-bridge UI tests | tasks 018, 035 | — |

## Debt

None left by this task: no ignore directive, no skipped test, no TODO, no stub. These limits are
recorded, and none of them is deferred work:
- The API is designed ahead of its callers. Each later task may add a method, and the "From 015"
  notes say what each one gets.
- `torrent.duplicate`'s shape may be refined by 021.
- `ErrOutcomeUnknown` is conservative: an EFBIG at `COMMIT` really rolls back, but the rule cannot
  tell it from an fsync failure after the commit frame.
- A repeated identical operation reaches `Problem` as `internal.unexpected` only if a service skips
  the journal answer that 023's note requires.
- A failed operation's cause is logged only when a facade method returns the error (017).

## Review (`/branch-review`, 2026-11-04)

Three reviewers, read-only, on the uncommitted tree against `5f53efd`:
- **code-reviewer:** no critical issue; 4 quality violations, 4 warnings.
- **security-reviewer:** no critical issue; 2 medium findings, 5 low.
- **tech-lead:** "needs changes", for two reproduced defects. It found the design sound: the commit
  rule checked against the pinned sources, one transaction per method, the `contracts` import and
  `Problem`'s precedence.

### Fixed, test first

| Finding | RED | GREEN |
|---|---|---|
| **Defect 1** (tech lead, reproduced): a library deleted or moved during a write got nil. In WAL mode SQLite commits into the unlinked or moved inode without an error, and the identity check ran only before `BEGIN`. | `TestALibraryLostWhileAChangeCommitsIsNeverConfirmed`: 10 of 10 cases (deleted / its folder moved × 5 write methods) `= <nil>, want ErrOutcomeUnknown and ErrReplaced` | `Write` checks the identity again after a nil `COMMIT`, and returns `ErrOutcomeUnknown` + `ErrReplaced` (`library.outcomeUnknown`) |
| **Defect 2** (tech lead, security M2, code-reviewer W2): `Torrents` looked a file's torrent up without `ok`. An orphan file row (foreign keys off, `sqlite3`'s default) was attached to another torrent, or panicked with no torrents, and a gap in `file_index` shifted every later index. | new cases "a file of no torrent", "a gap in a torrent's file indexes" in `TestATableChangedBehindTheLibrarysBackIsReportedNotHidden`: `the changed table went unnoticed` (the orphan was attached to torrent A) | `Torrents` selects `file_index` and refuses an unknown torrent ID and an index that is not the next one |
| code-reviewer Q4: the 17 behind-the-store cases asserted only `err != nil` | the stricter assertions were written, and passed on the existing cases | each case now requires `internal.unexpected` and every table's rows unchanged (`contents`, compared as a sorted set: a first version with `ORDER BY rowid` failed on the `WITHOUT ROWID` tables) |
| security L4: the SQL listing missed a file that only the release tags compile | `TestCheckStepsReportTheirOwnFailures` / "SQL in a file only the release build compiles": `product isolation` passed | `buildModes` gains `desktop,wv2runtime.download,production`; the real tree under the four modes: only `internal/library` |
| security M2 (rollback): a panic inside a transaction's function left the transaction open until its context ended | `TestAPanickingFunctionRollsBackAndFreesTheWriter`: `INSERT … (2): the library operation was cancelled: context deadline exceeded` after 10 s. The first version, with a context that never ended, hung even the cleanup's `Close` and was stopped. | `begin` rolls back on a panic and panics again |
| security M1: the library stored `../../etc/passwd`, `/abs/file` and `a/../b` as file paths, and infohashes of other info | the test fixtures now derive each infohash from its `Info` (the duplicate test uses records of one hash of an info). The new cases of `TestAnInvalidTorrentOrIntentIsRefusedAndWritesNothing` failed: `a file path through a parent = <nil>, want ErrInvalid` (stored). Every other library test passed with the new fixtures. | `Torrent.check` requires `sha1(Info) == V1`, `sha256(Info) == V2`, and for each file path `filepath.IsLocal`, `path.Clean(p) == p` and `p != "."`. The form of `TorrentFile.Path` is documented: relative to `SavePath`, the torrent's folder first. |

After the fixes, `go test -race -count=1 -covermode=atomic ./internal/library` → `pass`,
`coverage: 100.0% of statements` (51.2 s); `go test -race ./internal/devtool` → `pass`.

### Changed without a new test

These were comments, names and documents:
- `Library` and `doc.go` name the repositories (Q1).
- `docs/development/testing.md` names `txn` (Q2).
- `Settings`' named return is `saved`, so it no longer shadows the package's `settings` (W1).
- `Torrents` says it is the full read for the start (W3).
- `OperationIntent.ID` is documented as the request's own `operationID`. `Request` has no accessor,
  and adding one for every request is the shared `accept` path's (024) to design (W4).
- The settings round trip now fails when an exported field of `contracts.Settings` is left unset, so
  026/050 cannot add a field without a column (tech lead).
- The ADR gives the reason a full disk at `COMMIT` is a known outcome:
  `SQLITE_POWERSAFE_OVERWRITE = 1` in the pinned build, so no padding frames follow a commit frame
  (tech lead).

### Recorded for the task that owns it

These are notes in the ADR and the task files:
- **L1, `ErrorFormatter`'s cause line.** It can carry full local paths and long torrent text. This
  was first recorded for 017; the owner then chose to fix it in 015 (below).
- **L2, the journal's requests.** `operations.request` kept the request's paths for as long as the
  journal. This too was first recorded for 017/016 and then fixed in 015 by the owner's decision
  (below).
- **L3, stored paths.** A stored path is lexically safe, but the disk may have changed since.
  020/029/031 resolve and confine it before use.
- **L5, the file count.** It is bounded by `infocheck`'s tree-node limit and the import (019/023).
- **M1, case.** Paths that differ only in case collide on APFS; that is the path plan's (020).
  Hostile input reaching `ErrInvalid` is the import's to refuse first with a code of its own (023's
  note).
- **The `library.unavailable` text for read failures.** It is 050's wording.

### Checked and rejected

- **`checkpoint_fullfsync` (security, out of the diff).** By SQLite's documentation, with
  `fullfsync` on, F_FULLFSYNC is used for every sync, checkpoints included, so
  `checkpoint_fullfsync` is irrelevant. 008 sets `fullfsync=1` on every connection.
- **`CLAUDE.md` still names `library.Tx` and `Store`.** Agent instructions change only through
  `/update-rules` with the owner's confirmation: proposed at `/pushit`.

## Owner decision after the review (2026-11-05)

Asked about L1 and L2 (paths that outlive their use), the owner chose to fix both in 015 rather than
leave them to 016/017.

| Change | RED | GREEN |
|---|---|---|
| `FinishOperation` replaces the stored request with `{}`. A finished operation needs none: its fingerprint still tells a repeat, and its state and error answer it. A running operation keeps its request for 029. No migration: the column's `CHECK` is `json_valid`. | `TestAFinishedOperationIsReadBackExactlyAfterAReopen` → `fail`: every finished operation read back with `"destination":"/Users/owner/Downloads"` in its request, want `{}`. The same test now also requires a repeat of the finished operation to be `ErrOperationExists` with `SameRequest`. | `UPDATE operations SET …, request = '{}' WHERE … AND finished_at IS NULL`. `TestAFinishedOperationNeverFinishesAgain` was updated to expect `{}`. |
| `ErrorFormatter` writes no coded error's cause. It can carry full local paths (DATA-03), and only 017's redacting log may write it. The uncoded `internal.unexpected` line of 005 is unchanged, and is 017's. | `TestErrorFormatterWritesNoCauseOfACodedError` (it replaces `TestErrorFormatterLogsTheCauseOfACodedError`) → `fail`: `diagnostics = "OctopusTorrent: library.diskFull: \"disk I/O error in /Users/owner/Library/library.sqlite\"\n", want nothing` | the coded-cause branch removed; `app.go` differs from `5f53efd` only in its doc comment |

`go test -race -count=1 -covermode=atomic -cover ./internal/library ./internal/app/...` → `pass`,
100.0% each (library 61.0 s). Documents updated:
- the ADRs library-repositories and api-contract;
- the facade and library-repositories feature docs;
- the 016 and 017 notes;
- the task file.
