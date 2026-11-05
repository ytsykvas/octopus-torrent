# Report 013: transfer policy, display status and the order of commands for one torrent

**Task:** [013](../../product/tasks/013-transfer-policy-state-machine.md). **Requirements:** `TR-01`, `TR-03`, `STACK-03`, `AI-04`. **ADR:** [transfer-status-model](../decisions/transfer-status-model.md). **Feature:** [transfer policy](../app/transfer-policy.md). **Branch:** `task/013-transfer-policy`.

## Session architecture plan

Approved by the owner on 2026-11-03. Reviewed adversarially before approval by an architect agent;
the fixes it led to are listed in the self-assessment at the end of the plan.

### Context

Task [013](../../product/tasks/013-transfer-policy-state-machine.md) (P2), requirements `TR-01`, `TR-03`,
`STACK-03`, `AI-04`; owns register item **O11** for status priority and zero wanted (filter
intersection and section names stay with 055, discrepancy R7). Dependencies 005 and 007 are `done`.

The contract has the vocabulary (`contracts.TorrentStatus`, `manualPaused`, `uploadAllowed`), but
nothing decides which permissions and which status a torrent has, and the operation ledger starts
every operation in its own goroutine, so acceptance order is lost once `Start` returns (the
api-contract ADR assigns ordering keys on `Ledger.Start` to this task). 025, 053, 031, 029, 023 and
027 all need one owner of these rules.

For the owner nothing visible changes yet (no UI, no engine call). What changes: one tested table
decides "may this torrent download / upload, and what does its card say"; every command for one
torrent changes its intent strictly in the order it was accepted; applying is re-derived from the
latest state, so a slow or late answer can never bring back an old state or a removed torrent.

**Out of scope:** the engine adapter that implements the switch port (018), pause/resume commands
(025), the global stop with confirmations (053), persistence of the intent (015/023/025), `AppError`
codes for the new refusals (the first facade command that can return them, 025/031), filter sections
(055), UI. No dependency added, no `.npmrc` change.

### Boundaries

- **New, shared:** `internal/commandorder` — per-key FIFO order of work. Imports only the standard
  library and `contracts`. Below both the ledger and the services, so no service imports the facade
  layer.
- **New, service layer:** `internal/transfer` — pure policy, display status, the policy registry and
  the apply step. Imports only the standard library, `contracts`, `commandorder`. Not linked by the
  product until 025 (like `transferswitch`).
- **Changed, facade layer:** `internal/app/operations.Ledger` takes ordering keys; `main.go` creates
  one `commandorder.Order` and passes it to the ledger (025 passes the same one to the service).
- **No change:** DTOs, bindings, TypeScript, SQLite, engine.

### Design

#### 1. `internal/commandorder`

- `type Key struct{ level uint8; name string }` (comparable, map key). `Settings` (level 1);
  `Torrent(id contracts.TorrentID) Key`, `Torrents(ids) []Key` (level 0).
- `Order` (`New()`) holds `mu` and `queues map[Key][]*Turn`.
- `(*Order).Join(keys) (*Turn, error)`: sorts and de-duplicates; refuses a set spanning two levels
  (`ErrMixedLevels`: such a set can deadlock against a settings step fanning out); appends the turn
  to every key's queue in one critical section. Join order is the acceptance order — a total order,
  so the oldest live turn is first in all its queues: no deadlock between multi-key turns.
- Readiness: per turn `ready bool` under `mu`, the channel is closed only on false→true (never
  twice). `Join` checks only the new turn; `Leave` re-checks only queues where the leaving turn was
  at index 0, and deletes empty queues.
- `(*Turn).Wait(ctx) error`: ready, or ctx ended → leaves the queues and returns `ctx.Err()`.
- `(*Turn).Leave()`: idempotent; then cancels the turn's holding context (outside `mu`).
- `(*Turn).Holding(ctx) context.Context` (after a successful `Wait`): a child ctx carrying the turn,
  cancelled at `Leave`. `Holds(ctx, key) bool` is true only while a turn in ctx is still held (atomic
  flag, no lock): a goroutine that kept a step's ctx loses the permission when the step ends.
- `(*Order).Run(ctx, keys, step)`: nested only downward — every requested key must be of a strictly
  lower level than every key still held in ctx (a settings step may run torrent steps; a torrent step
  may run none), else `ErrHeld` instead of a deadlock. Join, Wait; if ctx ended even though the turn
  became ready, leave without running; else `step(turn.Holding(ctx))` with `defer turn.Leave()` (a
  recovered panic, 028, never keeps a key).

Deadlock freedom (written in the ADR): every turn holds one level; level-0 turns never nest, so the
oldest of them always runs to its end; a level-1 turn waits only for earlier level-1 turns and for
its own nested level-0 turns.

#### 2. Ledger ordering keys

