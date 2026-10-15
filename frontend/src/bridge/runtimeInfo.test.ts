import { afterEach, describe, expect, it, vi } from 'vitest'
import { getRuntimeInfo } from './runtimeInfo'

function stubGoMethod(impl: () => Promise<unknown>) {
  Object.assign(window, { go: { app: { App: { GetRuntimeInfo: vi.fn<() => Promise<unknown>>(impl) } } } })
}

afterEach(() => {
  Reflect.deleteProperty(window, 'go')
})

describe('getRuntimeInfo', () => {
  it('returns the Go facade answer as a plain RuntimeInfo DTO', async () => {
    stubGoMethod(() => Promise.resolve({ goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64' }))

    await expect(getRuntimeInfo()).resolves.toStrictEqual({ goVersion: 'go1.27.2', os: 'darwin', arch: 'arm64' })
  })

  it('propagates a failed Go call', async () => {
    stubGoMethod(() => Promise.reject(new Error('binding failed')))

    await expect(getRuntimeInfo()).rejects.toThrow('binding failed')
  })
})
