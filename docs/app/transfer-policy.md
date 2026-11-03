# Transfer policy: permissions, display status and the order of commands for one torrent

**Status:** `implemented` (policy, registry and command order; the product links only the order, through the ledger, until [025](../../product/tasks/025-pause-resume-application.md))  
**Requirements:** `TR-01`, `TR-03`, `TR-08`, `STACK-03`, `AI-04`  
**Related tasks:** [013](../../product/tasks/013-transfer-policy-state-machine.md); next [018](../../product/tasks/018-torrent-engine-adapter.md), [023](../../product/tasks/023-durable-import-commit.md), [025](../../product/tasks/025-pause-resume-application.md), [027](../../product/tasks/027-metrics-status-snapshots.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md), [031](../../product/tasks/031-delete-record-lifecycle.md), [053](../../product/tasks/053-global-upload-policy-production.md)  
**See also:** [ADR transfer-status-model](../decisions/transfer-status-model.md), [operation ledger](operations.md), [transfer switches](transfer-switches.md)  
**Last verified version:** branch `task/013-transfer-policy`  
**Last updated:** 2026-11-03

## Goal and behavior

```
UI command (pause / resume / settings / remove)                          [025, 053, 031]
  └─ ledger.Start(id, kind, fp, keys, mutation)     keys: Torrent(id)… or Settings
       └─ commandorder: the turn joins every key's queue now (acceptance order)
            └─ mutation (holds its keys), for every torrent of the request:
                 CheckIntentChange → persist [015] → registry change → Reconcile
                 └─ Reconcile: Lookup now → Decide(input) → Switches.Apply [018] → Record(rev)
                      (the revision moved meanwhile → apply again until one is recorded)
snapshot [027]: registry.All() → DisplayStatus(input, progress), Decide(input).Upload
```

For every torrent the application answers three questions from one place:

- **May it download, may it upload?** `transfer.Decide(Input) Permissions`, from the manual pause,
  the global upload stop, the phase (ready, checking, relinking, removing, waiting for metadata),
  a payload fault and quitting. Download and upload are independent: the global stop never touches
  download, and a manual pause stops both.
- **What does its card say?** `transfer.DisplayStatus(Input, Progress)`, one status by a fixed
  priority.
- **In what order do commands take effect?** Commands for one torrent (or for the settings) run
  in the order Go accepted them; the engine always gets the latest state, so a slow command, a late
  answer or a command queued behind a removal never brings back an old state or a removed torrent.

Both tables, with the reason for every row, are in the
[ADR](../decisions/transfer-status-model.md); a test keeps them equal to the code.

## Architecture analysis before the change

- **State owner:** `transfer.Registry`, one per runtime, an in-memory mirror of what the service
  already made durable (SQLite, 015) plus runtime-only phase and fault. React only shows `status`,
  `manualPaused` and `uploadAllowed` from the snapshot; it never computes them (no copy of the rule
  in TypeScript).
- **Order owner:** `commandorder.Order`, one per runtime, created in `main.go` (the `order`
  variable) and given to the ledger; the registry and the service (025) get the same one, so their
  own steps (fan-out, nudges, observations) share each torrent's queue with the operations.
  `NewRegistry(order, …)` and `Order.Holds` accept only keys of that order: a second `Order` is
  `ErrNotHeld`, never silent.
- **No side effects in the policy:** `Decide` and `DisplayStatus` are pure; the registry touches
  nothing but its memory; the engine is reached only through the `Switches` port (the adapter, 018).
  A test proves that `internal/transfer` and `internal/commandorder` reach no Wails, engine, SQLite,
  file or socket package.
- **Reused:** the status vocabulary and `AppError` from `contracts`; the ledger of 005 (now with
  keys); `transferswitch` stays the engine side (018 wraps it in `Switches`).

## Contracts and data

| Package | API |
|---|---|
| `internal/commandorder` | `Key`; `Settings`; `Torrent(id)`, `Torrents(ids)`; `New()`; `(*Order).Join(keys) (*Turn, error)`; `(*Turn).Wait(ctx)`, `Leave()`, `Holding(ctx)`; `(*Order).Holds(ctx, key)`; `(*Order).Run(ctx, keys, step)`; `ErrMixedLevels`, `ErrHeld`, `ErrLeft` |
| `internal/app/operations` | `NewLedger(runtimeID, order)`; `Start(id, kind, fingerprint, keys, mutation)` |
| `internal/transfer` | `Phase`, `Input`, `Permissions`, `Decide`; `Progress`, `DisplayStatus`; `Registry` (`NewRegistry(order, globalUploadDisabled)`, `Add`, `CheckIntentChange`, `SetManualPaused`, `SetPhase`, `SetPayloadFault`, `BeginRemoval`, `Forget`, `SetGlobalUploadDisabled`, `BeginShutdown`, `Record`, `Lookup`, `All`, `Reconcile`), `Entry`, `View`, `PolicyRevision`, `Switches`; errors `ErrNotHeld`, `ErrUnknown`, `ErrRemoved`, `ErrRemoving`, `ErrNotRemoving`, `ErrExists`, `ErrInvalidPhase` |

- No DTO, binding or TypeScript change. The registry's refusals become `AppError` codes in the first
  facade command that returns them (025, 031).
- `PolicyRevision` counts a torrent's policy changes within one runtime; it is not the bridge's
  snapshot revision.
- A mutator needs the torrent's key in its context (`Torrent(id)`; `Settings` for the global
  value), else `ErrNotHeld`. Readers need none.
- The zero `Phase` is no phase: it never transfers and `Add` refuses it, so an import or a restore
  that forgets a field cannot start a transfer by accident.
