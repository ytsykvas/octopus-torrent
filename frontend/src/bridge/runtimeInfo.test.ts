import { describe, expect, it } from 'vitest'
import { stubGoMethod } from '../test/goBinding'
import { getRuntimeInfo } from './runtimeInfo'

describe('getRuntimeInfo', () => {
  it('returns the Go facade answer, with the bundled SQLite, as a plain RuntimeInfo DTO', async () => {
    stubGoMethod('GetRuntimeInfo', () => Promise.resolve({ goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64', sqliteVersion: '3.53.4' }))

    await expect(getRuntimeInfo()).resolves.toStrictEqual({
      goVersion: 'go1.27.2',
      os: 'darwin',
      arch: 'arm64',
      sqliteVersion: '3.53.4',
    })
  })

  it('propagates a failed Go call', async () => {
    stubGoMethod('GetRuntimeInfo', () => Promise.reject(new Error('binding failed')))

    await expect(getRuntimeInfo()).rejects.toThrow('binding failed')
  })
})
