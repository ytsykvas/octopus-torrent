import { describe, expect, it } from 'vitest'
import { stubGoMethod } from '../test/goBinding'
import { getLibrarySnapshot } from './library'
import { example } from '../test/contractExamples'

describe('getLibrarySnapshot', () => {
  it('decodes the Go answer with exact 64-bit values', async () => {
    stubGoMethod('GetLibrarySnapshot', () => Promise.resolve(example('library-snapshot.json')))

    const result = await getLibrarySnapshot()

    expect(result).toMatchObject({ ok: true, value: { revision: 9007199254740992n } })
  })

  it('reports an answer that breaks the contract', async () => {
    stubGoMethod('GetLibrarySnapshot', () => Promise.resolve({ runtimeID: 'x', revision: '0', torrents: [] }))

    await expect(getLibrarySnapshot()).resolves.toMatchObject({
      ok: false,
      error: { code: 'contract.invalidDto', params: { path: 'runtimeID', reason: 'format' } },
    })
  })

  it('maps a rejected call to an AppError', async () => {
    stubGoMethod('GetLibrarySnapshot', () => Promise.reject(new Error('error parsing arguments')))

    await expect(getLibrarySnapshot()).resolves.toMatchObject({ ok: false, error: { code: 'bridge.callFailed' } })
  })
})