- `SetGlobalUploadDisabled` returns the torrents whose permissions change and those the engine is
  not in step with (a failed or unfinished apply): saving the same value again retries them.

## Scenarios and TDD

| Scenario | Test | Level |
|---|---|---|
| Turns of one key run in join order; disjoint keys do not wait; a multi-key turn waits for every key; keys sorted and de-duplicated; a mixed-level set refused | `commandorder/order_test.go` | unit |
| A cancelled turn behind the head, or first in one queue only, leaves the order intact; `Leave` idempotent; an ended context is never granted; a turn that left is never granted again; a turn of another order does not count | `commandorder/order_test.go` | unit |
| 300 goroutines with crossing key sets: no deadlock, never two holders of a key | `TestCrossingKeySetsNeverDeadlockAndNeverShareAKey` | unit, `-race` |
| `Run` holds keys only while the step runs (context cancelled, `Holds` false after); nests only downward | `commandorder/order_test.go` | unit |
| Operations of one torrent run in acceptance order even when the first goroutine starts last; different torrents run together; a cancelled or shut-down queued operation never runs and releases the next; a retry joins no second turn; a mixed-level key set is refused and not recorded | `operations/ledger_test.go` | unit, `-race` |
| Every combination of inputs, the zero phase included, follows the permission table and the status table; every row reachable; requirement invariants (TR-01, TR-08, TR-03, both 013 criteria); the zero input transfers nothing; the ADR states both tables | `transfer/policy_test.go`, `transfer/status_test.go` | unit |
| The policy packages and every package of this module they reach import only listed standard packages (no Wails, engine, SQLite, file, socket or clock package) | `transfer/imports_test.go` | structural |
| Registry: keys (also of another order), refusals, revisions, removal and tombstones, the global change and its retry of torrents out of step, shutdown, `Record` compare-and-set, copies, `CheckIntentChange` changes nothing, `Reconcile` applies again when a global change or the quit overtakes it, and stops when its context ends | `transfer/registry_test.go` | unit |
| Seeded random commands through the real ledger end with the latest accepted intent and global value, applied and not pending; never transfer for a paused torrent | `TestConcurrentCommandsEndWithTheLatestAcceptedIntentAndGlobalValue` | composition, `-race`, engine test double |
| A torrent added before, or between the global change and its fan-out, gets the new value | `TestATorrentAddedAroundAGlobalChangeGetsTheNewValue` | composition |
| Resume → remove → resume: late answers discarded, the second resume refused, nothing asked of the engine after the removal's stop, no return | `TestALateAnswerAfterRemovalIsDiscardedAndTheTorrentNeverComesBack` | composition |
| A pause of several torrents with an unknown ID first pauses the others and refuses only that one | `TestABulkPauseHandlesEveryTorrentAndRefusesOnlyTheBadOne` | composition |
| A resume queued behind a removal whose deletion failed is refused before it is stored | `TestAResumeOfATorrentBeingRemovedIsRefusedBeforeItIsStored` | composition |

RED/GREEN commands, the mutation checks that prove the composition tests detect their defects, and
the coverage are in the [report](../verification/013-transfer-policy-state-machine.md).

## Implementation and reuse

- `internal/commandorder/order.go`: one mutex, one queue per key; a turn's channel is closed once,
  when it becomes first in all its queues or when it leaves; `Leave` re-checks only the queues it
  headed.
- `internal/app/operations/ledger.go`: `Start` joins the turn under the ledger's lock (lock order
  ledger → order), the goroutine waits, then runs the mutation with the holding context.
- `internal/transfer/policy.go`, `status.go`: the two tables as code.
- `internal/transfer/registry.go`: the state, the key checks, `Reconcile` (Lookup → Decide → Apply →
  Record, again while the revision moves).
- `internal/transfer/order_test.go`: the composition later tasks implement (pause/resume, settings
  fan-out, import, removal), reduced to the policy.

## Checks and 100% coverage

`go tool devtool check` measures `internal/commandorder`, `internal/transfer` and the ledger at 100%
of statements from their unit tests; the changed line of `main.go` is covered by the gate's `.app`
run. Commands and results: [report](../verification/013-transfer-policy-state-machine.md). Not run
here, by scope: a real engine applying the permissions (018, 025, 053) and persistence (015).

## Refactoring and debt check

- The ledger's "no ordering per torrent" limit is gone; its document and the api-contract ADR say
  what is implemented.
- A defensive branch in `commandorder` that could never run was removed (the readiness signal is
  structural).
- No copy of a policy rule exists outside `internal/transfer`; the TypeScript side has none.
- Open primitives for later tasks, not debt: the `Switches` implementation (018), the facade
  commands (024, 025, 031, 050), persistence (015).

## Usage and recovery

- Change a torrent only inside its turn: an operation with `Torrents(ids)` keys, or
  `order.Run(ctx, Torrents(ids), step)`; inside a step pass the step's own context.
- `CheckIntentChange`, then persist, then change the registry, then `Reconcile`.
- A command for several torrents handles every one; a refusal is that torrent's outcome.
- After a global change, reconcile each returned torrent with `order.Run`, in parallel, each bounded.
- Keep steps short: hashing, fetching and copying run outside every step under their own lifetime
  context; only the phase changes are steps.
- An engine side of `Switches` returns when its context ends and puts a deadline on every engine
  call.
- `ErrNotHeld` in a test or a log means a change outside its order: a bug, not a retry case.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-11-03 | Created in task 013 | [report 013](../verification/013-transfer-policy-state-machine.md), [ADR](../decisions/transfer-status-model.md) |
