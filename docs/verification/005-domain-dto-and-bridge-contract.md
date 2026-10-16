# Report 005: our own Go–TS contract and errors

**Task:** [005](../../product/tasks/005-domain-dto-and-bridge-contract.md)  
**Date:** 15 October 2026  
**Change version:** commits `44877f2` (A), `b11e3a2` (B), `dfbc15c` (C), `497d6d9` (D) and the stage E commit after `ae04509`  
**Feature:** [bridge-contract](../features/bridge-contract.md); **ADR:** [api-contract](../decisions/api-contract.md)

## Session architecture plan

Approved by the owner on 15 October 2026 after two self-review passes.

- **Goal:** fix the Go–TypeScript contract: identifiers, units, 64-bit numbers as decimal strings,
  DTOs for the library, torrent, files, import preview and settings, `AppError` and its transport
  through Wails, accepted-versus-completed operations with an idempotent `operationID`, and the
  `runtimeID` + revision rules. Real services stay with 013/018/022–027/034/035.
- **Boundaries:** new Go package `internal/app/contracts` (DTOs, validation, strict request
  decoding) and `internal/app/operations` (the ledger, sole owner of operation goroutines); the
  facade gains `GetLibrarySnapshot`, `GetOperation` and `FormatError`; `main.go` wires
  `ErrorFormatter` and `OnShutdown`. TS: `frontend/src/bridge/contract/*` (decoders, errors, pure
  ordering rules) and own wrappers `bridge/library.ts`, `bridge/operations.ts`. Shared examples in
  `internal/app/contracts/testdata`, read by Go and TS tests.
- **Key findings before coding:** the Wails v2.16 JS runtime rejects with `new Error(message.error)`
  (`internal/frontend/runtime/desktop/calls.js:181`), so `ErrorFormatter` returns the AppError's JSON
  text; `encoding/json` fills missing request fields with zero values, so requests record missing,
  null and unknown keys themselves.
- **Stages:** A — Go contracts; B — ledger, facade, `main.go`, bindings, real round-trip through
  `wails dev`; C — TS decoders, errors, wrappers; D — TS ordering rules; E — documents, review,
  `done`.
- **Tests first:** see each stage below; RED is recorded before the implementation.
- **Coverage:** `go tool devtool check` (Go unit + `.app` profiles, TS four metrics by the
  independent inventory).

## Stage log

| Stage | State |
|---|---|
| A — Go contracts | done |
| B — ledger, facade, bindings | done |
| C — TS decoders and wrappers | done |
| D — TS ordering rules | done |
| E — review fixes, documents and acceptance | done |

## Stage A — Go contracts

