import { afterEach, describe, expect, it, vi } from 'vitest'
import { spikeBridge, transfer, transferEvent } from './bridge'
import { decodeDto } from '../bridge/contract/errors'
import { record } from '../test/contractExamples'

// The example Go checks against the same Transfer (internal/enginespike/testdata/transfer.json).
const files: Record<string, unknown> = import.meta.glob('../../../internal/enginespike/testdata/transfer.json', {
  eager: true,
  import: 'default',
})
const example = record(files['../../../internal/enginespike/testdata/transfer.json'])
const shared = { event: example.event, transfer: record(example.transfer) }

const decoded = {
  revision: 7n,
  state: 'paused',
  bytesCompleted: 524288n,
  length: 1048576n,
  payloadRead: 540672n,
  payloadWritten: 0n,
  peers: 1,
  clientsStarted: 2,
  error: '',
}

type Answer = () => Promise<unknown>

function stubGo(answer: Answer) {
  const api = { Start: vi.fn<Answer>(answer), Pause: vi.fn<Answer>(answer), Resume: vi.fn<Answer>(answer), Snapshot: vi.fn<Answer>(answer) }
  Object.assign(window, { go: { enginespike: { API: api } } })
  return api
}

function stubRuntime() {
  const off = vi.fn<() => void>()
  const runtime = {
    EventsOnMultiple: vi.fn<(name: string, callback: (...data: unknown[]) => void, max: number) => () => void>(() => off),
    WindowReloadApp: vi.fn<() => void>(),
  }
  Object.assign(window, { runtime })
  return { runtime, off }
}

afterEach(() => {
  Reflect.deleteProperty(window, 'go')
  Reflect.deleteProperty(window, 'runtime')
})

describe('the transfer decoder', () => {
  it('reads the shared example under the event name Go emits', () => {
    expect(shared.event).toBe(transferEvent)
    expect(decodeDto(transfer, shared.transfer)).toEqual({ ok: true, value: decoded })
  })

  it('rejects a number where Go writes a decimal string, and an unknown state', () => {
    expect(decodeDto(transfer, { ...shared.transfer, bytesCompleted: 524288 })).toMatchObject({
      ok: false,
      error: { params: { path: 'bytesCompleted', reason: 'type' } },
    })
    expect(decodeDto(transfer, { ...shared.transfer, state: 'seeding' })).toMatchObject({
      ok: false,
      error: { params: { path: 'state', reason: 'value' } },
    })
  })
})

describe('spikeBridge', () => {
  it.each(['start', 'pause', 'resume', 'snapshot'] as const)('%s calls Go and decodes its answer', async (name) => {
    const api = stubGo(() => Promise.resolve(shared.transfer))

    await expect(spikeBridge[name]()).resolves.toEqual({ ok: true, value: decoded })
    const called = { start: api.Start, pause: api.Pause, resume: api.Resume, snapshot: api.Snapshot }[name]
    expect(called).toHaveBeenCalledOnce()
  })

  it('turns a refused command into an AppError', async () => {
    stubGo(() => Promise.reject(new Error('no transfer is running')))

    await expect(spikeBridge.pause()).resolves.toMatchObject({
      ok: false,
      error: { code: 'bridge.callFailed', params: { detail: 'no transfer is running' } },
    })
  })

  it('subscribes to the transfer event, decodes every payload and unsubscribes', () => {
    const { runtime, off } = stubRuntime()
    const onTransfer = vi.fn<(value: unknown) => void>()
    const onError = vi.fn<(error: unknown) => void>()

    const unsubscribe = spikeBridge.subscribe(onTransfer, onError)
    const [name, callback, max] = runtime.EventsOnMultiple.mock.calls[0] ?? []
    expect([name, max]).toEqual([transferEvent, -1])
    callback?.(shared.transfer)
    callback?.({ ...shared.transfer, peers: -1 })
    unsubscribe()

    expect(onTransfer).toHaveBeenCalledExactlyOnceWith(decoded)
    expect(onError).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ code: 'contract.invalidDto' }))
    expect(off).toHaveBeenCalledOnce()
  })

  it('reloads the page through the Wails runtime', () => {
    const { runtime } = stubRuntime()

    spikeBridge.reload()

    expect(runtime.WindowReloadApp).toHaveBeenCalledOnce()
  })
})
