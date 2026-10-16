# Go–TypeScript bridge contract

**Status:** `implemented` (contract, ledger and pure rules; real services come with later tasks)  
**Requirements:** `AI-05`, `AI-02`, `STACK-01`, `STACK-02`, `STACK-03`  
**Related tasks:** [005](../../product/tasks/005-domain-dto-and-bridge-contract.md); users: 013, 024, 025, 027, 031, 034, 035, 050  
**Last verified version:** task 005 commits `44877f2`…stage E commit, full gate in the [report](../verification/005-domain-dto-and-bridge-contract.md)

## Goal and behavior

React and Go exchange only the product's own DTOs, with fixed identifiers, units and error codes,
so the two sides can be built separately against shared examples. Decisions and their reasons:
ADR [api-contract](../decisions/api-contract.md).

Implemented now:

- `GetLibrarySnapshot()` — this runtime's ID, revision 0 and no torrents (the library is 034);
- `GetOperation(operationID)` — the status recorded by the operation ledger, or
  `operation.notFound` naming the current runtime;
- every facade error crosses the bridge as the JSON text of an `AppError`;
- quitting the application cancels running operations and waits for them (at most 5 s).

Planned: command methods (024/025/031/050), event emission (027), the store calling the ordering
rules (035).

## Architecture analysis before the change

- Reused: the 003 chain (feature → own bridge → generated binding → facade) and the `run(start, …)`
  pattern of `main.go`. No other code existed.
- New boundaries: `internal/app/contracts` (types, validation, examples; no dependencies on
  services), `internal/app/operations` (the ledger, the only owner of operation goroutines),
  `frontend/src/bridge/contract` (decoders, encoders, error mapping, ordering — pure), wrappers
  `frontend/src/bridge/{library,operations}.ts` over `callGo`.
- Lifecycle: the ledger's root context is cancelled by Wails `OnShutdown`; `Shutdown(ctx)` waits for
  every goroutine or reports that some did not stop. The lifecycle hook lives in `main.go`, not on
  `App`, because every exported method of `App` is bound to JavaScript.
- State owner: Go. The frontend keeps no copy of operation results except what Go returns.

## Contracts and data

See the ADR for the field tables, enumerations, error codes and ordering rules. Shared examples:
`internal/app/contracts/testdata/*.json` with `manifest.json` (file, DTO type, direction, valid or
the expected path / field and reason). Both test suites read them.

## Scenarios and TDD

| Scenario | Test | RED |
|---|---|---|
| Valid examples round-trip in Go; invalid requests name field and reason | `internal/app/contracts/examples_test.go`, `values_test.go` | build failure: package API undefined |
| Same `operationID` runs once; conflict; cancel; shutdown | `internal/app/operations/ledger_test.go` | undefined `Ledger` |
| Snapshot and operation methods; `ErrorFormatter` JSON and diagnostics | `internal/app/app_test.go` | `New` takes no arguments |
| Formatter and shutdown wired into Wails | `main_test.go` | `run` signature |
| TS decodes every example; invalid at the expected path; exact bigints | `frontend/src/bridge/contract/examples.test.ts` | modules missing |
| Primitives, numbers, IDs, cross-field rules | `decode.test.ts`, `values.test.ts` | modules missing |
| Rejection → AppError; unknown code → fallback | `errors.test.ts` | modules missing |
| Encoders produce the Go examples | `commands.test.ts` | — (written with the module) |
| Ordering of snapshots, deltas, operations | `ordering.test.ts` | `./ordering` missing |
| Wrappers: decode, invalid DTO, rejection | `bridge/library.test.ts`, `bridge/operations.test.ts` | modules missing |

## Implementation and reuse

```text
internal/app/contracts/        DTOs, enums, numbers, ids, AppError, strict requests, Fingerprint, testdata/
internal/app/operations/       Ledger: Start, Get, Cancel, Shutdown
internal/app/app.go            GetLibrarySnapshot, GetOperation, ErrorFormatter
main.go                        ErrorFormatter(stderr), OnShutdown → stopOperations
frontend/src/bridge/contract/  decode, numbers, ids, enums, errors, errorMessages, library,
                               importPreview, settings, operations, commands, ordering
frontend/src/bridge/call.ts    callGo: binding call → Result<T>
frontend/src/bridge/library.ts, operations.ts   the only callers of the new bindings
```

Invariants: `struct` declares each field once and rejects missing and unknown keys; one
`decodeFields` decodes every Go request; the facade never returns a typed nil `*AppError` as
`error`.

## Checks and 100% coverage

`go tool devtool check` (the gate): Go unit + `.app` profiles with no zero block, TypeScript four
metrics per own file, bindings regenerated without diff, type-aware lint. Per-stage results and the
real Wails round-trips are in the [report](../verification/005-domain-dto-and-bridge-contract.md).
Real services: `not_run` until 027/034/035.

## Refactoring and debt check

`contracts.CheckUUID` is shared by requests and the facade; one decimal codec for four Go types;
`callGo` for every wrapper; type assertions replaced by type guards. No stub or deferred cleanup:
the two facade methods answer truthfully for the current application.

## Usage and recovery

A new command: request DTO in `contracts` (+ examples + manifest), `Validate`, a facade method that
starts it through the ledger, regenerate bindings, a wrapper on `callGo`, an encoder matching the
example. An `operation.notFound` from another runtime: show the full state, do not resend.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-15 | Contract, ledger, two facade methods, TypeScript decoders, wrappers and ordering rules | [report 005](../verification/005-domain-dto-and-bridge-contract.md), ADR [api-contract](../decisions/api-contract.md) |