- RED: `go test -race ./internal/app/contracts` with only the tests and `testdata/` written →
  `fail` (build failure: undefined `Request`, `Fingerprint`, `NewUUID`, the DTO types and the
  `outgoing`/`incoming` registries' targets). Expected: the package API did not exist.
- GREEN: the same command → `pass`; `go test -race -coverprofile` → 100.0% of statements, no zero
  block. `gofmt -l internal` empty, `go vet ./internal/app/...` clean.
- Shared examples: 51 JSON files in `internal/app/contracts/testdata` listed in `manifest.json`
  (valid and invalid, both directions); Go checks every `toFrontend` valid example by exact
  round-trip and every `toGo` example by `Validate` (field and reason).
- Refactoring: one strict decoder (`decodeFields`) for every request and `Settings`; one
  `marshalDecimal`/`unmarshalDecimal` pair for the four 64-bit types; validation helpers
  (`checkUUID`, `checkTorrentIDs`, `checkFileIndexes`, `checkPositive`, `checkAbsolute`) shared by
  all requests. Uncovered branches found at 95.6% were covered by new examples (empty and bad
  torrent IDs, preview version 0, relative destination, record-only with a list version, unknown
  theme, zero download limit) and an `OperationState.Finished` test, not by removing checks.

## Stage B — ledger, facade, `main.go`, bindings

- RED: `go test -race ./internal/app/operations` → `fail` (undefined `Ledger`, `NewLedger`);
  `go test ./internal/app` → `fail` (`New` takes no arguments, no `GetLibrarySnapshot`);
  `go vet .` → `fail` (`run` and `app.New` signatures). Expected: the API did not exist.
- GREEN: `go test -race -count=3 ./internal/app/operations` → `pass`, 100.0%;
  `go test -race ./internal/app/...` and `go test -race .` → `pass` (`main()` itself is covered only
  by the `.app` run of the full gate). `go tool wails generate module` regenerated
  `App.d.ts`, `App.js` and `models.ts`: every 64-bit field is `string` there.
- `go tool devtool check fast` → `GATE INCOMPLETE` (as designed: `.app` coverage run and network
  steps `not_run`); every executed step `pass`, bindings in sync.
- Real Wails round-trip (`go tool wails dev`, headless Chrome on `http://localhost:34115`,
  expressions evaluated through the DevTools protocol in the page):
  - `window.go.app.App.GetLibrarySnapshot()` resolved
    `{"runtimeID":"058fa117-…","revision":"0","torrents":[]}` — `pass`;
  - `GetOperation(<random UUID>)` rejected with an `Error` whose `message` is
    `{"code":"operation.notFound",…,"params":{"runtimeID":"058fa117-…"}}` — `pass`;
  - `GetOperation('not-a-uuid')` rejected with the `request.invalid` JSON (`field` `operationID`,
    `reason` `format`) — `pass`;
  - `GetOperation(42)` (a wrong JSON type, rejected by Wails' own argument parsing) never settled in
    the dev browser. Cause, read in Wails v2.16.0: `NewErrorCallback` returns the callback without
    the `c` prefix; the desktop frontend (`desktop/darwin/frontend.go:428`) delivers it anyway, so
    the `.app` rejects with plain text (→ `bridge.callFailed`), but the dev server
    (`devserver.go:207`) writes it unprefixed and the browser runtime ignores it. Consequence: the
    TypeScript encoders must never send a wrong type (stage C), and the limitation is recorded in
    the ADR. Not a defect of our code; not fixed upstream here.
- The packaged `.app` exercises the same `desktop/calls.js`; a click-through needs a UI that calls
  these methods (034/035).
- Refactoring: `contracts.CheckUUID` exported and reused by the facade instead of a second UUID
  check; lifecycle (`stopOperations`, 5 s bound) kept in `main.go` because every exported method of
  `App` is bound to JavaScript.

## Stage C — TypeScript decoders, errors, wrappers

- RED: `npm --prefix frontend test -- src/bridge/contract` with only `examples.test.ts` and
  `decode.test.ts` written → `fail` (both files: the imported modules `./errors`, `./library`,
  `./decode`, … did not exist). Expected.
- GREEN: `npm --prefix frontend run test:coverage` → `pass`, 100 tests, 16 own files at 100% of
  statements, branches, functions and lines; `npm --prefix frontend run typecheck` and
  `npm --prefix frontend run lint` (type-aware) → `pass`.
- The manifest test decodes every `toFrontend` example of `internal/app/contracts/testdata` (valid
  ones decode, invalid ones fail at the manifest's `path`); the command encoders produce exactly
  the `toGo` examples the Go tests accept. One set of examples for both languages.
- Real Wails round-trip through the own bridge (`go tool wails dev`, headless Chrome, modules
  imported by the page from the Vite dev server):
  - `getLibrarySnapshot()` → `{ok: true, value: {runtimeID: "daf9def6-…", revision: 0n, torrents: []}}`
    (a `bigint`) — `pass`;
  - `getOperation(crypto.randomUUID())` → `{ok: false, error: {code: "operation.notFound", …,
    params: {runtimeID: "daf9def6-…"}}}` — `pass`;
  - `getOperation('not-a-uuid')` → `request.invalid` with `field` `operationID`, `reason`
    `format` — `pass`.
- Refactoring: one `callGo` for every binding (rejection → AppError, answer → decoder); `struct`
  derives the key set from the decoders, so a field is declared once; type assertions replaced by
  type guards (the type-aware lint forbids unsafe assertions), with one documented `asserts`
  function where TypeScript cannot follow the loop in `struct`; the shared example loader lives in
  `src/test/contractExamples.ts` and validates the manifest instead of casting it.

## Stage D — TypeScript ordering rules

- RED: `npm --prefix frontend test -- src/bridge/contract/ordering.test.ts` → `fail` (cannot resolve
  `./ordering`). Expected.
- GREEN: the same → `pass`, 22 tests; `npm --prefix frontend run test:coverage` → `pass`, 122 tests,
  every own file at 100% on all four metrics; typecheck and type-aware lint → `pass`.
- Covered decisions: the answer to the latest fetch applies, an older one is discarded; a revision
  jump in a full snapshot applies without a fetch; an older snapshot of the same runtime is
  discarded; revision 0 of a new runtime applies and retires the previous one, whose later
  messages (even with a higher revision) are dropped; a delta applies only on exactly the current
  revision, an older one is discarded, a gap fetches once and nothing more while that fetch is in
  flight; a failed fetch allows the next one; operation states only move forward;
  `operation.notFound` from the accepting runtime means "not accepted" (resend with the same ID is
  safe), from another runtime "outcome unknown" (read the full state, never resend).
- These are pure functions; the store that calls them is task 035, so the behavior in the running
  application is `not_run` until then.

## Stage E — review, fixes, documents, acceptance

`/branch-review` (code-reviewer, security-reviewer, tech-lead) on `ae04509..HEAD` plus the stage E
documents: no critical finding. Every finding was fixed in code or recorded with its owning task;
none was dismissed.

Fixed in code (RED first unless noted):

| Finding | Fix | RED |
|---|---|---|
| A nil list encodes as `null`, a non-UTC time passes; the TS decoder rejects both | `contracts/marshal.go`: outgoing DTOs encode nil lists as `[]`, nil `params` as `{}`, times in UTC; a year outside 0–9999 becomes the zero time (Wails exits on an unencodable result) | `TestZeroValuesEncodeAsEmptyListsAndUTCTimes`: 8 `null`/zone failures, then `year outside of range` |
| Go and TS vocabularies kept in sync only by convention | `testdata/vocabulary.json`; Go `TestEnumerationsAndErrorCodesMatchTheSharedVocabulary`, TS `vocabulary.test.ts`; enumeration lists exported in Go, TS lists moved to `bridge/contract/enums.ts` | build failure (lists undefined); TS: `./enums` missing |
| `describeError` found `Object.prototype` members for codes such as `constructor` | catalogue moved to `errorMessages.ts` as a `Map` | `./errorMessages` missing |
| `bridge.callFailed` diagnostic text reached the message params | `describeError` drops them for that code | `params` was `{detail}` |
| The formatter dropped the cause of unexpected errors | `app.ErrorFormatter(stderr)` writes it, quoted (`%q`, no forged lines); a typed-nil `*AppError` is unexpected | undefined `ErrorFormatter`; then unquoted text, a forged line, `null` |
| Liveness: the awaited fetch answered by a runtime retired meanwhile left nothing applied and nothing fetching | `receiveSnapshot` fetches again; `needsFetch(state)` tells the store to fetch (with back-off, 035) | discard instead of fetch; `needsFetch` missing |
| An operation cancelled before its goroutine ran still ran its mutation | `Ledger.begin` ends it `cancelled` without running; `spawn` seam orders the test | `l.spawn` undefined; then the mutation ran |
| Runtime ID had two owners | `app.New(ledger)`, `ledger.RuntimeID()`; `run(start, stderr, ledger)` | compile errors of the changed signatures |
| Paths checked only with `filepath.IsAbs` | `checkFolderPath`: clean, below `/`, no control characters, ≤ 1024 bytes | 4 new examples accepted |
| Deletion confirmed by a made-up integer | `deletionPlanID` (Go-issued UUID) replaces `deletionListVersion` | 6 example failures |
| Unknown key echoed at any length | shortened to 64 characters | example with a 200-character key |
| `crypto.randomUUID` needs a secure context | `newOperationID` builds the UUID from `crypto.getRandomValues` | `randomUUID` called 200 times |
| Decimal text parsed at any length | longer than 20 digits → `range` before `BigInt` | none: the existing result was already `range`; the guard bounds work only |
| `withOperation` shared the mutation's `params` map | `maps.Clone` | written together with the fix (no separate RED) |
| Seven copies of the same `UnmarshalJSON` body | `decodeFields` resets its target and is called with the request itself | refactoring under the green example suite |
| `stateRank` repeated the state order | derived from `operationStates` | refactoring under the green ordering suite |

`commands.test.ts` was written together with `commands.ts` in stage C (no separate RED for that
module).

Recorded for owning tasks (ADR and the tasks' "Decisions, risks and boundaries"): ordering keys in
`Ledger.Start` for STACK-03 (013); one acceptance path `accept(kind, request, mutation)` as a merge
condition (024/025); pending operations after a reload (027/035); panic recovery and the ordered quit
(028); partial results of deletion and import commit (023/031/032); deletion plan in the UI (049);
destination only from the dialog or the saved default (034); symlinks and a writable directory
(020); clean torrent paths (019) and bidi-safe names (045); Wails' own logger redaction (017); a cap
on ledger entries (064).

### Results of the final state

- Go: `go test -race ./internal/app/...` → `pass`; `internal/app`, `contracts`, `operations` at
  100.0% of statements in unit runs.
- TS: `npm --prefix frontend run test:coverage` → `pass`, 139 tests, every own file at 100% of
  statements, branches, functions and lines; `typecheck` and type-aware `lint` → `pass`.
- Shared examples: 57 entries in `manifest.json` plus `vocabulary.json`.
- Real Wails round-trip on the final code (`go tool wails dev`, headless Chrome, own bridge
  modules): `getLibrarySnapshot` → `revision: 0n`; `getOperation` of a random ID and of an ID from
  `newOperationID` → `operation.notFound` with the runtime ID (the generated ID is accepted by Go's
  validation); `getOperation('not-a-uuid')` → `request.invalid` — `pass`.

- Full gate, final tree: the first `go tool devtool check` → `GATE FAILED` (`main_test.go` still
  expected the unquoted cause after the formatter switched to `%q`); test fixed, the second run →
  `GATE PASSED`: 19 own TS files at 100% on all four metrics, every block of every own Go file
  covered (unit tests + instrumented `.app` launched and quit), bindings in sync (6 files),
  type-aware lint, escape hatches, docs, plan, release age, `npm audit`, `govulncheck`, updates —
  all `pass`; engine integration (007, 018) and test-bridge UI tests (035) `not_run`. Only this
  report's text changed after that run.

### Contract tests, test bridge and real service (kept separate)

| Level | Result |
|---|---|
| Contract tests (Go and TS on the shared examples, ledger, ordering rules) | `pass` |
| Real Wails runtime through `wails dev` (same `desktop/calls.js` as the `.app`) | `pass` |
| Packaged `.app` calling the new methods from the UI | `not_run`: no UI calls them before 034/035; the gate launches and quits the `.app` (coverage of `main`) |
| Test bridge for browser UI tests | `not_run`: built by 035 |
| Real services behind the methods (library, publisher, store) | `not_run`: 027, 034, 035 |

### Debt check

No stub, no ignore directive, no skipped test. The two facade methods answer truthfully for the
current application. Open items are additions owned by named tasks (listed above), not cleanup left
in this change.
