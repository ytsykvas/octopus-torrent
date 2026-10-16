import { describe, expect, it } from 'vitest'
import {
  ContractViolation,
  array,
  boolean,
  integer,
  nullable,
  oneOf,
  refine,
  string,
  stringMap,
  struct,
  timestamp,
  type Decoder,
} from './decode'

function violation(decoder: Decoder<unknown>, value: unknown): { path: string; reason: string } {
  try {
    decoder(value, 'root')
  } catch (error) {
    if (error instanceof ContractViolation) return { path: error.path, reason: error.reason }
    throw error
  }
  throw new Error('no violation')
}

describe('primitive decoders', () => {
  it('accept values of their type', () => {
    expect(string('a', '')).toBe('a')
    expect(boolean(false, '')).toBe(false)
    expect(integer(0)(Number.MAX_SAFE_INTEGER, '')).toBe(Number.MAX_SAFE_INTEGER)
    expect(nullable(string)(null, '')).toBeNull()
    expect(nullable(string)('x', '')).toBe('x')
    expect(oneOf(['a', 'b'])('b', '')).toBe('b')
    expect(array(integer(0))([1, 2], '')).toStrictEqual([1, 2])
    expect(stringMap({ a: 'b' }, '')).toStrictEqual({ a: 'b' })
    expect(timestamp('2026-10-15T12:00:00.5Z', '')).toBe('2026-10-15T12:00:00.5Z')
  })

  it.each([
    ['string', string, 1, 'root', 'type'],
    ['boolean', boolean, 'true', 'root', 'type'],
    ['integer of another type', integer(0), '1', 'root', 'type'],
    ['unsafe integer', integer(0), 2 ** 53, 'root', 'type'],
    ['fraction', integer(0), 1.5, 'root', 'type'],
    ['integer below its minimum', integer(1), 0, 'root', 'range'],
    ['null outside nullable', string, null, 'root', 'null'],
    ['unknown enumeration value', oneOf(['a']), 'b', 'root', 'value'],
    ['enumeration of another type', oneOf(['a']), 1, 'root', 'type'],
    ['array of another type', array(string), {}, 'root', 'type'],
    ['bad array item', array(string), ['a', 1], 'root[1]', 'type'],
    ['map of another type', stringMap, [], 'root', 'type'],
    ['bad map value', stringMap, { a: 1 }, 'root.a', 'type'],
    ['timestamp of another type', timestamp, 1, 'root', 'type'],
    ['timestamp not in UTC', timestamp, '2026-10-15T12:00:00+02:00', 'root', 'format'],
    ['impossible date', timestamp, '2026-02-30T12:00:00Z', 'root', 'format'],
  ])('reject %s', (_name, decoder, value, path, reason) => {
    expect(violation(decoder as Decoder<unknown>, value)).toStrictEqual({ path, reason })
  })
})

describe('struct', () => {
  const point = struct({ x: integer(0), label: nullable(string) })

  it('decodes exactly the declared keys', () => {
    expect(point({ x: 1, label: null }, '')).toStrictEqual({ x: 1, label: null })
  })

  it.each([
    ['a missing key', { label: null }, 'root.x', 'missing'],
    ['an unknown key', { x: 1, label: null, extra: true }, 'root.extra', 'unknown'],
    ['a bad value', { x: -1, label: null }, 'root.x', 'range'],
    ['null', null, 'root', 'null'],
    ['an array', [], 'root', 'type'],
  ])('rejects %s', (_name, value, path, reason) => {
    expect(violation(point, value)).toStrictEqual({ path, reason })
  })

  it('names fields without a leading dot at the root', () => {
    expect(() => point({}, '')).toThrow(expect.objectContaining({ path: 'x' }))
  })

  it('does not take inherited keys for present ones', () => {
    expect(violation(struct({ toString: string }), {})).toStrictEqual({ path: 'root.toString', reason: 'missing' })
  })
})

describe('refine', () => {
  it('runs a cross-field check after decoding', () => {
    const positive = refine(integer(0), (value, path) => {
      if (value === 0) throw new ContractViolation(path, 'range')
    })

    expect(positive(1, '')).toBe(1)
    expect(violation(positive, 0)).toStrictEqual({ path: 'root', reason: 'range' })
  })
})
