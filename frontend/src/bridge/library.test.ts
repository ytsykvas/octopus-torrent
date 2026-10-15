import { afterEach, describe, expect, it, vi } from 'vitest'
import { getLibrarySnapshot } from './library'
import { example } from '../test/contractExamples'

function stubGoMethod(impl: () => Promise<unknown>) {
  Object.assign(window, { go: { app: { App: { GetLibrarySnapshot: vi.fn<() => Promise<unknown>>(impl) } } } })
}

afterEach(() => {
  Reflect.deleteProperty(window, 'go')
})

describe('getLibrarySnapshot', () => {
  it('decodes the Go answer with exact 64-bit values', async () => {
    stubGoMethod(() => Promise.resolve(example('library-snapshot.json')))

    const result = await getLibrarySnapshot()

    expect(result).toMatchObject({ ok: true, value: { revision: 9007199254740992n } })
  })

  it('reports an answer that breaks the contract', async () => {
    stubGoMethod(() => Promise.resolve({ runtimeID: 'x', revision: '0', torrents: [] }))

    await expect(getLibrarySnapshot()).resolves.toMatchObject({
      ok: false,
      error: { code: 'contract.invalidDto', params: { path: 'runtimeID', reason: 'format' } },
    })
  })

  it('maps a rejected call to an AppError', async () => {
    stubGoMethod(() => Promise.reject(new Error('error parsing arguments')))

    await expect(getLibrarySnapshot()).resolves.toMatchObject({ ok: false, error: { code: 'bridge.callFailed' } })
  })
})
