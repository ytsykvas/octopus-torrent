# Adding a UI → Go command

> Last updated: 2026-10-15

A step-by-step recipe for a new mutation that goes from a button to a Go service. Every step
starts with a failing test (RED → GREEN → REFACTOR). The worked example is a hypothetical
`setFilesWanted`; its request DTO already exists.

## 1. The request in Go — `internal/app/contracts`

- Define a struct with `json` tags and an unexported `shape shape` field. Every tagged field is
  required, and `null` is allowed only for pointer fields.
- Add strict decoding, so a missing `wanted` is an error, not `false`:

  ```go
  func (r *SetFilesWantedRequest) UnmarshalJSON(data []byte) (err error) {
  	r.shape, err = decodeFields(data, r)
  	return err
  }
  ```

- `Validate()` returns the first problem in field order: `r.shape.problem("")`, then
  `CheckUUID`, `checkFileIndexes`, `checkFolderPath`, and so on.
- `normalized()` drops the operation ID and sorts/deduplicates ID sets, so `Fingerprint`
  recognizes a retry.
- A new 64-bit field uses `ByteCount` / `BytesPerSecond` / `DurationSeconds` / `Revision` and
  carries `ts_type:"string"`. A reflection test fails without it.
- A new enumeration value or error code goes into the Go list, `testdata/vocabulary.json` and the
  TypeScript side in the same change: `frontend/src/bridge/contract/enums.ts` for enumerations, the
  `fallbacks` map in `errorMessages.ts` for codes. Both suites compare them.

## 2. Shared examples — `internal/app/contracts/testdata`

- Add valid and invalid JSON examples and list each in `manifest.json`:
  - request: `{"file", "type", "direction": "toGo", "valid": true}`, or `"valid": false` with
    `"field"` and `"reason"`;
  - DTO Go sends: `{"file", "type", "direction": "toFrontend", "valid": true}`, or `"valid": false`
    with the TypeScript `"path"`.
- Register the type in Go: `incoming` (or `outgoing`) in `examples_test.go`.
- Register it in TypeScript: a new outgoing DTO's decoder goes into `decoders` in
  `frontend/src/bridge/contract/examples.test.ts` (the test requires every manifest type to have
  one), and a new request gets a case in the table of `commands.test.ts`.

Go validates every example. TypeScript decodes every `toFrontend` example (invalid ones must fail at
`path`), and its listed encoder cases must produce their `toGo` examples.

## 3. The service and the mutation

The mutation is `func(ctx) *contracts.AppError`:

- return `nil` when the change took effect;
- return `operation.cancelled` only when nothing took effect;
- return any other code for a failure.

If the mutation can be partly done, do not make it cancellable once it runs; report a partial result
as `failed`. See [operation ledger](../app/operations.md).

## 4. The facade method — `internal/app`

```go
func (a *App) SetFilesWanted(r contracts.SetFilesWantedRequest) (contracts.OperationStatus, error)
```

Start it through the single acceptance path (`accept(kind, request, mutation)`, added by the first
command in 024/025): validate, take the operation ID from the request, fingerprint, `ledger.Start`.
Return a nil `error` explicitly, never a typed-nil `*AppError` ([facade](../app/facade.md)).

## 5. Bindings

Run `go tool wails generate module`, then `git checkout -- frontend/wailsjs/runtime`. Commit
`frontend/wailsjs/` with the Go change.

## 6. The encoder and the wrapper — `frontend/src/bridge`

- `contract/commands.ts`: a request interface and an encoder that writes exactly the Go keys
  (bigint → `encodeDecimal`).
- `<area>.ts`: the generated binding takes the generated model class, so build it from the encoded
  object without a cast:
  `callGo(() => SetFilesWanted(contracts.SetFilesWantedRequest.createFrom(encodeSetFilesWanted(r))), operationStatus)`
  (`createFrom` copies the keys; Go still decodes them strictly).
- Tests: the encoder deep-equals the shared example; the wrapper is tested with a stubbed
  `window.go` ([calling Go](calling-go.md)).

## 7. The UI

- The component receives the bridge function as a prop. `App.tsx` (later the store) wires the real
  one.
- Make one `newOperationID()` per user intention and reuse it when retrying.
- Show `describeError(result.error)`. Read completion with `getOperation` or from the full state,
  never only from an event.

## 8. Documents and the gate

- Update [contract](contract.md) and the ADR table for new codes and fields. Update the feature
  document of the domain and [docs/README.md](../README.md).
- Run `go tool devtool check` → `GATE PASSED`.
