# Snapshot, delta and operation ordering

> Last updated: 2026-10-15

Go will push library changes as events and answer full-state requests. Messages can arrive late,
twice, or from a previous run of Go. `frontend/src/bridge/contract/ordering.ts` decides, for every
message, whether to **apply** it, **discard** it, or **fetch** the full snapshot. The functions are
pure: the store of task 035 keeps the state, owns the subscriptions and calls them. Event emission
comes with task 027 (`library.snapshot`, `library.delta`, `operation.changed`).

## Flow

```
mount: subscribe to events ─▶ startFetch(state) → fetchNumber ─▶ GetLibrarySnapshot()
answer  ─▶ receiveSnapshot(state, {kind:'fetch', fetchNumber}, snapshot) → apply | discard | fetch
event library.snapshot ─▶ receiveSnapshot(state, {kind:'event'}, snapshot) → apply | discard | fetch
event library.delta    ─▶ receiveDelta(state, delta)                       → apply | discard | fetch
fetch failed ─▶ fetchFailed(state, n); while needsFetch(state): fetch again with back-off (035)
```

`OrderingState` holds the applied `{runtimeID, revision}` (or `null`), the retired runtimes, the
number of the latest fetch, and whether a fetch is in flight.

## Rules

| Message | Decision |
|---|---|
| Answer to an older fetch | discard: only the latest fetch counts |
| Full snapshot, same runtime, revision ≥ current | apply. It may skip revisions, and that never triggers a fetch |
| Full snapshot, same runtime, revision < current | discard |
| Full snapshot of a new runtime (even revision 0) | apply. The previous runtime is retired |
| Anything from a retired runtime | discard whatever its revision. If nothing is applied and no fetch is in flight (for example the awaited answer came from it), fetch again |
| Delta, same runtime, `baseRevision == current` | apply. The current revision becomes `delta.revision` |
| Delta, same runtime, `baseRevision < current` | discard: already applied |
| Delta, same runtime, `baseRevision > current` (a gap) | fetch once. Nothing more while that fetch is in flight |
| Delta before any snapshot | discard while the first fetch is in flight, otherwise fetch |
| Delta of a new runtime | retire the current one, then fetch (once) |

Only one Go process emits to a window. A message from an unseen runtime therefore means Go
restarted; there is no order between runtimes.

## Operations

- `isLaterOperationState(previous, next)`: states only move forward (`accepted` → `running` → a
  final one). An `operation.changed` with an earlier or equal state is discarded, and a final state
  never changes to another final one.
- `notFoundMeaning(error, acceptedBy)` reads `operation.notFound`:
  - `'notAccepted'`: the same runtime never accepted it, so resend with the same ID;
  - `'outcomeUnknown'`: Go restarted, so read the full state and never resend because of it.

## Example

```ts
let state = initialOrdering
const first = startFetch(state)                       // fetchNumber 1
state = first.state
const d = receiveSnapshot(state, { kind: 'fetch', fetchNumber: 1 }, { runtimeID: A, revision: 41n })
state = d.state                                       // apply: current = A@41
const next = receiveDelta(state, { runtimeID: A, baseRevision: 43n, revision: 44n })
// next.action === 'fetch' (gap): call getLibrarySnapshot(), then
// receiveSnapshot(next.state, { kind: 'fetch', fetchNumber: next.fetchNumber }, answer)
```

Tests: `frontend/src/bridge/contract/ordering.test.ts` covers every row above. Decisions:
[ADR](../decisions/api-contract.md#snapshots-deltas-and-ordering).
