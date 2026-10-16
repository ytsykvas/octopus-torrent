import { StrictMode } from 'react'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { SpikeApp } from './SpikeApp'
import type { SpikeBridge, Transfer } from './bridge'
import type { AppError, Result } from '../bridge/contract/errors'

function transfer(overrides: Partial<Transfer> = {}): Transfer {
  return {
    revision: 1n,
    state: 'idle',
    bytesCompleted: 0n,
    length: 0n,
    payloadRead: 0n,
    payloadWritten: 0n,
    peers: 0,
    clientsStarted: 0,
    recoveries: 0,
    error: '',
    ...overrides,
  }
}

type Command = () => Promise<Result<Transfer>>
type Subscribe = SpikeBridge['subscribe']

const ok = (value: Transfer): Result<Transfer> => ({ ok: true, value })
const failed = (detail: string): Result<Transfer> => ({
  ok: false,
  error: { code: 'bridge.callFailed', operationID: null, torrentID: null, params: { detail } },
})

// fakeBridge records the order of the calls and hands the test the event callbacks.
function fakeBridge(snapshot: Result<Transfer> = ok(transfer())) {
  const order: string[] = []
  const listeners: { onTransfer: (t: Transfer) => void; onError: (e: AppError) => void }[] = []
  const unsubscribe = vi.fn<() => void>()
  const bridge: SpikeBridge = {
    start: vi.fn<Command>(() => Promise.resolve(ok(transfer({ revision: 2n, state: 'running', clientsStarted: 2, length: 1048576n })))),
    pause: vi.fn<Command>(() => Promise.resolve(ok(transfer({ revision: 5n, state: 'paused', clientsStarted: 2, length: 1048576n })))),
    resume: vi.fn<Command>(() => Promise.resolve(ok(transfer({ revision: 6n, state: 'running', clientsStarted: 2, length: 1048576n })))),
    snapshot: vi.fn<Command>(() => {
      order.push('snapshot')
      return Promise.resolve(snapshot)
    }),
    subscribe: vi.fn<Subscribe>((onTransfer, onError) => {
      order.push('subscribe')
      listeners.push({ onTransfer, onError })
      return unsubscribe
    }),
    reload: vi.fn<() => void>(),
  }
  const emit = (t: Transfer) => act(() => listeners.forEach((l) => l.onTransfer(t)))
  return { bridge, order, listeners, unsubscribe, emit }
}

