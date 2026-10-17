# Calling Go from React

> Last updated: 2026-10-15

React reaches Go only through `frontend/src/bridge`. A wrapper there calls the generated binding,
treats the answer as untrusted `unknown`, decodes it against the [contract](contract.md), and
returns a `Result<T>`. A wrapper never rejects (except the 003 prototype `getRuntimeInfo`), so
components handle one shape for every outcome.

## Flow

```
component (bridge function received as a prop)
  └─ getOperation(id)                                    frontend/src/bridge/operations.ts
       └─ callGo(() => GetOperation(id), operationStatus) frontend/src/bridge/call.ts
            ├─ await binding()        frontend/wailsjs/go/app/App.js → Go
            │    └─ rejects → fromRejection(reason)        bridge/contract/errors.ts
            │         ├─ Error.message is AppError JSON  → that AppError
            │         └─ anything else                   → bridge.callFailed {detail}
            └─ resolves → decodeDto(decoder, answer)
                 ├─ matches the contract  → { ok: true,  value }
                 └─ violates it           → { ok: false, error: contract.invalidDto {path, reason} }
```

```ts
type Result<T> = { ok: true; value: T } | { ok: false; error: AppError }

const result = await getLibrarySnapshot()
if (!result.ok) return show(describeError(result.error)) // key + params + English fallback
result.value.revision // bigint
```

## Wrappers

| Function | Go method | Value |
|---|---|---|
| `getRuntimeInfo()` (`bridge/runtimeInfo.ts`) | `GetRuntimeInfo` | `RuntimeInfo`. This is the 003 prototype: it rejects instead of returning a `Result` |
| `getLibrarySnapshot()` (`bridge/library.ts`) | `GetLibrarySnapshot` | `Result<LibrarySnapshot>` |
| `getOperation(id)` (`bridge/operations.ts`) | `GetOperation` | `Result<OperationStatus>` |

Nothing outside `frontend/src/bridge` imports `frontend/wailsjs`. The generated model classes are
never trusted: their `createFrom` accepts anything.

## Decoders

`bridge/contract/decode.ts` has small strict decoders that throw `ContractViolation(path, reason)`.
`decodeDto` turns the violation into `contract.invalidDto`.

| Decoder | Accepts |
|---|---|
| `string`, `boolean` | a value of that JSON type (`null` → reason `null`) |
| `integer(min)` | a safe integer ≥ `min` |
| `decimal` (`numbers.ts`) | a 64-bit decimal string → `bigint` (no sign, no leading zero, ≤ 2^64 − 1) |
| `uuid`, `infohashV1`, `infohashV2` (`ids.ts`) | lower-case canonical formats |
| `oneOf(values)` | one value of a closed enumeration (`enums.ts`) |
| `timestamp` | RFC 3339 in UTC, kept as text |
| `nullable(d)`, `array(d)`, `stringMap` | `null` or `d`; a list of `d`; an object of strings |
| `struct({...})` | an object with **exactly** these keys. A missing key → `missing`, an extra one → `unknown` |
| `refine(d, check)` | `d`, then a check across fields |

A path names the field: `torrents[0].files[1].index`. A violation anywhere rejects the whole
answer. Go and the frontend ship in one binary, so a difference is a bug, not a version skew.

## Errors for the UI

`describeError(error)` (`bridge/contract/errorMessages.ts`) returns a message key, its parameters and
an English fallback:

- a known code → `error.<code>` with its parameters;
- an unknown code → `error.unknown` with `{ code }`;
- `bridge.callFailed` → no parameters, because the diagnostic text (local paths, Wails messages) is
  not for display.

The texts move to the dictionaries in 041. Codes and meanings are in the
[ADR table](../decisions/api-contract.md#errors).

## Sending data to Go

Requests are encoded by `bridge/contract/commands.ts`. Each encoder writes **exactly** the keys Go
decodes and turns `bigint` limits into decimal strings. Go rejects an unknown key, so an extra
runtime property never leaks. Operation IDs come from `newOperationID()` (`ids.ts`), once per user
intention. It is built from `crypto.getRandomValues` because `crypto.randomUUID` needs a secure
context.

## Testing a wrapper

Stub the global the generated binding calls with `stubGoMethod` (`src/test/goBinding.ts`; `setup.ts`
removes the stub after each test), and assert the `Result`:

```ts
stubGoMethod('GetOperation', () => Promise.resolve(example('operation-succeeded.json')))
await expect(getOperation(id)).resolves.toMatchObject({ ok: true, value: { state: 'succeeded' } })
```

`example(name)`, `record`, `decodedExample` and `manifest` come from `src/test/contractExamples.ts`.
They read the same JSON files the Go tests read ([testing](../development/testing.md)).
