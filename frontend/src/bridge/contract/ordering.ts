// Ordering rules for library snapshots, deltas and operation statuses (STACK-03, ADR
// api-contract). Pure decisions: the store (task 035) owns the state and the subscriptions and
// asks these functions what to do with each message.
//
// The client subscribes to events first, then fetches the full snapshot. A full snapshot is
// authoritative for its runtime and may skip revisions. A delta applies only on exactly the current
// revision of the same runtime; a gap means one fetch, never several. A new runtime retires the
// previous one, whose late messages are then dropped whatever their revision.
import type { AppError } from './errors'
import type { RuntimeID } from './ids'
import type { OperationState } from './operations'

export interface OrderingState {
  /** The runtime and revision of the applied library state; null before the first snapshot. */
  readonly current: { readonly runtimeID: RuntimeID; readonly revision: bigint } | null
  readonly retired: readonly RuntimeID[]
  /** Number of the newest fetch; answers to older fetches are discarded. */
  readonly latestFetch: number
  readonly fetchInFlight: boolean
}

export const initialOrdering: OrderingState = { current: null, retired: [], latestFetch: 0, fetchInFlight: false }

/** Where a full snapshot came from: the answer to a numbered fetch, or the library.snapshot event. */
export type SnapshotSource = { kind: 'fetch'; fetchNumber: number } | { kind: 'event' }

export type Decision =
  | { action: 'apply'; state: OrderingState }
  | { action: 'discard'; state: OrderingState }
  /** Fetch the full snapshot with this number. */
  | { action: 'fetch'; state: OrderingState; fetchNumber: number }

/** Starts fetching the full snapshot; the answer must come back with fetchNumber. */
export function startFetch(state: OrderingState): { state: OrderingState; fetchNumber: number } {
  const fetchNumber = state.latestFetch + 1
  return { state: { ...state, latestFetch: fetchNumber, fetchInFlight: true }, fetchNumber }
}

/** A failed fetch lets the next gap fetch again; the failure of an older fetch changes nothing. */
export function fetchFailed(state: OrderingState, fetchNumber: number): OrderingState {
  return fetchNumber === state.latestFetch ? { ...state, fetchInFlight: false } : state
}

export function receiveSnapshot(
  state: OrderingState,
  source: SnapshotSource,
  snapshot: { runtimeID: RuntimeID; revision: bigint },
): Decision {
  if (source.kind === 'fetch') {
    if (source.fetchNumber !== state.latestFetch) return { action: 'discard', state }
    state = { ...state, fetchInFlight: false }
  }
  if (state.retired.includes(snapshot.runtimeID)) return { action: 'discard', state }
  const { current } = state
  if (current !== null && current.runtimeID === snapshot.runtimeID && snapshot.revision < current.revision) {
    return { action: 'discard', state }
  }
  return { action: 'apply', state: { ...retire(state, snapshot.runtimeID), current: { ...snapshot } } }
}

export function receiveDelta(
  state: OrderingState,
  delta: { runtimeID: RuntimeID; baseRevision: bigint; revision: bigint },
): Decision {
  if (state.retired.includes(delta.runtimeID)) return { action: 'discard', state }
  const { current } = state
  if (current !== null && current.runtimeID === delta.runtimeID) {
    if (delta.baseRevision === current.revision) {
      return { action: 'apply', state: { ...state, current: { runtimeID: delta.runtimeID, revision: delta.revision } } }
    }
    if (delta.baseRevision < current.revision) return { action: 'discard', state }
  } else if (current !== null) {
    state = { ...retire(state, delta.runtimeID), current: null }
  }
  if (state.fetchInFlight) return { action: 'discard', state }
  const fetch = startFetch(state)
  return { action: 'fetch', ...fetch }
}

// Retires the current runtime when runtimeID is a different one.
function retire(state: OrderingState, runtimeID: RuntimeID): OrderingState {
  const { current } = state
  if (current === null || current.runtimeID === runtimeID) return state
  return { ...state, retired: [...state.retired, current.runtimeID] }
}

const stateRank: Record<OperationState, number> = { accepted: 0, running: 1, succeeded: 2, failed: 2, cancelled: 2 }

/** Operation states only move forward; an operation.changed with an earlier or equal state is discarded. */
export function isLaterOperationState(previous: OperationState, next: OperationState): boolean {
  return stateRank[next] > stateRank[previous]
}

/**
 * What operation.notFound means for an operation accepted by acceptedBy. From the same runtime the
 * operation was never accepted (the ledger never forgets one), so resending with the same operation
 * ID is safe. From another runtime Go restarted: the outcome is unknown, so read the full state and
 * never resend because of this answer.
 */
export function notFoundMeaning(error: AppError, acceptedBy: RuntimeID): 'notAccepted' | 'outcomeUnknown' {
  return error.params.runtimeID === acceptedBy ? 'notAccepted' : 'outcomeUnknown'
}