describe('SpikeApp', () => {
  it('subscribes before it asks for the snapshot, then shows the snapshot', async () => {
    const { bridge, order } = fakeBridge()

    render(<SpikeApp bridge={bridge} />)

    expect(await screen.findByText('idle')).toBeInTheDocument()
    expect(order).toEqual(['subscribe', 'snapshot'])
    expect(screen.getByText('0 / 0 bytes (0%)')).toBeInTheDocument()
  })

  it('keeps an event that arrived before the snapshot when the snapshot is older', async () => {
    let answer: ((r: Result<Transfer>) => void) | undefined
    const { bridge, emit } = fakeBridge()
    bridge.snapshot = vi.fn<Command>(() => new Promise<Result<Transfer>>((resolve) => (answer = resolve)))
    render(<SpikeApp bridge={bridge} />)

    emit(transfer({ revision: 4n, state: 'running', bytesCompleted: 262144n, length: 1048576n }))
    await act(async () => answer?.(ok(transfer({ revision: 3n }))))

    expect(screen.getByText('running')).toBeInTheDocument()
    expect(screen.getByText('262144 / 1048576 bytes (25%)')).toBeInTheDocument()
  })

  it('shows the progress of every newer event and ignores older ones', async () => {
    const { bridge, emit } = fakeBridge()
    render(<SpikeApp bridge={bridge} />)
    await screen.findByText('idle')

    emit(transfer({ revision: 8n, state: 'running', bytesCompleted: 524288n, length: 1048576n, peers: 1, clientsStarted: 2 }))
    emit(transfer({ revision: 7n, state: 'running', bytesCompleted: 1n, length: 1048576n }))

    expect(screen.getByText('524288 / 1048576 bytes (50%)')).toBeInTheDocument()
    expect(screen.getByText('1 peer, 2 engine clients started, 0 stalls woken')).toBeInTheDocument()

    emit(transfer({ revision: 9n, state: 'running', bytesCompleted: 524288n, length: 1048576n, peers: 2, clientsStarted: 2, recoveries: 1 }))
    expect(screen.getByText('2 peers, 2 engine clients started, 1 stall woken')).toBeInTheDocument()
  })

  it('starts, pauses and resumes through the bridge; a reply older than the screen is ignored', async () => {
    const user = userEvent.setup()
    const { bridge, emit } = fakeBridge()
    render(<SpikeApp bridge={bridge} />)
    await screen.findByText('idle')

    expect(screen.getByRole('button', { name: 'Pause' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Start' }))
    expect(screen.getByText('running')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Start' })).toBeDisabled()

    emit(transfer({ revision: 9n, state: 'running', clientsStarted: 2, bytesCompleted: 4n, length: 1048576n }))
    await user.click(screen.getByRole('button', { name: 'Pause' }))
    expect(bridge.pause).toHaveBeenCalledOnce()
    expect(screen.getByText('running')).toBeInTheDocument() // the reply (revision 5) is older than revision 9

    emit(transfer({ revision: 10n, state: 'paused', clientsStarted: 2, length: 1048576n }))
    await user.click(screen.getByRole('button', { name: 'Resume' }))
    expect(bridge.resume).toHaveBeenCalledOnce()
  })

  it('shows a refused command, a failed run and an event that broke the contract', async () => {
    const user = userEvent.setup()
    const { bridge, emit, listeners } = fakeBridge()
    bridge.start = vi.fn<Command>(() => Promise.resolve(failed('the spike is stopped')))
    render(<SpikeApp bridge={bridge} />)
    await screen.findByText('idle')

    await user.click(screen.getByRole('button', { name: 'Start' }))
    expect(screen.getByRole('alert')).toHaveTextContent('bridge.callFailed: the spike is stopped')

    emit(transfer({ revision: 3n, state: 'failed', error: 'start the seeder: no more peers' }))
    expect(screen.getByText('failed')).toBeInTheDocument()
    expect(screen.getAllByRole('alert').map((a) => a.textContent)).toContain('Run failed: start the seeder: no more peers')
    expect(screen.getByRole('button', { name: 'Start' })).toBeEnabled()

    act(() => listeners[0]?.onError({ code: 'contract.invalidDto', operationID: null, torrentID: null, params: { path: 'peers', reason: 'range' } }))
    expect(screen.getAllByRole('alert').map((a) => a.textContent)).toContain('contract.invalidDto: peers range')
  })

  it('clears a refused command once a later reply succeeds', async () => {
    const user = userEvent.setup()
    const { bridge } = fakeBridge()
    bridge.start = vi
      .fn<Command>()
      .mockResolvedValueOnce(failed('no more peers'))
      .mockResolvedValueOnce(ok(transfer({ revision: 2n, state: 'running', clientsStarted: 2, length: 1048576n })))
    render(<SpikeApp bridge={bridge} />)
    await screen.findByText('idle')

    await user.click(screen.getByRole('button', { name: 'Start' }))
    expect(screen.getByRole('alert')).toHaveTextContent('bridge.callFailed: no more peers')
    await user.click(screen.getByRole('button', { name: 'Start' }))
    expect(screen.getByText('running')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('shows a snapshot that failed', async () => {
    const { bridge } = fakeBridge(failed('no window'))

    render(<SpikeApp bridge={bridge} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('bridge.callFailed: no window')
    expect(screen.getByText('not loaded')).toBeInTheDocument()
  })

  it('keeps one live subscription under StrictMode and none after unmount', async () => {
    const { bridge, unsubscribe } = fakeBridge()

    const { unmount } = render(
      <StrictMode>
        <SpikeApp bridge={bridge} />
      </StrictMode>,
    )
    await screen.findByText('idle')
    expect(vi.mocked(bridge.subscribe).mock.calls.length - unsubscribe.mock.calls.length).toBe(1)

    unmount()
    expect(unsubscribe.mock.calls.length).toBe(vi.mocked(bridge.subscribe).mock.calls.length)
  })

  it('ignores a snapshot that arrives after unmount', async () => {
    let answer: ((r: Result<Transfer>) => void) | undefined
    const { bridge } = fakeBridge()
    bridge.snapshot = vi.fn<Command>(() => new Promise<Result<Transfer>>((resolve) => (answer = resolve)))
    const errors = vi.spyOn(console, 'error')

    const { unmount } = render(<SpikeApp bridge={bridge} />)
    unmount()
    await act(async () => answer?.(ok(transfer({ revision: 3n }))))

    expect(errors).not.toHaveBeenCalled()
  })

  it('reloads the page through the bridge', async () => {
    const user = userEvent.setup()
    const { bridge } = fakeBridge()
    render(<SpikeApp bridge={bridge} />)

    await user.click(screen.getByRole('button', { name: 'Reload UI' }))

    expect(bridge.reload).toHaveBeenCalledOnce()
  })
})