- `NewLedger(runtimeID, order *commandorder.Order)`;
  `Start(id, kind, fingerprint, keys []commandorder.Key, mutation)`.
- After the duplicate/conflict/stopped checks, under `l.mu`: `turn, err := l.order.Join(keys)` (a
  refused key set → `internal.unexpected`, nothing recorded). Lock order is always `l.mu → o.mu`;
  `Leave`/`Wait` never run under `l.mu`.
- Goroutine: `defer cancel(); defer turn.Leave()`; `turn.Wait(ctx)`; the existing `begin` (ends
  `cancelled` without running when ctx ended, also when ready and cancelled raced);
  `mutation(turn.Holding(ctx))`. A queued operation stays `accepted`; `Cancel`/`Shutdown` end it
  cancelled and release the next; a duplicate `Start` joins nothing.
- `main.go`: `operations.NewLedger(contracts.NewRuntimeID(), commandorder.New())` (still branch-free).
- Keys per command (ADR table, for 024/025/031/050; no code without a command): pause/resume/remove
  → `Torrents(ids)`; setFilesWanted → `Torrent(id)`; updateSettings → `Settings`; confirmImport →
  none (its new torrent's first step is `order.Run(ctx, {Torrent(newID)}, …)`). 024's `accept` takes
  the keys from the request (like the operation ID), never a free parameter.

#### 3. `internal/transfer`

**`policy.go`** — `Phase` (`PhaseReady`, `PhaseWaitingForMetadata`, `PhaseChecking`,
`PhaseRelinking`, `PhaseRemoving`); `Input{ManualPaused, GlobalUploadDisabled bool; Phase;
PayloadFault bool; ShuttingDown bool}`; `Permissions{Download, Upload bool}`; `Decide(Input)`.

`PayloadFault` means the torrent's files cannot be used safely: folder missing, replaced, unreadable
or full (009 guard, 029/030), or deleting them failed (032). An apply error, an unconfirmed upload
stop (`ErrNotConfirmed`), a network or tracker problem is **not** a fault: it is an outcome shown in
`errors`, never an input of `Decide` (so a global stop can never stop download, TR-08).

| # | Shutting down | Phase | Payload fault | Manual pause | Global upload stop | Download | Upload |
|---|---|---|---|---|---|---|---|
| 1 | yes | any | any | any | any | no | no |
| 2 | no | not ready: waiting for metadata, checking, relinking, removing, unknown | any | any | any | no | no |
| 3 | no | ready | yes | any | any | no | no |
| 4 | no | ready | no | yes | any | no | no |
| 5 | no | ready | no | no | yes | yes | no |
| 6 | no | ready | no | no | no | yes | yes |

Wanted and completion are not inputs: wanted is file priority (TR-06), and the download switch stays
on when complete (a newly selected file downloads with no switch call; no pause/allow toggling that
sets the finding-12 trap). Network state and rate limits are not inputs (TR-05, TR-04). Checking
stops seeding only while it runs: a verification is not a policy stop (TR-03). Shutdown stops payload
without touching the stored intent. A library record exists only after verified metadata (011/022/
023), so `PhaseWaitingForMetadata` is unreachable for records today; the row keeps the rule.

**`status.go`** — `Progress{WantedFiles int; Complete bool}` (observed by 024/027);
`DisplayStatus(Input, Progress) contracts.TorrentStatus`, first match wins:

| # | Condition | Status |
|---|---|---|
| 1 | phase relinking | `relinking` (the user is fixing the fault) |
| 2 | payload fault | `error` (a missing folder, or a failed deletion) |
| 3 | phase removing | `deleting` |
| 4 | phase checking | `checking` (file checks are not transfer, TR-01) |
| 5 | phase waiting for metadata | `waitingForMetadata` |
| 6 | unknown phase | `error` |
| 7 | manual pause | `paused` |
| 8 | no wanted file | `noFilesSelected` |
| 9 | not complete | `downloading` |
| 10 | global upload stop off | `seeding` |
| 11 | otherwise | `completed` (UI-19: stopped sign, "Completed" kept) |

Shutdown does not change the status. "Seeding" comes from the policy, never from the engine's
`Seeding()` or the observed rate. `manualPaused` stays in the snapshot, so the card still offers
"Start" for a paused torrent shown as `error`/`checking` (UI-13).

**`registry.go`** — `Registry` (`NewRegistry(globalUploadDisabled bool)`, the stored value is
required: restore must never start with upload allowed): the in-memory owner of every torrent's
policy state in this runtime. It mirrors what the service already made durable — every mutator is
called only after its SQLite write (or the 031 journal) succeeded, so a failed write never needs a
rollback — plus runtime-only phase and fault. Every change bumps the torrent's `PolicyRevision`.

| Method | Key held in ctx | Behaviour / refusals |
|---|---|---|
| `Add(ctx, id, Entry{ManualPaused, Phase, PayloadFault})` | `Torrent(id)` | any phase, `PhaseRemoving` for an unfinished removal (028/029); `ErrExists`, `ErrRemoved`, `ErrInvalidPhase` |
| `SetManualPaused`, `SetPhase` (not removing) | `Torrent(id)` | `ErrUnknown`, `ErrRemoved`, `ErrRemoving`, `ErrInvalidPhase` |
| `SetPayloadFault(ctx, id, bool)` | `Torrent(id)` | allowed while removing (failed deletion); `ErrUnknown`, `ErrRemoved` |
| `BeginRemoval(ctx, id)` | `Torrent(id)` | idempotent; clears the fault, so a fault while removing means only "deletion failed"; afterwards intent and phase are frozen |
| `Forget(ctx, id)` | `Torrent(id)` | only while removing (`ErrNotRemoving`) → tombstone: every later call, `Add` included, gets `ErrRemoved` |
| `SetGlobalUploadDisabled(ctx, bool) []TorrentID` | `Settings` | unchanged → nil; else bumps and returns (sorted) the entries whose `Decide` output changes |
| `BeginShutdown()` | none (one-way, never enables anything) | bumps every revision so late outcomes are discarded; no fan-out (the adapter stops data requests at quit, 018/028; restore re-applies before payload) |
| `Record(id, rev, problem *contracts.AppError) bool` | none | compare-and-set under the registry lock: stored only if `rev` is still current and the torrent not removed; else discarded (a late acknowledgement) |
| `Lookup(id) (View, error)`, `All() []View` | none (readers) | `View{ID, Input, Revision, Applied, Problem}`; `Pending()` = applied revision ≠ current; `All` is one consistent moment for a snapshot (027) |
| `Reconcile(ctx, id, sw Switches) error` | `Torrent(id)` | the apply step below |

Missing key → `ErrNotHeld`: the order rule (STACK-03) is checked, not a convention. Stored problems
are copied in and out (no shared `Params` map).

**`Switches`** (port at the point of use, implemented by the engine adapter in 018; the tests use a
recording double named as such): `Apply(ctx, id, want Permissions) *contracts.AppError`. The adapter
owns the engine's actual switch state and calls a switch only when it differs from `want` (compared
with the engine's actual state, never with a recorded outcome: a discarded outcome may still have
changed a switch).

