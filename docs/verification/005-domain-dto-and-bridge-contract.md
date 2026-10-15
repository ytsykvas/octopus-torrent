# Report 005: our own Go–TS contract and errors

**Task:** [005](../../product/tasks/005-domain-dto-and-bridge-contract.md)  
**Date:** 15 October 2026  
**Change version:** stage commits after `ae04509`  
**Feature:** bridge-contract (stage E); **ADR:** api-contract (stage E)

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
| D — TS ordering rules | planned |
| E — documents and acceptance | planned |

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
