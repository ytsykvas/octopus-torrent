# Go–TypeScript contract: DTOs, numbers, errors, operations and ordering

**Status:** `accepted`  
**Date:** 2026-10-15  
**Task:** [005](../../product/tasks/005-domain-dto-and-bridge-contract.md)  
**Register item:** new (the [architecture §4](../../product/APPLICATION_ARCHITECTURE.md#4-gotypescript-contract) left the wire formats open)  
**Requirements:** `AI-05`, `AI-02`, `STACK-01`, `STACK-02`, `STACK-03`

## Question

Every later task sends data between Go and React through Wails v2. Wails generates bindings but
does not define identifiers, units, the representation of 64-bit numbers, error shape, how an
accepted command differs from a completed one, how a retry is recognized, or how stale snapshots
and events are discarded. Those rules must be fixed once, executable in both languages, before
the engine (007, 018), the library (022), the publisher (027) and the store (035) are built on them.

## Decision

### Identifiers and formats

| Value | Wire format | Made by |
|---|---|---|
| `torrentID`, `pendingImportID`, `runtimeID` | UUID v4, lower-case canonical text | Go |
| `operationID` | UUID v4, lower-case canonical text | the frontend (`newOperationID`, built from `crypto.getRandomValues`: `crypto.randomUUID` needs a secure context, which the `wails://` scheme is not declared to be), once per user intention; a retry reuses it |
| `deletionPlanID` | UUID v4 | Go, when it builds the file list a deletion with files shows; bound to exactly those torrents and files, usable once, valid only in that runtime (031/032) |
| `infohashV1` | 40 lower-case hex (SHA-1); `null` = no v1 | Go |
| `infohashV2` | 64 lower-case hex (full SHA-256); `null` = no v2; never both `null` | Go |
| `fileIndex` | JSON integer, the engine's index, `0 … fileCount − 1`; pad files keep theirs | Go |
| timestamps | RFC 3339 in UTC (`2026-10-15T12:00:00Z`) | Go |
| `TorrentFile.path` | relative, `/`-separated, without the torrent's root folder; untrusted text; folders are derived, no folder IDs | Go |

`runtimeID` is a new identity for each Go run (not an installation identifier, not analytics);
revisions are compared only within one runtime.

### Numbers

- Every 64-bit quantity — bytes, bytes per second, seconds (ETA), `revision` — is a **decimal
  string** on the wire (`"0"`, `"18446744073709551615"`: no sign, spaces, exponent or leading zeros)
  and a `bigint` in TypeScript. Go types `ByteCount`, `BytesPerSecond`, `DurationSeconds`,
  `Revision`; their fields carry `ts_type:"string"` so the generated models say `string`, and a
  reflection test fails when a new 64-bit field lacks it.
- Small bounded integers (indexes, counts, `previewVersion`) are JSON numbers, safe integers on
  both sides.
- `null` means unknown, never zero: `totalBytes: null` = metadata not known yet, `"0"` = known and
  empty; `etaSeconds: null` = not computable (not downloading, or no download rate), `"0"` = under
  a second; a speed limit `null` = unlimited, `"0"` is rejected (stopping upload is
  `globalUploadDisabled`, TR-04).

### DTOs

Go: `internal/app/contracts` (each field documents its unit and absence). TypeScript:
`frontend/src/bridge/contract`. All keys are always present.

| DTO | Fields |
|---|---|
| `LibrarySnapshot` | `runtimeID`, `revision`, `torrents` |
| `LibraryDelta` | `runtimeID`, `baseRevision`, `revision` (> base), `upserted` (full torrent states), `removed` (IDs) |
| `TorrentSnapshot` | `id`, `infohashV1`, `infohashV2`, `name`, `status`, `manualPaused`, `uploadAllowed`, `totalBytes`, `wantedBytes`, `completedBytes`, `downloadBytesPerSecond`, `uploadBytesPerSecond`, `peerCount`, `etaSeconds`, `savePath`, `source`, `addedAt`, `fileCount`, `selectedFileCount`, `files`, `errors` |
| `TorrentFile` | `index`, `path`, `sizeBytes`, `completedBytes`, `wanted` |
| `ImportPreview` | `pendingImportID`, `previewVersion` (≥ 1), `source`, `state`, `name`, `files`, `totalBytes`, `destination`, `existingTorrentID` |
| `PreviewFile` | `index`, `path`, `sizeBytes`, `selected` |
| `Settings` | `defaultSavePath` (`null` = not chosen), `theme`, `language`, `downloadLimit`, `uploadLimit`, `globalUploadDisabled` |
| `OperationStatus` | `operationID`, `runtimeID` (that accepted it), `kind`, `state`, `error` (set exactly when failed or cancelled) |
| `AppError` | `code`, `operationID`, `torrentID`, `params` (strings; `{}` when none) |

Closed enumerations (an unknown value is a contract violation; adding one is a contract change):

- status: `waitingForMetadata`, `checking`, `downloading`, `paused`, `completed`, `seeding`,
  `noFilesSelected`, `error`, `deleting`, `relinking` — vocabulary only; priority is O11
  (013/027/055);
- source: `file`, `finder`, `drop`, `magnet`, `url`, `watchFolder`;
- import state: `waitingForMetadata`, `ready`, `failed`, `cancelled`, `expired`;
- theme: `system`, `light`, `dark`; language: `en`, `uk`, `es`, `de`, `fr`, `pt`;
- remove mode: `recordOnly`, `withFiles`;
- operation kind: `pauseTorrents`, `resumeTorrents`, `setFilesWanted`, `confirmImport`,
  `removeTorrents`, `updateSettings`; operation state: `accepted` → `running` → `succeeded` |
  `failed` | `cancelled`.

### Requests

`TorrentSelectionRequest` (pause/resume), `SetFilesWantedRequest`, `ConfirmImportRequest`,
`CancelImportRequest` (no operation ID: changes nothing), `RemoveTorrentsRequest`
(`deletionPlanID` required for `withFiles`, `null` for `recordOnly`: a number the frontend could
make up would prove no confirmation, DEL-02/DEL-05), `UpdateSettingsRequest`. Every non-trivial
mutation carries `operationID`.

Folder paths from the frontend (`destination`, `defaultSavePath`) must be clean absolute paths
below `/` (`filepath.Clean(p) == p`), without control characters (a NUL truncates a path in C
APIs), at most 1024 bytes. That is only the lexical check: the facade accepts a destination only
if it equals a folder Go got from the native dialog in this run or the saved default (034), and the
file service resolves symlinks and checks a writable directory before any write (020). An unknown
key is echoed in `params.field` shortened to 64 characters.

Go decodes each request **field by field** (`encoding/json`, which Wails uses, would read a missing
`wanted` as `false` and ignore unknown keys): a missing, `null`, unknown or mistyped key becomes
`request.invalid` with `params.field` (a JSON path such as `fileIndexes[1]` or
`settings.uploadLimit`) and `params.reason` (`missing`, `null`, `unknown`, `type`, `format`,
`range`, `empty`, `value`). `Validate()` adds the value rules. The facade re-validates even what the
UI validated (STACK-01). The TypeScript encoders write exactly the keys of each request.

### Operations

- Accepting returns `OperationStatus` with `accepted`; completion is read again with
  `GetOperation(operationID)` or seen in the full state. An event is never the only evidence.
- Idempotency: the key is `operationID`; the fingerprint is the SHA-256 of the normalized request
  (operation ID removed, ID sets sorted and deduplicated). Same ID + same kind and fingerprint →
  the stored status, no second mutation; otherwise `operation.idConflict`.
- Cancellation cancels the operation's context. An operation still `accepted` ends `cancelled`
  without running its mutation; a `running` one ends as its mutation decides (`operation.cancelled`
  only when nothing took effect); a finished one keeps its final state. A mutation that can be
  partly done — removing files of several torrents (031/032), confirming an import (023) — is not
  cancellable once it runs: it finishes, and a partial result is `failed` with a code whose params
  list what was done. Those tasks add the codes.
- The ledger (`internal/app/operations`) is in memory, one per run, and never evicts (an evicted
  ID would let a retry run again); persistence of import and deletion operations is the Operations
  table of 014. It owns every operation goroutine: Wails `OnShutdown` cancels them and waits at
  most 5 s, and nothing starts afterwards.
- `operation.notFound` carries the current `runtimeID`. From the runtime that accepted the
  operation it means "never accepted" (resending with the same ID is safe); from another runtime
  Go restarted and the outcome is **unknown**: read the full state, never resend because of it.
- **Order (STACK-03).** The ledger itself does not order work: every accepted operation gets its own
  goroutine, so acceptance order is lost once `Start` returns, and serializing inside the mutation
  (a per-torrent lock) could still let a later command win. Commands for one torrent must be
  ordered **at acceptance**: task 013 extends `Start` with ordering keys (the sorted torrent IDs of
  the request; an operation runs when the previous one holding any of its keys has finished; keys
  are taken in sorted order, so multi-torrent commands cannot deadlock). This is a planned change of
  the ledger API, owned by 013.
- **One acceptance path.** The first command (024/025) adds a single `accept(kind, request,
  mutation)` that validates the request, takes its operation ID from the request (an
  `operationID()` method on `Request`), computes the fingerprint and starts the mutation; facade
  methods never repeat those three steps by hand.
- **Bounds.** The ledger has no cap on entries or running operations yet; the IDs are chosen by the
  frontend, so a runaway loop grows memory. 064 (O17) measures and sets a cap with an
  `operation.busy` error. The single acceptance path (024) is a merge condition: no facade method
  may start a request that skipped `Validate()`.
- **Not yet in the contract:** after a frontend reload React has lost its operation IDs, so it
  cannot ask for pending operations; 027/035 add pending operations to the snapshot or a
  `ListOperations` method. A panicking mutation would stop the application: 028 recovers it as
  `failed`/`internal.unexpected`, and folds the ledger stop into the ordered quit (operations,
  then the publisher, then the engine, then the database).

### Errors

`AppError.code` is `<area>.<problem>`, stable, never UI text.

| Code | Origin | Params |
|---|---|---|
| `request.invalid` | Go | `field`, `reason` |
| `operation.idConflict` | Go | — |
| `operation.notFound` | Go | `runtimeID` |
| `operation.cancelled` | Go | — |
| `internal.unexpected` | Go (any error without a code) | — |
| `bridge.callFailed` | TypeScript (the call failed before Go answered) | `detail` (diagnostics only) |
| `contract.invalidDto` | TypeScript (a Go answer broke the contract) | `path`, `reason` |

Transport: the Wails v2.16 JavaScript runtime rejects a failed call with `new Error(message.error)`
(`internal/frontend/runtime/desktop/calls.js:181`), so an object would arrive as
`"[object Object]"`. `options.App.ErrorFormatter` (`app.ErrorFormatter(stderr)`) therefore returns
the **JSON text** of the `AppError`, and writes the cause of any error without a code to stderr
(local diagnostics take over in 017), never to the UI; the bridge parses `Error.message`, and any other rejection becomes
`bridge.callFailed`. `describeError` maps a known code to the message key `error.<code>` with its
params and an English fallback; an unknown code gets `error.unknown` with the code (texts move to
the dictionaries in 041). Each task that adds a code adds it to this table, to `contracts/errors.go`,
to `testdata/vocabulary.json` and to the catalogue in `bridge/contract/errorMessages.ts`.

### Snapshots, deltas and ordering

Event names: `library.snapshot`, `library.delta`, `operation.changed` (emitted from 027).
The rules, as pure functions in `bridge/contract/ordering.ts`:

1. Subscribe first, then fetch the full snapshot; each fetch is numbered, an answer to an older
   fetch is discarded.
2. A full snapshot is authoritative for its runtime and may skip revisions — never a reason to
   fetch again; an older snapshot of the same runtime is discarded.
3. A delta applies only when its runtime matches and `baseRevision` equals the current revision;
   an older one is discarded; a gap starts one fetch, and none while a fetch is in flight.
4. A different runtime retires the current one: its later messages are dropped whatever their
   revision; revision 0 of the new runtime is accepted. Only one Go process emits to a window, so
   a message from an unseen runtime means Go restarted; there is no order between runtimes.
   If the awaited fetch answer comes from a runtime retired meanwhile, fetch again; while no state
   is applied and nothing is fetched (`needsFetch`), the store fetches with its own back-off (035).
5. Operation states only move forward; an `operation.changed` with an earlier or equal state is
   discarded.

### Strictness

Both test suites compare their enumerations and error codes with one file,
`internal/app/contracts/testdata/vocabulary.json`, so neither side can add a value alone. Go
normalizes outgoing DTOs when encoding: a nil list is `[]`, a nil `params` is `{}`, times are UTC.

Go and the frontend ship in one binary, so both sides are strict: a missing or unknown key, a
wrong type, an unknown enumeration value is a violation (`request.invalid` in Go,
`contract.invalidDto` in TypeScript). The bridge treats binding results as `unknown` and never
trusts the generated classes. The decoders are hand-written: no new dependency.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| 64-bit values as JSON numbers | Simple | Silent loss above 2^53 − 1 (a 9 PB size, a long-running revision) | Violates the precision criterion |
| Only values above 2^53 as strings | Fewer strings | Two representations per field; every reader must branch | Ambiguous |
| AppError object through the Wails error | Natural in Go | The Wails runtime keeps only text: `"[object Object]"` | Does not survive the transport |
| zod/valibot decoders | Less code | A new dependency under the 14-day policy for a few small decoders | Not needed; hand-written decoders are 100% covered |
| Trusting generated model classes | No code | No validation; `createFrom` accepts anything | Violates STACK-01 |
| Persisting the ledger in 005 | Survives restart | Needs the SQLite repository (014/022) | Restart semantics are defined instead (`runtimeID` in `notFound`) |
| Go generates `operationID` | One generator | A lost response leaves the frontend without the ID to retry with | The ID must exist before the call |

## Verified grounds

Report: [005](../verification/005-domain-dto-and-bridge-contract.md). Apple M4, macOS 27.0.1,
Go 1.27.2, Wails v2.16.0, Node 26.11.1, TypeScript 7.0.2.

- `go tool devtool check` → `GATE PASSED` on the final tree (100% Go and TypeScript coverage,
  bindings in sync).
- 57 shared JSON examples (`internal/app/contracts/testdata/manifest.json`) and one vocabulary
  file (`vocabulary.json`, compared by both suites): Go round-trips every
  outgoing valid example exactly and validates every incoming one to the expected field and
  reason; TypeScript decodes every outgoing example and fails each invalid one at the expected
  path; the TypeScript encoders produce the incoming examples — `pass`.
- Boundaries `"0"`, `"9007199254740991"`, `"9007199254740992"`, `"18446744073709551615"` accepted;
  `"18446744073709551616"`, negative, leading zero, exponent rejected in both languages — `pass`.
- Ledger under `-race`: 20 concurrent duplicates run the mutation once; conflict; cancel before and
  after commit; shutdown waits and reports a mutation that ignores cancellation — `pass`.
- Real Wails runtime (`go tool wails dev`, headless Chrome): `GetLibrarySnapshot` resolved with a
  string revision; `GetOperation` rejected with `Error.message` = the `operation.notFound` and
  `request.invalid` JSON; the own bridge returned `revision: 0n` and the decoded AppErrors — `pass`.
- Not proven here: the packaged `.app` calling these methods from UI (no UI calls them before
  034/035; the `.app` uses the same `desktop/calls.js`); event emission (027); the store (035).

## Consequences

- New facade methods take request DTOs from `internal/app/contracts` and start them through the
  single acceptance path (see Operations); they return `error` as `*contracts.AppError` (never a
  typed nil). The runtime ID has one owner, the ledger (`app.New(ledger)`, `ledger.RuntimeID()`).
- New React code reaches Go only through wrappers in `frontend/src/bridge` built on `callGo` and a
  decoder; it receives `Result<T>`, never a rejected promise.
- Wails logs a whole call message, arguments included, when it cannot parse or dispatch it
  (`dispatcher.go:46`): paths now, magnet links later (DATA-02). Local diagnostics (017) set an own
  `options.App.Logger` that drops or redacts those lines. The error formatter quotes the cause it
  writes (`%q`), so torrent-controlled text cannot forge a log line, and treats a typed-nil
  `*AppError` as an unexpected error.
- Go never fails to encode a DTO: Wails exits on an unencodable result, so a stored time outside
  years 0–9999 encodes as the zero time instead.
- Torrent-controlled text (`name`, file `path`) is accepted by the decoders as is — rejecting it
  would make one hostile torrent hide the whole library. The engine adapter cleans relative paths
  (019) and the UI renders names isolated from bidi controls (045, 046).
- Known limit (Wails v2.16.0): when Wails' own argument parsing fails, the dev server sends the
  error callback without its `c` prefix (`devserver.go:207`), so in `wails dev` the call never
  settles; the desktop `.app` rejects with plain text (`bridge.callFailed`). Our encoders never send
  a wrong type. Recheck on a Wails upgrade.
- A Wails upgrade must keep `ErrorFormatter`, the `ts_type` tag and the rejection with
  `new Error(text)`; the tests of `ErrorFormatter` and of `fromRejection` pin the behavior we rely on.
- Adding a field, an enumeration value or an error code is a contract change: update the Go type,
  the TypeScript decoder, the shared examples and this ADR together.
