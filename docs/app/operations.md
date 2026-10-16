# Operation ledger

> Last updated: 2026-10-15

`internal/app/operations.Ledger` runs every non-trivial mutation the window asks for **once per
operation ID**, and keeps its status readable afterwards. Accepting a command and completing it
are separate: the facade answers at once with `accepted`, and the frontend reads the final state
later with `GetOperation` (or sees it in the full state). So a response lost on the bridge never
hides the outcome, and a retry never repeats the change.

No facade command uses it yet. The first commands (024/025) start their mutations through it.
Today `GetOperation` reads it, and quitting the app stops it.

## Flow

```
frontend: operationID = newOperationID()  (once per user intention, reused on retry)
   └─ facade command(request)                                   [024/025+]
        ├─ request.Validate()          → request.invalid
        ├─ contracts.Fingerprint(req)  (SHA-256 of the normalized request)
        └─ ledger.Start(id, kind, fingerprint, mutation)
             ├─ same kind + same fp → return the stored status (nothing runs again)
             ├─ different kind / fp → operation.idConflict
             ├─ id unknown, after Shutdown → operation.cancelled
             └─ id unknown          → register "accepted", spawn goroutine → return accepted
        goroutine: cancelled already? → "cancelled" (mutation never runs)
                   else "running" → mutation(ctx) → "succeeded" | "failed" | "cancelled"
frontend: GetOperation(id) → ledger.Get(id) → status | operation.notFound{runtimeID}
```

## States

`accepted` → `running` → one of `succeeded`, `failed`, `cancelled`. States only move forward.
`error` is set exactly when the state is `failed` or `cancelled`, and it carries the operation ID.

| The mutation returns | Final state |
|---|---|
| `nil` | `succeeded` |
| an `AppError` with code `operation.cancelled` | `cancelled` |
| any other `AppError` | `failed` (a copy of the error with the operation ID) |

## Cancellation

`ledger.Cancel(id)` cancels the operation's context and returns the status at that moment (still
`accepted` or `running`); read the final state again with `Get`. No facade method exposes it yet:

- **still `accepted`:** it ends `cancelled`, and its mutation never runs;
- **`running`:** the mutation decides. It returns `operation.cancelled` only if nothing took effect,
  so a committed change keeps `succeeded`;
- **finished:** nothing changes.

A mutation that can be partly done is not cancellable once it runs. This covers removing files of
several torrents (031/032) and confirming an import (023). It finishes, and a partial result is
`failed` with a code that lists what was done.

## Restart and `operation.notFound`

The ledger lives in memory, one per run, identified by its `runtimeID` (a new UUID each time Go
starts). `operation.notFound` carries `params.runtimeID`:

- **the same runtime that accepted the operation:** it was never accepted, so resending with the
  same ID is safe;
- **another runtime:** Go restarted, and the outcome is unknown. Read the full state, and never
  resend because of this answer (`notFoundMeaning` in [ordering](../bridge/ordering.md)).

Persistence of import and deletion operations across runs comes with the Operations table (014).

## Shutdown

`main.go` wires Wails `OnShutdown` to `stopOperations`, which works like this:

1. `ledger.Shutdown(ctx)` with a 5 s deadline refuses new operations and cancels every operation
   context.
2. It then waits for all goroutines.
3. If a mutation ignores cancellation, it reports
   `OctopusTorrent: operations still running at quit: …` on stderr and lets the process exit.

The ledger is the only owner of operation goroutines.

## API

```go
ledger := operations.NewLedger(contracts.NewRuntimeID())
status, problem := ledger.Start(id, contracts.OperationPauseTorrents, contracts.Fingerprint(req),
	func(ctx context.Context) *contracts.AppError {
		// do the work; honor ctx before anything takes effect
		return nil
	})
status, problem = ledger.Get(id)
status, problem = ledger.Cancel(id)
err := ledger.Shutdown(ctx) // ctx's error when goroutines did not end in time
```

## Limits (owned by later tasks)

- **No ordering per torrent (STACK-03):** every operation gets its own goroutine. Task 013 adds
  ordering keys to `Start`.
- **No single acceptance path yet:** 024/025 add `accept(kind, request, mutation)` (validate,
  operation ID from the request, fingerprint, start). Facade methods must not repeat those steps
  by hand.
- **No cap:** there is no limit on entries or running operations (064). Entries are never evicted:
  an evicted ID would let a retry run again.
- **No recovery:** a panicking mutation stops the app. 028 recovers it and folds the ledger stop
  into the ordered quit.

Tests: `internal/app/operations/ledger_test.go`. They cover concurrent duplicates under `-race`,
conflicts, cancel before start and after commit, and a shutdown that waits or reports.
