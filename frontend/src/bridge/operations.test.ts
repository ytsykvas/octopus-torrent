import { describe, expect, it } from 'vitest'
import { stubGoMethod } from '../test/goBinding'
import { getOperation } from './operations'
import { example } from '../test/contractExamples'

const operationID = 'c0ffee00-1234-4abc-9def-0123456789ab'

describe('getOperation', () => {
  it('asks Go for the operation and decodes its status', async () => {
    const method = stubGoMethod('GetOperation', (_id: string) => Promise.resolve(example('operation-succeeded.json')))

    await expect(getOperation(operationID)).resolves.toMatchObject({ ok: true, value: { state: 'succeeded' } })
    expect(method).toHaveBeenCalledWith(operationID)
  })

  it('returns the AppError Go rejected the call with', async () => {
    stubGoMethod('GetOperation', () => Promise.reject(new Error(JSON.stringify(example('app-error.json')))))

    await expect(getOperation(operationID)).resolves.toStrictEqual({ ok: false, error: example('app-error.json') })
  })
})