**Apply protocol** (`Reconcile`, the "command dispatcher"):
1. Hold `Torrent(id)`; `Lookup` at execution — never values captured at acceptance.
2. `problem := sw.Apply(ctx, id, Decide(view.Input))`.
3. `Record(id, view.Revision, problem)`; return the problem (or a registry refusal).

Intent changes are ordered (acceptance order through the keys); applying is level-triggered and
idempotent, so the order of reconcile steps does not matter — only that each runs under the
torrent's key and reads the latest state. A new global revision: the settings operation (holds
`Settings`) persists (053), calls `SetGlobalUploadDisabled`, then runs `order.Run(ctx,
{Torrent(id)}, Reconcile)` for each returned ID, in parallel (053 sets the bound; a settings save
waits at most for the slowest torrent). A torrent added concurrently is either in the returned list
or reads the new global in its own first reconcile. Between a reconcile that read the old global and
the fan-out step, upload may stay allowed for that step's duration: the outcome of the new revision
is `Pending` until the fan-out records it (053 reports it).

Removal: journal (031) → `BeginRemoval` → `Reconcile` (off) → engine drop, record removal (031/032)
→ `Forget`. A queued resume gets `ErrRemoving`/`ErrRemoved`; a late outcome fails `Record`; a
`ResumeDownload` nudge (025) is a torrent step that nudges only while `Decide(view.Input).Download`.

