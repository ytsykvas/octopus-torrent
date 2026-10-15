# Wails facade

> Last updated: 2026-10-15

`internal/app.App` is the only Go type the window can call. Wails binds **every exported method**
of it to JavaScript (`window.go.app.App.<Method>`), generates `frontend/wailsjs/go/app/App.{js,d.ts}`
and the models in `frontend/wailsjs/go/models.ts`. React never calls these directly: the own bridge
in `frontend/src/bridge` wraps them ([calling Go](../bridge/calling-go.md)).

## Flow

```
React feature ─ prop ─▶ frontend/src/bridge/<area>.ts   callGo(() => Generated(), decoder)
                          └─ frontend/wailsjs/go/app/App.js   window.go.app.App.Method(args)
                               └─ Wails dispatcher (JSON args → Go values, strict request decoding)
                                    └─ internal/app.App.Method
                                         ├─ validate (contracts.CheckUUID / Request.Validate)
                                         └─ delegate (operations.Ledger, later the services)
                               ◀─ result → JSON                     ─ resolve(result)
                               ◀─ error  → ErrorFormatter → AppError JSON text ─ reject(new Error(text))
```

## Bound methods

| Method | Answer | Errors | Since |
|---|---|---|---|
| `GetRuntimeInfo()` | `RuntimeInfo{goVersion, os, arch}` of the compiled binary | none | 003 |
| `GetLibrarySnapshot()` | `LibrarySnapshot` of this runtime: revision `"0"`, no torrents until the library exists (034) | none | 005 |
| `GetOperation(operationID)` | `OperationStatus` recorded by the [operation ledger](operations.md) | `request.invalid` (missing or malformed ID), `operation.notFound` with `params.runtimeID` | 005 |

DTO fields, units and error codes: [bridge contract](../bridge/contract.md) and ADR
[api-contract](../decisions/api-contract.md).

## Errors

- A method returns `error` as a `*contracts.AppError`. It never returns a typed-nil
  `*AppError` as `error` (that is a non-nil interface): write `if problem != nil { return x, problem }`
  and `return x, nil` separately.
- `app.ErrorFormatter(stderr)` is installed as `options.App.ErrorFormatter`. It turns the error into
  the **JSON text** of the `AppError`, because the Wails runtime rejects the promise with
  `new Error(text)` and would print an object as `[object Object]`.
- Any error without a code becomes `internal.unexpected`. Its cause is written to stderr as
  `OctopusTorrent: unexpected error: "<quoted text>"` and never reaches the UI. A typed-nil
  `*AppError` is treated the same way.
- Wails' own failures happen before the method runs: arguments it cannot parse, or an unknown
  method. They reject with plain text, and the bridge maps that to `bridge.callFailed`. In
  `wails dev` such a call never settles (a Wails v2.16 dev-server quirk), so the TypeScript encoders
  never send a wrong type. A request argument that is not a JSON object fails the same way (inside
  Wails' parsing), not as `request.invalid`.
- An unknown key is echoed in `request.invalid`'s `params.field`, cut to 64 characters plus `…`.

## Rules

- **Every exported method is public API.** Lifecycle hooks (startup, shutdown) stay out of `App`:
  `main.go` wires `OnShutdown` to `stopOperations`, which stops the ledger.
- Dependencies come in through `app.New(...)`. Today that is `app.New(ledger)`. The ledger owns the
  runtime ID, and the facade reads it with `ledger.RuntimeID()`.
- The facade re-validates everything (STACK-01), delegates, and returns own DTOs. It never returns
  engine objects, pointers into services, or channels.
- Arguments are plain JSON values or request DTOs from `internal/app/contracts`. Requests decode
  field by field, so a missing key is an error, not a zero value.

## Adding a method

1. Write the test in `internal/app/app_test.go` first, then the method.
2. Run `go tool wails generate module`. Commit the changed `frontend/wailsjs/` with the Go change.
   Wails also flips the file modes of `frontend/wailsjs/runtime/*`: restore them with
   `git checkout -- frontend/wailsjs/runtime`. The gate compares only content.
3. Add a wrapper in `frontend/src/bridge` ([calling Go](../bridge/calling-go.md)).

For a command (a mutation), follow the full recipe in [adding a command](../bridge/adding-a-command.md).

## Configuration

`main.go` → `run(start, stderr, ledger)`:

| `options.App` field | Value |
|---|---|
| `Title`, `Width`, `Height` | `OctopusTorrent`, 1024 × 768 |
| `AssetServer.Assets` | embedded `frontend/dist` |
| `Bind` | `[]any{app.New(ledger)}` (exactly one bound object) |
| `ErrorFormatter` | `app.ErrorFormatter(stderr)` |
| `OnShutdown` | `stopOperations(ctx, ledger, stderr)`, bounded by 5 s |
