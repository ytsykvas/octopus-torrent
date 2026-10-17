import { vi } from 'vitest'

/**
 * Installs a fake of one Go facade method where the generated binding calls it
 * (`window.go.app.App[name]`) and returns the mock. `setup.ts` removes `window.go` after each test.
 */
export function stubGoMethod<A extends unknown[]>(name: string, impl: (...args: A) => Promise<unknown>) {
  const method = vi.fn<(...args: A) => Promise<unknown>>(impl)
  Object.assign(window, { go: { app: { App: { [name]: method } } } })
  return method
}