Rules written in the ADR for 018/025/053 (each is a deadlock otherwise):
- a step holding a key may wait for a goroutine only after cancelling it, and that goroutine's own
  `order.Run` must use its own lifetime context (a `ResumeDownload` watcher's nudge captures the
  watcher's ctx; a pause cancels the watcher, then waits);
- never call `order.Run` from an engine callback or while holding an engine/client lock: forward the
  event to a goroutine;
- long-lived goroutines never keep a step's ctx (it is cancelled and loses `Holds` at `Leave`);
- pause, resume, setFilesWanted and updateSettings are not cancellable once running (no facade
  exposes `Cancel` for them; added to the api-contract list); only quit cuts their apply short, and
  restore applies the stored intent before any payload.

### Files

| File | Change |
|---|---|
| `internal/commandorder/order.go`, `order_test.go` | new |
| `internal/app/operations/ledger.go` | edit: `NewLedger`, `Start`, the goroutine body |
| `internal/app/operations/ledger_test.go` | edit: existing calls get `nil` keys; ordering tests |
| `main.go`, `main_test.go`, `internal/app/app_test.go` | edit: `NewLedger(…, commandorder.New())` |
| `internal/transfer/policy.go`, `status.go`, `registry.go` | new |
| `internal/transfer/policy_test.go`, `status_test.go`, `registry_test.go`, `imports_test.go` | new |
| `internal/transfer/order_test.go` (package `transfer_test`: real Ledger + Order + Registry + the recording `Switches` double) | new |
| `docs/decisions/transfer-status-model.md` | new ADR (O11; both tables between markers) |
| `docs/app/transfer-policy.md` | new feature doc (template) |
| `docs/verification/013-transfer-policy-state-machine.md` | new report (this plan as the session plan) |
| `docs/app/operations.md`, `docs/decisions/api-contract.md` (Order bullet, non-cancellable list), `docs/bridge/adding-a-command.md` (keys), `docs/app/transfer-switches.md` (link) | edit |
| `product/APPLICATION_ARCHITECTURE.md` §3/§5, `docs/README.md`, `docs/decisions/project-baseline.md` (O11 accepted for 013's part, R7 open for 055, M3 013's part), `README.md` (current state, next tasks), `CLAUDE.md` (code map, big picture) | edit |
| `product/tasks/013-*.md` (status, artifacts, execution result); "From 013" notes in 015, 018, 023, 024, 025, 027, 028, 029, 030, 031, 032, 035, 045, 053, 055 | edit; `go tool devtool plan-nav` |

### Tests first (RED → GREEN)

| Step | Test written first | Expected RED |
|---|---|---|
| 0 | Verify in the Wails v2.16.0 source (`go mod download` of the pinned module; read `internal/frontend/…/dispatcher` and the darwin frontend) whether each bound call runs in its own goroutine | — (a fact for the ADR: if yes, Go acceptance order can differ from click order, and 035/045 send the next state-changing command for a torrent only after the previous call returned) |
| 1 | `commandorder/order_test.go`: acceptance order on one key, logged (B starts only after A returned, with A blocked until B's `Join` returned); disjoint keys run together; crossing multi-key sets (`{A,B}`/`{B,A}` ×200 under `-race`, a deadline only as hang guard); a cancelled turn behind a ready head (no double close) and a cancelled head in one queue but not another let the next run; idempotent `Leave`; no keys → ready; `ErrMixedLevels`; `Run`: `ErrHeld` for torrent→any and settings→settings, settings→torrent allowed, a cancelled ctx never runs the step, the holding ctx is cancelled and `Holds` false after the step | `undefined: New` |
| 2 | `ledger_test.go`: two operations on one key run in acceptance order even when the first goroutine starts last (`l.spawn` delays it); cancel and shutdown of a queued operation end it cancelled without running and release the next; disjoint keys run together; the mutation's ctx holds its keys and loses them after; a mixed-level key set is refused and not recorded | `too many arguments in call to NewLedger` / `l.Start` |
| 3 | `policy_test.go`, `status_test.go`: both tables as ordered wildcard data; every combination (2 shutdown × 6 phases incl. unknown × fault × pause × global; ×2 wanted ×2 complete for status) equals its first matching row; every row reachable; requirement invariants over all combinations (TR-01: pause ⇒ off/off; TR-08: global ⇒ upload off, and flipping global never changes download; criterion 1: resume never cancels the ban; criterion 2: global on never enables a paused torrent; TR-03: clean ready unpaused with global on ⇒ upload); the markdown rendering of both tables equals the marked block of the ADR | `undefined: Decide`; then `open …/transfer-status-model.md: no such file` |
| 4 | `imports_test.go`: `go/build` walks the non-test imports of `internal/transfer` and `internal/commandorder`, recursing into module packages; standard library only from an allow-list (`context`, `errors`, `slices`, `sync`, `sync/atomic`, `maps`, `cmp` as used), module packages only `contracts`/`commandorder`/`transfer`; no Wails, engine, SQLite, `os`, `net` | written before `policy.go`: no Go files found; mutation check (an `os` import for one run) shows it refuses |
| 5 | `registry_test.go`: every refusal; `ErrNotHeld` without the key and after the step ended; revision bumps; global returns only entries whose permissions change, nil when unchanged; shutdown bumps all, keeps intent, makes `Decide` off; removal freezes intent/phase, clears the fault, fault still settable; `Forget` tombstones, `Add` after it refused; `Record` CAS: stale revision, removed, current; `Pending`; `All` sorted and consistent; problems copied | `undefined: NewRegistry` |
| 6 | `order_test.go` (composition, `Switches` double named as such): seeded permutations (seed logged) of pause/resume/global toggles over three torrents through the real ledger; final applied state per torrent == `Decide(latest accepted intent, latest accepted global)` and no torrent `Pending`; the double fails the test if `Apply` ever gets download or upload for a torrent paused at that moment (intent changes only under the same key, so deterministic); a torrent added before and after a global change (two deterministic interleavings via channels) ends with the new global; removal: resume (its revision captured in the step, its "confirmation" held on a channel) → remove → resume — the released late outcome fails `Record`, the second resume is refused, nothing allowed after the removal step, `Add` after `Forget` refused, `Lookup` → `ErrRemoved` | `undefined: transfer.Switches` before step 5's code; its proof is the mutation check below |

**Mutation check (recorded in the report, each a one-run local edit, reverted):** `Start` ignoring
keys → the ordering scenarios fail; `Reconcile` using a captured view instead of `Lookup` → the
global scenario fails; `Record` without the revision check → the late-outcome scenario fails.

No `time.Sleep` decides a result: order is observed through channels and an event log; deadlines
only guard against hangs.

### Coverage

Go raw counters through `go tool devtool check`: `commandorder` and `transfer` 100% from unit tests;
`ledger.go` 100% from its tests; `main.go`'s changed statement by the instrumented `.app` run the
gate already does. No TypeScript, no native code.

### Real boundaries

Real: the ledger, the order and the registry under `-race`. Test double: `Switches` (records what it
is given). Not run here, by scope: the engine applying permissions (018/025/053 on `transferswitch`),
persistence (015). No `.app` QA: nothing in the window changes; start/quit is covered by the gate's
`.app` run.

### Documents

ADR `transfer-status-model.md` (`accepted`): both tables and the reason for each priority, the fault
definition, the keys per command, the levels and the deadlock argument, the apply protocol, the
global revision fan-out, removal and tombstones, shutdown, the rules for later tasks, the Wails
dispatch fact; alternatives (a lock inside the mutation; a settings operation holding all torrent
keys; applying values captured at acceptance; status from `Seeding()`; one fault flag for every
error); consequences per later task. Feature doc `docs/app/transfer-policy.md`. Report with RED/GREEN
commands and results, the mutation check, coverage, debt check. Task 013 → `in_progress` now, `done`
with the execution result after the gate.

### Verification

1. `go test -race -count=1 ./internal/commandorder ./internal/transfer ./internal/app/... .`
2. `go tool devtool check-docs`, `go tool devtool plan-nav`, `go tool devtool check-plan`
3. `go tool devtool check` → `GATE PASSED` (the only acceptance; network and the GUI session)
4. `/branch-review` (code-reviewer, security-reviewer, tech-lead); fix findings, rerun the gate
5. Commit/PR only when the owner asks (`/pushit`)

### Self-assessment: 6/10 → 8/10

An adversarial review (architect agent) of the first design found, and this plan fixes:
- `Leave` could close a ready channel twice (panic) → a `ready` flag, re-check only changed heads;
- a key set mixing levels deadlocks against a settings fan-out → `ErrMixedLevels`, single-level turns;
- `Holds` outlived the step in kept contexts → the holding ctx is cancelled and the flag cleared at
  `Leave`;
- one fault flag would let an unconfirmed upload stop switch off download (TR-08) → `PayloadFault`
  only; apply problems are outcomes;
- no observed half, and check-then-write raced with global bumps → `Record` compare-and-set,
  `Pending`, `Reconcile` as production code the composition test drives;
- a sequential fan-out held `Settings` for N × 2 s → parallel, level-triggered fan-out;
- cancel halfway left the engine out of step → those commands are not cancellable once running;
- shutdown needed a key and a fan-out → a one-way flag;
- an old fault masked `deleting` → cleared at `BeginRemoval`;
- import check saw only direct imports → recursive over module packages.

What remains weak, and why it is acceptable:
- The `Switches` port and `Reconcile` are designed before their implementations (018/025/053).
  053's asynchronous confirmations may need a second recording path; `Record` with a captured
  revision already supports it, and the port is ours to change.
- Whether Wails runs each call in its own goroutine is not verified yet (module source not in the
  cache; step 0). Either answer keeps this design; it only decides a rule for 035/045.
- The key proof travels in a context: a caller that passes `context.Background()` to `Run` inside a
  step bypasses the nesting check and can self-deadlock. Go has no goroutine identity to detect it;
  the ADR states the rule and the tests always pass the step's ctx.
- The ADR tables are checked by a test that reads the ADR (a first in this repository); formatting
  changes in the ADR fail the test until the data is changed with them — intended coupling.
- No real engine applies these permissions in 013 (scope); the real-engine proof is 025/053.

## RED → GREEN

Working directory `/Users/tsykvas/projects/torrent`, Go 1.27.2, Apple M4, macOS 27.0.1; data: none
(in-memory registry, order and ledger; the engine is a recording test double). Every GREEN command
exits 0.

| Step | Command | Result |
|---|---|---|
| 0 Wails dispatch | read `internal/frontend/desktop/darwin/frontend.go` of the pinned Wails v2.16.0 module | each call runs in `go func() { f.dispatcher.ProcessMessage(…) }()` (line 427): the frontend sends the next command for a torrent only after the previous call returned (035/045) |
| 1 order RED | `go vet ./internal/commandorder` with `order_test.go` only | `fail` as expected: `order_test.go:23:28: undefined: Order` |
| 1 order GREEN | `go test -race -count=3 -cover ./internal/commandorder` | `pass`, 100% (a first run was 98.8%: an unreachable guard in `signal`, removed — see refactoring) |
| 2 ledger RED | `go vet ./internal/app/operations` with the new tests | `fail` as expected: `ledger_test.go:56:28: too many arguments in call to NewLedger` |
| 2 ledger GREEN | `go test -race -count=1 -cover ./internal/app/operations`; `go test -race -count=1 . ./internal/app/...` | `pass`, 100% for `operations` |
| 3 policy RED | `go test ./internal/transfer` with `policy_test.go`, `status_test.go` | `fail` as expected: `undefined: Phase`; with the code: `open ../../docs/decisions/transfer-status-model.md: no such file or directory`; with empty marked blocks: both tables differ from the ADR |
| 3 policy GREEN | `go test -count=1 -cover ./internal/transfer` after the ADR tables | `pass` (93.8% until the registry tests: `Phase.valid` unused) |
| 4 imports | `go test ./internal/transfer -run TestThePolicyReaches` with an `os` import added to `policy.go` for one run | `fail` as expected: `imports os`; restored: `pass` |
| 5 registry RED | `go vet ./internal/transfer` with `registry_test.go` | `fail` as expected: `registry_test.go:38:30: undefined: Registry` |
| 5 registry GREEN | `go test -race -count=1 -cover ./internal/transfer` | `pass`, 100% |
| 6 composition | `go test -race -count=1 -v -run 'Concurrent|AddedAround|LateAnswer' ./internal/transfer` | `pass` (seeds logged); its proof is the mutation checks below |
| review RED (L4) | `go test ./internal/commandorder` with `TestATurnThatLeftIsNeverGrantedAgain` | `fail` as expected: `undefined: ErrLeft` |
| review RED (M3) | `go vet ./internal/transfer` with the `CheckIntentChange` tests | `fail` as expected: `r.CheckIntentChange undefined` |
| review RED (M1, L1) | `go test ./internal/transfer` with `CheckIntentChange` only | `fail` as expected: an overtaken apply was not applied again (`calls [{Download:true Upload:true}]`, want a second `{Download:true}`), the quit during an apply left `{true,true}`, an ended context returned nil, and the same global value returned `[]` instead of `[B C]` |
| review RED (M4) | `go test ./internal/transfer` with `PhaseReady` made non-zero first | the ADR tables differ (the zero phase is now "not ready"); mutation checks below prove the new tests |
| review GREEN | `go test -race -count=1 -cover ./internal/commandorder ./internal/transfer ./internal/app/... .` | `pass`; 100% for `commandorder`, `transfer`, `app`, `contracts`, `operations` (the root package's `main()` is measured by the gate's `.app` run) |
| repeat | `go test -race -count=5 ./internal/transfer ./internal/commandorder ./internal/app/operations` | `pass` five times |
| docs, plan | `go tool devtool check-docs`; `go tool devtool check-plan` | `pass`; `pass` (69 tasks, navigation up to date) |

## Gate

| Run | Command | Result |
|---|---|---|
| 1 (2026-11-03, after the review fixes) | `go tool devtool check` | `GATE FAILED` only on three dependency steps that fail on `main` too and are not caused by 013: `release age` (the x/net v0.60.0 exception must go), `npm audit` (vite 8.3.2, undici 8.11.2), `govulncheck` (GO-2026-6809 in Go 1.27.2). Every other step `pass`: gofmt, vet, bindings, Go tests under `-race`, the instrumented `.app` run, go coverage (every block of every own Go file), native coverage, escape hatches, product isolation (315 packages), release build, docs, plan, single code version; `not_run`: engine integration (018), test-bridge UI tests (035) |
| 2 (2026-11-03, 013 on top of the dependency branch) | `go tool devtool check` | `GATE PASSED`: every step `pass` — the three dependency steps (release age: 185 npm versions and 359 Go modules; `npm audit`; `govulncheck`), Go tests under `-race`, the instrumented `.app` run, go coverage (every block of every own Go file), native coverage, product isolation (315 packages), release build, docs (170 Markdown files), plan, single code version; `not_run`: engine integration (018), test-bridge UI tests (035) |

The owner chose (2026-11-03) to fix the dependencies in a separate branch, as with
`deps/jsdom-30.1.2`, not in this one: `deps/go-1.27.3-vite-8.3.4`,
[PR #18](https://github.com/ytsykvas/octopus-torrent/pull/18) (Go 1.27.3 and the updates out of
quarantine, plus two flaky timing tests of 007 and 010 fixed). 013 is based on that branch, not yet
on `main`, so #18 merges first. Run 2 used Go 1.27.3; the RED → GREEN runs above used Go 1.27.2.

## Review (`/branch-review`) and fixes

Three reviewers ran on the uncommitted work. None found a critical defect; every finding below was
fixed in this change (tests first where it is code) or answered here.

| Finding | Fix |
|---|---|
| code: `Holding` could leak a cancel when called twice or after `Leave` | the releases chain; after `Leave` the context is cancelled at once; `TestEveryHoldingContextEndsWhenTheTurnLeaves` |
| code/tech-lead: `Holds` accepted a turn of any `Order` | `(*Order).Holds`; `NewRegistry(order, global)`; `TestATurnOfAnotherOrderDoesNotCount`, the "another order" cases of `TestEveryMutatorNeedsItsKey` |
| code: `Entry` and `entry` in one package | the stored state is `record` |
| code: `Reconcile` discarded `Record`'s result | it now repeats until recorded (L1 below) |
| code: the cause of a refused key set is dropped | kept: `internal.unexpected` carries no params by the api-contract rule (details go only to the local log); a mixed set is a facade bug, caught by `TestAKeySetOfTwoLevelsIsRefusedAndNotRecorded` |
| code: tests poll with `time.Sleep(1 ms)` against a deadline | kept: bounded, no flake in 5 + 8 repeated runs; the result is never decided by the sleep |
| tech-lead: `imports_test.go` used a deny-list, the plan an allow-list | an allow-list per reached package: `transfer` and `commandorder` only `cmp context errors maps slices sync sync/atomic`; `contracts` its current list (lexical `path/filepath`, `crypto/rand`, JSON); a new import fails |
| tech-lead: steps must be short | ADR rule; 029/030/032 notes |
| tech-lead: two owners of the `ResumeDownload` watcher | one owner: the adapter (018) starts and cancels it; 025 injects the gate function; ADR, 018 and 025 notes |
| tech-lead: `Switches` would make the adapter import `transfer` | 018 exposes its own types; 025 wires the `transfer.Switches` wrapper (ADR, 018 note) |
| tech-lead: 027 has no change notification | 027 note |
| tech-lead: the removal test's comment said "held on a channel" | the comment says what the test does: the late answer is recorded with the old revision inside the removal step and again after it |
| security M1: a failed apply is never retried; the same global value returns nothing | `SetGlobalUploadDisabled` also returns the torrents out of step (pending or with a problem), without a new revision; `TestSavingTheGlobalValueRetriesTheTorrentsNotInStep` |
| security M2: the reference pause stops at the first refusal | every ID handled, refusals per torrent; `TestABulkPauseHandlesEveryTorrentAndRefusesOnlyTheBadOne`; ADR rule, 025 note |
| security M3: a resume of a removing torrent is stored before the refusal | `Registry.CheckIntentChange` before the write; `TestCheckIntentChangeChangesNothing`, `TestAResumeOfATorrentBeingRemovedIsRefusedBeforeItIsStored`; ADR, 025 note |
| security M4: zero values permit everything | `PhaseReady` is not zero; the zero phase never transfers and `Add`/`SetPhase` refuse it; `TestTheZeroInputTransfersNothing`, the "none (zero)" rows of both ADR tables |
| security L1: `Reconcile` reports success for a stale apply | it applies again while the revision moves, returns ctx's error once ctx ends; `TestReconcileAppliesAgainUntilTheCurrentRevisionIsRecorded` (global change, quit, ended context); 028 calls `BeginShutdown` before the ledger's shutdown |
| security L2: no deadline on a step that holds a key | `Switches` contract: `Apply` returns when ctx ends and bounds every engine call (018); bounded fan-out (053) |
| security L3: memory grows with the IDs a request carries | 025 note: cap the IDs per request and the operations in flight |
| security L4: `Wait` after `Leave` granted the turn again | `ErrLeft`; `Leave` wakes a blocked `Wait`; `TestATurnThatLeftIsNeverGrantedAgain` |
| security L5: one order per runtime is a convention | `main.go` names the `order`; `NewRegistry(order, …)` refuses keys of another order; the 025 wiring test asserts it is the ledger's |

## Mutation checks

Each is a one-run edit of the final code, reverted at once (`git status` and a green rerun after
the batch); command `go test -count=1 -timeout 40s <package>`.

| # | Edit | Caught by |
|---|---|---|
| 1 | `Start` joins no keys (`Join(nil)`) | 4 ledger tests (`TestOperationsOfOneTorrentRunInAcceptanceOrder`, `TestACancelledQueuedOperationIsSkippedAndReleasesTheNext`, `TestAMutationHoldsItsKeysOnlyWhileItRuns`, `TestAKeySetOfTwoLevelsIsRefusedAndNotRecorded`) and all 5 composition tests |
| 2 | `Reconcile` applies a view captured once (no `Lookup` per attempt) | `TestReconcileAppliesAgainUntilTheCurrentRevisionIsRecorded` (`context deadline exceeded` of its apply guard), `TestConcurrentCommandsEndWithTheLatestAcceptedIntentAndGlobalValue` |
| 3 | `Record` without the revision check | `TestRecordKeepsOnlyTheOutcomeOfTheCurrentRevision`, `TestReconcileAppliesAgainUntilTheCurrentRevisionIsRecorded`, `TestALateAnswerAfterRemovalIsDiscardedAndTheTorrentNeverComesBack` |
| 4 | `Reconcile` applies once (no repeat) | `TestReconcileAppliesAgainUntilTheCurrentRevisionIsRecorded` |
| 5 | the global save does not return torrents out of step | `TestSavingTheGlobalValueRetriesTheTorrentsNotInStep` |
| 6 | `Wait` grants a turn that left | `TestATurnThatLeftIsNeverGrantedAgain` |
| 7 | `Leave` does not wake a blocked `Wait` | `TestATurnThatLeftIsNeverGrantedAgain` |
| 8 | `Holds` accepts a turn of another order | `TestATurnOfAnotherOrderDoesNotCount`, `TestEveryMutatorNeedsItsKey` |
| 9 | the policy imports `os` | `TestThePolicyReachesNoWailsDiskOrNetwork` |
| 10 | `Phase.valid` accepts the zero phase | `TestAddRefusesAKnownTorrentAndAnInvalidPhase`, `TestAChangeBumpsTheRevisionAndARepeatDoesNot` |
| 11 | `PhaseReady` back to zero | does not build: the zero phase and `PhaseReady` collide in the tested phase names |
| 12 | the reference pause stores without `CheckIntentChange` | `TestAResumeOfATorrentBeingRemovedIsRefusedBeforeItIsStored` |
| 13 | the reference pause stops at the first refusal | `TestABulkPauseHandlesEveryTorrentAndRefusesOnlyTheBadOne` |

## Deviations from the approved plan

- `NewRegistry(order, globalUploadDisabled)` and `(*Order).Holds` instead of
  `NewRegistry(globalUploadDisabled)` and a package-level `Holds`: a key of a second order is
  refused (review).
- `Reconcile` applies again until its outcome is recorded instead of leaving the torrent pending for
  the fan-out; `SetGlobalUploadDisabled` also returns torrents out of step; `CheckIntentChange` and
  `ErrLeft` are new (security review).
- The zero `Phase` is no phase (`PhaseReady` is 1): both tables have a "none (zero)" phase.
- The removal scenario records the first resume's late answer with its revision inside the removal
  step and after it, instead of holding it on a channel: the same late answer, deterministic.
- The import check's allow-list has a list per reached package (`contracts` has its own), as the
  plan's "as used" implied.

## Boundaries confirmed / not run

- `pass` — both tables over every combination of inputs: permissions 2 × 7 phases (5 declared, the
  zero, an unknown) × 2 × 2 × 2; status × 2 wanted × 2 complete; every row reachable; the ADR
  states both.
- `pass` — resuming never cancels the global stop; re-enabling upload never starts a paused torrent
  (invariants over every input, and the composition test).
- `pass` — the policy reaches no Wails, engine, SQLite, file, socket or clock package (allow-list).
- `pass` — concurrent commands in random orders (seeded, logged) end with the latest accepted intent
  and global value, applied; a late answer after removal is discarded; the removed torrent never
  comes back; a bulk pause with a bad ID pauses the rest; a resume behind a failed deletion is never
  stored.
- `not_run` — a real engine applying the permissions (018, 025, 053) and persistence (015): out of
  scope. Interface checks use the recording `Switches` double (`engineDouble`); no `.app` QA, because
  nothing in the window changes (start and quit are covered by the gate's `.app` run).

## Coverage

`go test -race -cover`: 100% of statements for `internal/commandorder`, `internal/transfer`,
`internal/app`, `internal/app/contracts` and `internal/app/operations`. The changed `main()` is
covered only by the instrumented `.app` run of the gate. No TypeScript and no native code changed.

## Refactoring and debt

- `commandorder.signal` lost a defensive `if ready { return }` that could never run (100% exposed
  it): readiness is structural, written in its comment. The later `ready` flag is reachable from
  both sides (`signal` and a `Leave` of a queued turn).
- `Holding` was rewritten as one `if/else` so every path uses its cancel (`go vet` lostcancel).
- The registry test helpers hold each key once and `add` runs in its own step, matching how
  services use the order.
- The ledger's "no ordering" limit is removed from its document and from the api-contract ADR.
- No `TODO`, no skipped test, no ignore directive, no lowered threshold, no new dependency.
- Left to later tasks by scope, not debt: the engine side of `Switches` and the watcher (018), the
  facade commands and their `AppError` codes (024, 025, 031), persistence (015), the snapshot (027).
