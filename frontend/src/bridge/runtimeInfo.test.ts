import { afterEach, describe, expect, it, vi } from 'vitest'
import { getRuntimeInfo } from './runtimeInfo'

type WailsWindow = { go?: unknown }

function stubGoMethod(impl: () => Promise<unknown>) {
  ;(window as WailsWindow).go = { app: { App: { GetRuntimeInfo: vi.fn(impl) } } }
}

afterEach(() => {
  delete (window as WailsWindow).go
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
