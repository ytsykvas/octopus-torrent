import { describe, expect, it, vi } from 'vitest'
import { ContractViolation, type Decoder } from './decode'
import { decodeDto } from './errors'
import { infohashV1, infohashV2, newOperationID, uuid } from './ids'
import { libraryDelta, torrentSnapshot } from './library'
import { MAX_UINT64, decimal, encodeDecimal } from './numbers'
import { operationStatus } from './operations'
import { example, record } from '../../test/contractExamples'

function reason(decoder: Decoder<unknown>, value: unknown): string {
  try {
    decoder(value, '')
  } catch (error) {
    if (error instanceof ContractViolation) return `${error.path} ${error.reason}`
    throw error
  }
  return 'accepted'
}

describe('decimal strings', () => {
  it.each([
    ['0', 'accepted'],
    ['9007199254740991', 'accepted'],
    ['9007199254740992', 'accepted'],
    ['18446744073709551615', 'accepted'],
    ['18446744073709551616', ' range'],
    ['-1', ' format'],
    ['01', ' format'],
    ['1e3', ' format'],
    [' 1', ' format'],
    ['', ' format'],
    ['1'.repeat(10_000), ' range'],
  ])('%s → %s', (text, expected) => {
    expect(reason(decimal, text)).toBe(expected)
  })

  it('encode bigints and refuse values outside 64 bits', () => {
    expect(encodeDecimal(MAX_UINT64)).toBe('18446744073709551615')
    expect(encodeDecimal(0n)).toBe('0')
    expect(() => encodeDecimal(-1n)).toThrow(RangeError)
    expect(() => encodeDecimal(MAX_UINT64 + 1n)).toThrow(RangeError)
  })
})

describe('identifiers', () => {
  it('makes operation IDs the Go side accepts, without needing a secure context', () => {
    const randomUUID = vi.spyOn(crypto, 'randomUUID')
    const ids = Array.from({ length: 200 }, () => newOperationID())

    expect(ids.every((id) => reason(uuid, id) === 'accepted')).toBe(true)
    expect(new Set(ids).size).toBe(ids.length)
    expect(randomUUID).not.toHaveBeenCalled()
  })

  it.each([
    [uuid, '3F2A7C1E-5B4D-4E8F-9A6B-1C2D3E4F5A6B'],
    [uuid, '3f2a7c1e-5b4d-1e8f-9a6b-1c2d3e4f5a6b'],
    [infohashV1, '0123456789abcdef0123456789abcdef0123456'],
    [infohashV2, '0123456789abcdef0123456789abcdef01234567'],
  ])('rejects %#', (decoder, text) => {
    expect(reason(decoder, text)).toBe(' format')
  })
})

describe('cross-field rules', () => {
  const delta = record(example('library-delta.json'))
  const upserted = delta.upserted
  const waiting = record(Array.isArray(upserted) ? upserted[0] : undefined)
  const failed = record(example('operation-failed.json'))

  it('allows no file before the metadata is known', () => {
    expect(reason(torrentSnapshot, { ...waiting, files: [{ index: 0, path: 'a', sizeBytes: '1', completedBytes: '0', wanted: true }] })).toBe(
      'files[0].index range',
    )
  })

  it('requires a delta to move forward', () => {
    expect(reason(libraryDelta, { ...delta, revision: '41' })).toBe('revision range')
  })

  it('requires an error exactly for failed and cancelled operations', () => {
    expect(reason(operationStatus, { ...failed, error: null })).toBe('error null')
    expect(reason(operationStatus, { ...failed, state: 'succeeded' })).toBe('error value')
    expect(decodeDto(operationStatus, example('operation-succeeded.json')).ok).toBe(true)
  })
})
