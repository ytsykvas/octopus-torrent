import { describe, expect, it } from 'vitest'
import { integer } from './decode'
import { appError, callFailed, decodeDto, describeError, fromRejection } from './errors'
import { decodedExample } from '../../test/contractExamples'

const notFound = decodedExample(appError, 'app-error.json')

function broken(): never {
  throw new TypeError('bug')
}

describe('fromRejection', () => {
  it('reads the AppError Go put into the Error message', () => {
    expect(fromRejection(new Error(JSON.stringify(notFound)))).toStrictEqual(notFound)
  })

  it.each([
    ['an Error with plain text (Wails argument parsing)', new Error('error parsing arguments: bad'), 'error parsing arguments: bad'],
    ['an object turned into text', new Error('[object Object]'), '[object Object]'],
    ['JSON that is not an AppError', new Error('{"code":1}'), '{"code":1}'],
    ['a rejection that is not an Error', 'timeout', 'timeout'],
  ])('maps %s to bridge.callFailed', (_name, reason, detail) => {
    expect(fromRejection(reason)).toStrictEqual(callFailed(detail))
  })
})

describe('decodeDto', () => {
  it('lets errors other than contract violations through', () => {
    expect(() => decodeDto(broken, 1)).toThrow(TypeError)
  })

  it('reports the path and reason of a violation', () => {
    expect(decodeDto(integer(1), 0)).toStrictEqual({
      ok: false,
      error: { code: 'contract.invalidDto', operationID: null, torrentID: null, params: { path: '', reason: 'range' } },
    })
  })
})

describe('describeError', () => {
  it('gives a known code its message key and parameters', () => {
    expect(describeError(notFound)).toStrictEqual({
      key: 'error.operation.notFound',
      params: notFound.params,
      fallback: 'The result of this action is unknown; the current state is shown.',
    })
  })

  it('gives an unknown code the generic message with the code', () => {
    expect(describeError({ ...notFound, code: 'torrent.newProblem' })).toStrictEqual({
      key: 'error.unknown',
      params: { code: 'torrent.newProblem' },
      fallback: 'Unexpected error (torrent.newProblem).',
    })
  })
})
