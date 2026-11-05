# Transfer permissions, display status and the order of commands for one torrent

**Status:** `accepted`  
**Date:** 2026-11-03  
**Task:** [013](../../product/tasks/013-transfer-policy-state-machine.md)  
**Register item:** O11 ([baseline](project-baseline.md#6-open-decision-register)) — status priority and zero wanted; the filter intersection and section names stay with [055](../../product/tasks/055-search-status-filters.md) (R7)  
**Requirements:** `TR-01`, `TR-03`, `TR-08`, `STACK-03`, `AI-04`

## Question

A torrent has a stored user pause, a stored global upload stop, a file selection, and states the
application itself puts it in (checking its data, relinking, removing it, quitting). The pause,
resume, global upload, removal, restore and snapshot tasks (025, 053, 031, 029, 027) all need the
same answers:

- What may this torrent transfer: download and upload, each on its own?
- What one status does its card show when several conditions hold at once?
- In what order do concurrent commands for one torrent take effect, and how does a new global
  revision reach every torrent?
- How can a late answer, a slow command or a command queued behind a removal never bring back an
  old state or a removed torrent?

The ledger of 005 starts every operation in its own goroutine, so acceptance order was lost once
`Start` returned; a lock inside the mutation would still let a later command win
([api-contract](api-contract.md#operations)).

## Decision

### Permissions: `transfer.Decide`

The first row that matches wins. The table is checked by `internal/transfer/policy_test.go` over
every combination of inputs, and the block below must equal the tested one.

<!-- transfer-permissions:begin -->
| # | Shutting down | Phase | Payload fault | Manual pause | Global upload stop | Download | Upload |
|---|---|---|---|---|---|---|---|
| 1 | yes | any | any | any | any | no | no |
| 2 | no | not ready: waiting for metadata, checking, relinking, removing, none (zero), unknown | any | any | any | no | no |
| 3 | no | ready | yes | any | any | no | no |
| 4 | no | ready | no | yes | any | no | no |
| 5 | no | ready | no | no | yes | yes | no |
| 6 | no | ready | no | no | no | yes | yes |
<!-- transfer-permissions:end -->

- **Rows 4–6 are TR-01 and TR-08.** A manual pause stops both directions; the global stop stops
  upload only. Resuming never cancels the global stop, and turning the global stop off never starts
  a paused torrent.
- **Wanted files and completion are not inputs.** Which files are wanted is their priority in the
  engine (TR-06: "not a permission or a ban on seeding"). Download stays allowed when everything
  wanted is complete: a newly selected file then downloads with no switch call, and no pause/allow
  toggling sets the stall of findings 12 and 25.
- **Not inputs either:** network state and rate limits. After a network loss the engine reconnects
  by itself (TR-05, scenario 13); a limit is a rate, not a permission (TR-04).
- **Phases (row 2).** While the application checks the data (029, 030), relinks it (030) or removes
  the record (031, 032), nothing transfers. A check stops seeding only while it runs: a verification
  is not a policy stop (TR-03). A library record is created only after verified metadata (011, 022,
  023), so no record waits for metadata today; the row is the rule for one that ever does. An
  unknown phase never transfers, and neither does the zero value: `PhaseReady` is not zero, so a
  caller that forgets to set the phase gets a torrent that never transfers (and `Registry.Add`
  refuses it) instead of one that does.
- **Payload fault (row 3).** The torrent's files cannot be used safely: the folder is missing,
  replaced, unreadable or full (the 009 guard, 029, 030), or deleting them failed (032). An apply
  error, an unconfirmed upload stop (`transferswitch.ErrNotConfirmed`), a network or a tracker
  problem is **not** a fault. It is an outcome reported in the snapshot's `errors` and never an
  input, so a global stop can never stop download.
- **Shutdown (row 1)** stops payload without changing the stored intent, so the next start restores
  it.

### Display status: `transfer.DisplayStatus`

The first row that matches wins, checked over every combination by
`internal/transfer/status_test.go`. Shutdown does not change the status.

<!-- transfer-status:begin -->
| # | Phase | Payload fault | Manual pause | Wanted files | Complete | Global upload stop | Status |
|---|---|---|---|---|---|---|---|
| 1 | relinking | any | any | any | any | any | `relinking` |
| 2 | any | yes | any | any | any | any | `error` |
| 3 | removing | any | any | any | any | any | `deleting` |
| 4 | checking | any | any | any | any | any | `checking` |
| 5 | waiting for metadata | any | any | any | any | any | `waitingForMetadata` |
| 6 | none (zero), unknown | any | any | any | any | any | `error` |
| 7 | ready | any | yes | any | any | any | `paused` |
| 8 | ready | any | any | none | any | any | `noFilesSelected` |
| 9 | ready | any | any | any | no | any | `downloading` |
| 10 | ready | any | any | any | any | no | `seeding` |
| 11 | ready | any | any | any | any | any | `completed` |
<!-- transfer-status:end -->

Why this order:

| Rule | Reason |
|---|---|
| Relinking over the fault | The user is already fixing the fault; a relink that is cancelled returns to `error` (030 keeps the previous error). |
| A fault over everything else | The user must act, also when the torrent is paused: resuming it would only show the error later. A fault while removing means only "deleting the files failed": `BeginRemoval` clears an older fault. |
| Removing, checking, waiting for metadata over the pause | They are what the application is doing now. `manualPaused` stays in the snapshot, so the card still offers "Start" for a paused torrent (UI-13). |
| Pause over no wanted file | The pause is the user's torrent-wide choice; the card's action must be "Start". |
| No wanted file over completed and seeding | Nothing selected is the reason nothing downloads (research §7: "No files selected"); the upload rate still shows what it sends. |
| Seeding from the policy, not from `Seeding()` or the rate | The engine's flag ignores completeness, and the rate flickers (architecture §5). A complete torrent under the global stop is `completed`: the stopped sign with the "Completed" text (UI-19), and the engine refuses its new connections (finding 28). |

The vocabulary is the closed enumeration of [api-contract](api-contract.md#dtos); every status the
function returns is in it (tested).

### The order of commands: `commandorder`

- **Keys.** `commandorder.Torrent(id)` (level 0) names a change to one torrent;
  `commandorder.Settings` (level 1) a change to the stored settings, the global upload policy
  included. A turn holds keys of one level: a mixed set is refused (`ErrMixedLevels`), because it
  could wait for a settings step whose nested torrent step waits for it.
- **Acceptance order.** A turn joins the queue of every key at once, at acceptance
  (`Order.Join`); it runs when it is first in all of them. Joins form one total order, so the oldest
  unfinished turn is first in all its queues: multi-key turns cannot deadlock each other. A
  cancelled waiting turn leaves its queues; the next one proceeds.
- **Nesting.** Inside a step, `Order.Run` may ask only for keys of a strictly lower level than every
  key the step holds (`ErrHeld` otherwise): a settings step may run torrent steps, a torrent step
  may run none. Every wait is finite: level-0 turns never nest, so the oldest of them always runs to
  its end; a level-1 turn waits only for earlier level-1 turns and its own nested level-0 turns.
- **Holding.** A granted turn's context carries the turn; `order.Holds(ctx, key)` is true only for a
  turn of that same order and only until the turn leaves, and that context is cancelled then, so a
  goroutine that kept a step's context neither holds the key nor keeps running on it. A turn that
  has left is never granted again (`Wait` returns `ErrLeft`).
- **The ledger** takes keys in `Start(id, kind, fingerprint, keys, mutation)`: an operation joins
  its queues when it is accepted, stays `accepted` while it waits, and its mutation's context holds
  the keys. A queued operation that is cancelled, or stopped by `Shutdown`, ends `cancelled` without
  running and releases the next. `main.go` creates the one `Order` (the variable `order`) and passes
  it to the ledger; 025 passes the same one to the registry and the service, so the service's own
  steps share each torrent's queue. `NewRegistry(order, …)` checks keys against that order only: a
  key held in a second `Order` is `ErrNotHeld`, so a service wired with its own order fails at once
  instead of losing the ordering silently.

Keys of each command (the facade's single acceptance path, 024, takes them from the request, like
the operation ID; never a free parameter):

| Command | Keys |
|---|---|
| pause, resume (`TorrentSelectionRequest`) | `Torrents(torrentIDs)` |
| setFilesWanted | `Torrent(torrentID)` |
| removeTorrents | `Torrents(torrentIDs)` |
| updateSettings | `Settings` |
| confirmImport | none; the new torrent's first step is `order.Run(ctx, {Torrent(newID)}, …)` after its durable commit |

**Wails calls arrive in any order.** The Wails v2.16.0 macOS frontend reads the window's messages
in order but dispatches each call in its own goroutine (`internal/frontend/desktop/darwin/
frontend.go`, `processMessage`: `go func() { f.dispatcher.ProcessMessage(…) }()`). Two quick calls
can therefore reach `Ledger.Start` in either order. The order this decision guarantees is Go's
acceptance order; the frontend sends the next state-changing command for a torrent only after the
previous call returned (it returns at acceptance, so the wait is short) — a rule for 035 and 045.

### The registry and the apply step: `transfer.Registry`

`NewRegistry(order, globalUploadDisabled)` takes the runtime's order and the stored value: a
restore never starts with upload allowed. The registry is the in-memory owner of every torrent's
policy state in this runtime. It mirrors what the service already made durable: a mutator is called
only after its SQLite write (or the 031 journal) succeeded, so a failed write never needs a
rollback. Phase and fault are runtime-only. Every change bumps the torrent's `PolicyRevision`.

**A change of the intent is checked before it is stored.** In its step, the service calls
`CheckIntentChange(ctx, id)` (the refusal `SetManualPaused` would return, without changing
anything), then writes SQLite, then `SetManualPaused`, then `Reconcile`. Only a step holding the
torrent's key changes it, so the answer stays true until the step ends. Without the check, a resume
queued behind a removal whose deletion failed would be stored as "resumed" while the runtime keeps
the torrent removing, and the next start could bring it back running.

- **Mutators need the key.** `Add`, `CheckIntentChange`, `SetManualPaused`, `SetPhase`,
  `SetPayloadFault`, `BeginRemoval`, `Forget` and `Reconcile` need `Torrent(id)` held in their
  context;
  `SetGlobalUploadDisabled` needs `Settings`. Without it they return `ErrNotHeld`: STACK-03 is
  checked, not a convention. `BeginShutdown` is the one exception: one-way, it never enables
  anything.
- **Removal.** `BeginRemoval` freezes the intent and the phase (`ErrRemoving` afterwards) and clears
  the fault; `SetPayloadFault` still works (a failed deletion). `Forget`, only while removing,
  leaves a tombstone: every later call, `Add` included, gets `ErrRemoved`, so a removed entry never
  reappears in this runtime. IDs are random per import, so a tombstone never blocks a re-import.
  `Add` accepts `PhaseRemoving`, for a record whose removal was unfinished at a crash (028, 029).
- **Global revision.** `SetGlobalUploadDisabled` returns (sorted) the torrents to reconcile: those
  whose permissions change, whose revisions it bumps, and those the engine is not in step with — a
  failed or an unfinished apply — without a new revision. Saving the same value again therefore
  retries an unconfirmed upload stop; nil when nothing is to reconcile.
- **Outcomes.** `Record(id, revision, problem)` stores the engine's answer only if that revision is
  still the torrent's current one and the torrent is not removed (compare-and-set under the
  registry's lock). Anything else is a late acknowledgement and is discarded. `View.Pending()` is
  true while the current revision has no recorded outcome. `Lookup` and `All` (one consistent
  moment, for the snapshot of 027) are readers and need no key.
- **The apply step** `Reconcile(ctx, id, switches)`, run holding `Torrent(id)`:
  1. `Lookup` now, never values captured at acceptance;
  2. `switches.Apply(ctx, id, Decide(view.Input))`;
  3. `Record(id, view.Revision, problem)`. If the revision moved meanwhile (a global change, or the
     quit), the outcome is discarded and the step goes back to 1, until one is recorded; it returns
     the problem, or the context's error once the context has ended (the torrent stays pending).

  Only steps without the torrent's key move its revision (a settings step, the quit), each once per
  user or quit action, so the repeats end. A nil result means the engine was in step when its
  outcome was recorded.

  `Switches` is the port at the point of use; 018 provides the engine side (see its consequence).
  The engine side owns the engine's actual switch state and calls a switch only when it differs
  from what is asked: it compares with the engine, never with a recorded outcome (a discarded
  outcome may still have moved a switch). `Apply` runs while the step holds the key, so it is
  bounded: it returns when its context ends and puts its own deadline on every engine call.

**Why it holds.** Intent changes are ordered: acceptance order through the keys. Applying is
level-triggered and idempotent: the order of reconcile steps does not matter, only that each runs
under the torrent's key and reads the latest state. The last reconcile of a torrent therefore
applies the latest accepted intent with the latest global value.

**A new global revision** reaches every entry like this. The settings operation holds `Settings`.
It persists the value (053) and calls `SetGlobalUploadDisabled`. Then it runs
`order.Run(ctx, {Torrent(id)}, Reconcile)` for each torrent returned, in parallel, with a bound
053 sets, so a settings save waits at most for the slowest torrent.

A torrent added meanwhile is either in the returned list (its fan-out step runs after its add step)
or reads the new value in its own first reconcile. A reconcile that read the old value just before
the change finds its outcome refused and applies again with the new value, so upload stays allowed
at most for one `Apply` call. A torrent whose stop failed stays out of step, is reported by 053, and
is retried by the next save of the settings.

**Removal order.** Journal (031) → `BeginRemoval` → `Reconcile` (both off) → engine drop and record
removal (031, 032) → `Forget`. A resume queued behind it gets `ErrRemoving` or `ErrRemoved`; a late
outcome fails `Record`; a `ResumeDownload` nudge (025) is a torrent step that nudges only while
`Decide(view.Input).Download`.

### Rules for the tasks that build on this

Each of these prevents a deadlock or a lost update:

- A step holding a key may wait for a goroutine only after cancelling it. That goroutine's own
  `order.Run` must use its own lifetime context: a `ResumeDownload` watcher's nudge captures the
  watcher's context, and a pause cancels the watcher first, then waits for it.
- Never call `order.Run` from an engine callback or while holding an engine or client lock. The
  step that holds the key may need that lock (`DisallowDataUpload` takes the client lock), so
  forward the event to a goroutine.
- A long-lived goroutine never keeps a step's context: it is cancelled, and loses `Holds`, when the
  step ends.
- Pause, resume, setFilesWanted and updateSettings are not cancellable once running (no facade
  method exposes `Cancel` for them). Only quit cuts their apply short, and the restore applies the
  stored intent before any payload (029).
- Inside a step, pass the step's context to `Run`. A `context.Background()` there bypasses the
  nesting check and can deadlock on the step's own key: Go has no goroutine identity to detect it.
- Steps are short: a registry change, the apply, a database write, an engine call with its own
  deadline. Long work — hashing the data (029, 030), the metadata fetch (011, 023), any copy — runs
  outside every step under its own lifetime context; a step marks its start and its end (the phase,
  the fault). Otherwise a pause waits for a whole recheck, and a settings save whose fan-out queues
  behind that torrent waits too. A step that must stop such work (a removal during a check) cancels
  it and then waits for it, as in the first rule. The removal (031, 032) holds its keys through the
  move to the Trash, which is a rename on the same volume (012).
- A command for several torrents handles every one: a refusal of one (unknown, removed, removing)
  is reported for that torrent and never skips the others. Stopping at the first refusal would let
  any stale ID first in the list leave the rest running.
- `Apply` is bounded (above), and so is each nested `Run` of a fan-out: one hung engine call must
  not keep a key, and with it every later command for that torrent, for ever.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| A per-torrent lock inside the mutation | Simple | The goroutines race for the lock, so a later command can win | Loses acceptance order (STACK-03) |
| The settings operation holds every torrent's key | One turn for the whole change | A torrent added after acceptance is missed; every torrent waits for the slowest; the key set is fixed before the value is stored | Level-triggered reconcile reaches added torrents and runs them in parallel |
| Apply the values captured when the command was accepted | No re-read | A slow step applies an old global value after a newer one | Each step reads the latest state |
| Status from the engine's `Seeding()` or the upload rate | No policy code | `Seeding()` ignores completeness; the rate flickers | Architecture §5 |
| One fault flag for every error | One field | An unconfirmed upload stop would also stop download (TR-08) | Payload faults only; other problems are outcomes |
| One apply per `Reconcile`; a refused outcome waits for the fan-out | Simpler | The step reported success for a state it no longer had; a quit during the apply left upload on | Apply again until the outcome is recorded |
| `PhaseReady` as the zero phase | Short literals | A forgotten field gives a torrent that transfers | The zero phase is no phase: it never transfers and `Add` refuses it |
| Lock-ordered acquisition of sorted keys | Familiar | Needs a lock per key and still loses acceptance order between commands that start together | One atomic join gives the total order |

## Verified grounds

Report: [013](../verification/013-transfer-policy-state-machine.md). Apple M4, macOS 27.0.1,
Go 1.27.2.

- `internal/transfer`: both tables over every combination of inputs (permissions: 2 × 7 phases —
  the five declared, the zero and an unknown — × 2 × 2 × 2; status: × 2 wanted × 2 complete), every
  row reachable, the requirement invariants (TR-01, TR-08, TR-03, both acceptance criteria of 013),
  the ADR blocks equal to the tested tables, only listed standard packages reached (no Wails, engine,
  SQLite, file, socket or clock package) — `pass`, 100% of statements.
- `internal/commandorder`, the ledger, the registry and the composition test (the real ledger,
  order and registry; the engine as a recording double) under `-race`, five repeated runs — `pass`,
  100%. Thirteen one-run mutations of the final code (the ledger ignoring keys, a captured view, no
  revision check, one apply only, no retry, a left turn granted, `Holds` across orders, an `os`
  import, the zero phase accepted, the reference pause without the check or stopping at a refusal)
  are each caught.
- Wails v2.16.0 dispatches each call in its own goroutine (`darwin/frontend.go:427`) — read in the
  pinned module.
- Not proven here: a real engine applying the permissions (018, 025, 053), persistence (015).

## Consequences

- **018** provides the engine side of the switches with its own types (for example
  `SetSwitches(ctx, id, download, upload bool)`), never importing `internal/transfer`: the
  dependency runs from the services to the adapter, so 025 wires a small `transfer.Switches` over
  it. It owns the actual switch state, calls a switch only when it changes, stops upload with
  `transferswitch.StopUpload` and puts a deadline on every engine call. It owns the
  `ResumeDownload` watcher too, because the watcher needs the engine torrent: a download change to
  on starts it under the adapter's own lifetime context, a change to off (or the close) cancels it
  and waits for it before switching. Its nudge runs through a gate function the service injects
  (025): `order.Run(watcherCtx, {Torrent(id)}, …)` that nudges only while
  `Decide(view.Input).Download`. It maps engine errors to `AppError` codes for `Record`, sets
  `PayloadFault` from the storage guard through a torrent step, and stops data requests itself at
  quit.
- **023** calls `Add` after the durable commit (the stored pause, an explicit phase), then
  `Reconcile`, in the new torrent's step.
- **024** builds `accept` with the keys from the request; `wanted` feeds `Progress.WantedFiles`,
  never the permissions.
- **025** pause/resume, under the operation's keys, for every torrent of the request:
  `CheckIntentChange`, persist, `SetManualPaused`, `Reconcile`; a refusal is that torrent's outcome
  and never skips the rest. Maps `ErrRemoving`/`ErrRemoved`/`ErrUnknown` to `AppError` codes; wires
  `transfer.Switches` over 018's switch method and injects the watcher's gate function; passes the
  runtime's one order to `NewRegistry` and the service, and its wiring test asserts it is the
  ledger's. Caps the number of torrent IDs a request may carry, and the operations in flight, so
  memory does not grow with what the frontend sends.
- **027** builds the snapshot from `All()`: `status = DisplayStatus(view.Input, progress)`,
  `uploadAllowed = Decide(view.Input).Upload`, `Pending`/`Problem` into `errors`; a removed ID is
  absent. The registry sends no change notification: 027 reads `All()` when it builds a snapshot, or
  adds a hook to the registry with its own tests.
- **028** quit: `BeginShutdown` first (one-way, it needs no key, so a mutation that outlives the
  ledger's bounded shutdown still applies "off") → the ledger's shutdown → the adapter stops data
  requests → engine close.
- **029** restore: `NewRegistry(order, stored global)`, `Add` with the stored `ManualPaused` and
  `PhaseChecking` or `PhaseReady` (`PhaseRemoving` for an unfinished removal; the zero phase is
  refused) before any payload, then `Reconcile`. The hashing runs outside the step (the short-steps
  rule).
- **030** `PhaseRelinking`, then `PhaseChecking`, then ready; the fault clears only after the check,
  and the check runs outside the step.
- **031/032** follow the removal order above; a failed deletion is `SetPayloadFault(true)` while
  removing.
- **035/045** send the next state-changing command for a torrent only after the previous call
  returned.
- **053** fans the global revision out as above, each nested `Run` bounded, and reports pending,
  confirmed or not confirmed per torrent from `View`; a save of the same value retries the torrents
  out of step.
- **055** owns the filter sections and their intersection (R7); it uses `DisplayStatus`, never a
  second status rule.
- A change to either table changes this ADR and the test data together; the tests fail otherwise.
