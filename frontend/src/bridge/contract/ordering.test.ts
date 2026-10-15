import { describe, expect, it } from 'vitest'
import {
  fetchFailed,
  initialOrdering,
  isLaterOperationState,
  notFoundMeaning,
  receiveDelta,
  receiveSnapshot,
  startFetch,
  type OrderingState,
} from './ordering'
import type { AppError } from './errors'

const A = '8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e'
const B = '058fa117-e7a4-4b9d-9476-c2430080fa26'

function at(runtimeID: string, revision: bigint, fetchInFlight = false): OrderingState {
  return { ...initialOrdering, current: { runtimeID, revision }, latestFetch: 1, fetchInFlight }
}

const delta = (runtimeID: string, baseRevision: bigint, revision: bigint) => ({ runtimeID, baseRevision, revision })

describe('full snapshots', () => {
  it('subscribe first, then fetch: the answer to the fetch is applied', () => {
    const { state, fetchNumber } = startFetch(initialOrdering)
    const decision = receiveSnapshot(state, { kind: 'fetch', fetchNumber }, { runtimeID: A, revision: 7n })

    expect(decision.action).toBe('apply')
    expect(decision.state).toMatchObject({ current: { runtimeID: A, revision: 7n }, fetchInFlight: false })
  })

  it('discard the answer to an older fetch', () => {
    const first = startFetch(initialOrdering)
    const second = startFetch(first.state)

    const late = receiveSnapshot(second.state, { kind: 'fetch', fetchNumber: first.fetchNumber }, { runtimeID: A, revision: 1n })

    expect(late.action).toBe('discard')
    expect(late.state.fetchInFlight).toBe(true)
  })

  it('apply a revision jump without fetching again: snapshots may skip revisions', () => {
    const decision = receiveSnapshot(at(A, 3n), { kind: 'event' }, { runtimeID: A, revision: 90n })

    expect(decision).toMatchObject({ action: 'apply', state: { current: { revision: 90n } } })
  })

  it('discard a snapshot older than the applied state', () => {
    expect(receiveSnapshot(at(A, 5n), { kind: 'event' }, { runtimeID: A, revision: 4n }).action).toBe('discard')
  })

  it('accept revision 0 of a new runtime and retire the previous one', () => {
    const decision = receiveSnapshot(at(A, 500n), { kind: 'event' }, { runtimeID: B, revision: 0n })

    expect(decision).toMatchObject({ action: 'apply', state: { current: { runtimeID: B, revision: 0n }, retired: [A] } })
    expect(receiveSnapshot(decision.state, { kind: 'event' }, { runtimeID: A, revision: 501n }).action).toBe('discard')
  })
})

describe('deltas', () => {
  it('apply on exactly the base revision of the same runtime', () => {
    const decision = receiveDelta(at(A, 41n), delta(A, 41n, 42n))

    expect(decision).toMatchObject({ action: 'apply', state: { current: { runtimeID: A, revision: 42n } } })
  })

  it('discard a delta older than the applied state', () => {
    expect(receiveDelta(at(A, 42n), delta(A, 40n, 41n)).action).toBe('discard')
  })

  it('fetch once on a gap, not again while that fetch is in flight', () => {
    const gap = receiveDelta(at(A, 41n), delta(A, 43n, 44n))

    expect(gap).toMatchObject({ action: 'fetch', fetchNumber: 2, state: { fetchInFlight: true } })
    expect(receiveDelta(gap.state, delta(A, 44n, 45n)).action).toBe('discard')
  })

  it('discard deltas until the first snapshot while it is being fetched', () => {
    const { state } = startFetch(initialOrdering)

    expect(receiveDelta(state, delta(A, 0n, 1n)).action).toBe('discard')
  })

  it('fetch when a delta comes before any snapshot was asked for', () => {
    expect(receiveDelta(initialOrdering, delta(A, 0n, 1n)).action).toBe('fetch')
  })

  it('fetch once for a new runtime and drop the late messages of the retired one', () => {
    const decision = receiveDelta(at(A, 41n), delta(B, 0n, 1n))

    expect(decision).toMatchObject({ action: 'fetch', state: { current: null, retired: [A], fetchInFlight: true } })
    expect(receiveDelta(decision.state, delta(A, 41n, 42n)).action).toBe('discard')
    expect(receiveDelta(decision.state, delta(B, 1n, 2n)).action).toBe('discard')
  })

  it('fetch again after a failed fetch', () => {
    const gap = receiveDelta(at(A, 41n), delta(A, 43n, 44n))
    if (gap.action !== 'fetch') throw new Error('expected a fetch')
    const failed = fetchFailed(gap.state, gap.fetchNumber)

    expect(failed.fetchInFlight).toBe(false)
    expect(receiveDelta(failed, delta(A, 44n, 45n)).action).toBe('fetch')
  })

  it('ignore the failure of an older fetch', () => {
    const second = startFetch(startFetch(initialOrdering).state)

    expect(fetchFailed(second.state, 1).fetchInFlight).toBe(true)
  })
})

describe('operation states', () => {
  it.each([
    ['accepted', 'running', true],
    ['running', 'succeeded', true],
    ['accepted', 'cancelled', true],
    ['running', 'accepted', false],
    ['succeeded', 'running', false],
    ['failed', 'succeeded', false],
    ['running', 'running', false],
  ] as const)('%s → %s is later: %s', (previous, next, later) => {
    expect(isLaterOperationState(previous, next)).toBe(later)
  })
})

function notFound(runtimeID: string): AppError {
  return { code: 'operation.notFound', operationID: null, torrentID: null, params: { runtimeID } }
}

describe('operation.notFound', () => {
  it('from the runtime that accepted the operation: it was never accepted', () => {
    expect(notFoundMeaning(notFound(A), A)).toBe('notAccepted')
  })

  it('from another runtime: the outcome is unknown, read the full state and never resend', () => {
    expect(notFoundMeaning(notFound(B), A)).toBe('outcomeUnknown')
  })
})
