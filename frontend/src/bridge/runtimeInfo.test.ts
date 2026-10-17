import { describe, expect, it } from 'vitest'
import { stubGoMethod } from '../test/goBinding'
import { getRuntimeInfo } from './runtimeInfo'

// The same values as the Go JSON pin, internal/app/app_test.go TestRuntimeInfoJSONUsesBridgeFieldNames.
const answer = { goVersion: 'go1.0', os: 'darwin', arch: 'arm64', sqliteVersion: '3.53.4' }

describe('getRuntimeInfo', () => {
  it('decodes the Go facade answer, with the bundled SQLite', async () => {
    stubGoMethod('GetRuntimeInfo', () => Promise.resolve(answer))

    await expect(getRuntimeInfo()).resolves.toStrictEqual({ ok: true, value: answer })
  })

  it('reports an answer that breaks the contract', async () => {
    stubGoMethod('GetRuntimeInfo', () => Promise.resolve({ goVersion: 'go1.0', os: 'darwin', arch: 'arm64' }))

    await expect(getRuntimeInfo()).resolves.toMatchObject({
      ok: false,
      error: { code: 'contract.invalidDto', params: { path: 'sqliteVersion', reason: 'missing' } },
    })
  })

  it('maps a rejected call to an AppError', async () => {
    stubGoMethod('GetRuntimeInfo', () => Promise.reject(new Error('binding failed')))

    await expect(getRuntimeInfo()).resolves.toMatchObject({ ok: false, error: { code: 'bridge.callFailed' } })
  })
})
