import { afterEach, describe, expect, it, vi } from 'vitest'
import { getOperation } from './operations'
import { example } from '../test/contractExamples'

const operationID = 'c0ffee00-1234-4abc-9def-0123456789ab'

function stubGoMethod(impl: (id: string) => Promise<unknown>) {
  const method = vi.fn<(id: string) => Promise<unknown>>(impl)
  Object.assign(window, { go: { app: { App: { GetOperation: method } } } })
  return method
}

afterEach(() => {
  Reflect.deleteProperty(window, 'go')
})

describe('getOperation', () => {
  it('asks Go for the operation and decodes its status', async () => {
    const method = stubGoMethod(() => Promise.resolve(example('operation-succeeded.json')))

    await expect(getOperation(operationID)).resolves.toMatchObject({ ok: true, value: { state: 'succeeded' } })
    expect(method).toHaveBeenCalledWith(operationID)
  })

  it('returns the AppError Go rejected the call with', async () => {
    stubGoMethod(() => Promise.reject(new Error(JSON.stringify(example('app-error.json')))))

    await expect(getOperation(operationID)).resolves.toStrictEqual({ ok: false, error: example('app-error.json') })
  })
})
