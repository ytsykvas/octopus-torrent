// Strict decoders for the Go–TS contract (ADR api-contract). Go and the frontend ship in one
// binary, so any difference from the contract — a missing or unknown key, a wrong type, an unknown
// enumeration value — is a violation, never something to tolerate. Decoders throw
// ContractViolation; decodeDto (errors.ts) turns it into the contract.invalidDto AppError.

/** Why a value violates the contract; the same vocabulary as Go's request.invalid reasons. */
export type ViolationReason = 'missing' | 'unknown' | 'null' | 'type' | 'format' | 'range' | 'value'

/** A value that does not match the contract, at a JSON path such as "torrents[0].files[1].index". */
export class ContractViolation extends Error {
  constructor(
    readonly path: string,
    readonly reason: ViolationReason,
  ) {
    super(`contract violation at ${path || '(root)'}: ${reason}`)
  }
}

/** Turns an untrusted value at path into T, or throws ContractViolation. */
export type Decoder<T> = (value: unknown, path: string) => T

export function keyPath(path: string, key: string): string {
  return path === '' ? key : `${path}.${key}`
}

function typeViolation(value: unknown, path: string): ContractViolation {
  return new ContractViolation(path, value === null ? 'null' : 'type')
}

export const string: Decoder<string> = (value, path) => {
  if (typeof value !== 'string') throw typeViolation(value, path)
  return value
}

export const boolean: Decoder<boolean> = (value, path) => {
  if (typeof value !== 'boolean') throw typeViolation(value, path)
  return value
}

/** A JSON number that is a safe integer, at least min. */
export function integer(min: number): Decoder<number> {
  return (value, path) => {
    if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw typeViolation(value, path)
    if (value < min) throw new ContractViolation(path, 'range')
    return value
  }
}

/** The decoded value, or null for null: null always has a stated meaning in the contract. */
export function nullable<T>(decoder: Decoder<T>): Decoder<T | null> {
  return (value, path) => (value === null ? null : decoder(value, path))
}

function isOneOf<T extends string>(values: readonly T[], text: string): text is T {
  return values.some((candidate) => candidate === text)
}

/** One of a closed enumeration. */
export function oneOf<T extends string>(values: readonly T[]): Decoder<T> {
  return (value, path) => {
    const text = string(value, path)
    if (!isOneOf(values, text)) throw new ContractViolation(path, 'value')
    return text
  }
}

export function array<T>(decoder: Decoder<T>): Decoder<T[]> {
  return (value, path) => {
    if (!Array.isArray(value)) throw typeViolation(value, path)
    return value.map((item: unknown, index) => decoder(item, `${path}[${index}]`))
  }
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function plainObject(value: unknown, path: string): Record<string, unknown> {
  if (!isPlainObject(value)) throw typeViolation(value, path)
  return value
}

/** An object of string values, such as AppError params. */
export const stringMap: Decoder<Record<string, string>> = (value, path) => {
  const object = plainObject(value, path)
  return Object.fromEntries(Object.entries(object).map(([key, item]) => [key, string(item, keyPath(path, key))]))
}

type Decoded<S> = { [K in keyof S]: S[K] extends Decoder<infer T> ? T : never }

// struct sets every key of shape from that key's own decoder before calling this, which is exactly
// what Decoded<S> states; TypeScript cannot follow that through the loop over Object.entries.
function everyKeyDecoded<S>(_decoded: Record<string, unknown>): asserts _decoded is Decoded<S> {}

/** An object with exactly the keys of shape, each decoded by its decoder. */
export function struct<S extends Record<string, Decoder<unknown>>>(shape: S): Decoder<Decoded<S>> {
  return (value, path) => {
    const object = plainObject(value, path)
    const decoded: Record<string, unknown> = {}
    for (const [key, decoder] of Object.entries(shape)) {
      if (!Object.hasOwn(object, key)) throw new ContractViolation(keyPath(path, key), 'missing')
      decoded[key] = decoder(object[key], keyPath(path, key))
    }
    const unknownKey = Object.keys(object).find((key) => !Object.hasOwn(shape, key))
    if (unknownKey !== undefined) throw new ContractViolation(keyPath(path, unknownKey), 'unknown')
    everyKeyDecoded<S>(decoded)
    return decoded
  }
}

/** decoder followed by a check across fields, which throws ContractViolation itself. */
export function refine<T>(decoder: Decoder<T>, check: (value: T, path: string) => void): Decoder<T> {
  return (value, path) => {
    const decoded = decoder(value, path)
    check(decoded, path)
    return decoded
  }
}

// RFC 3339 in UTC as Go's time.Time encodes it: seconds required, fraction optional, "Z" zone.
const utcTimestamp = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$/

/** An RFC 3339 UTC timestamp, kept as its text. */
export const timestamp: Decoder<string> = (value, path) => {
  const text = string(value, path)
  const date = new Date(text)
  if (!utcTimestamp.test(text) || Number.isNaN(date.getTime()) || date.toISOString().slice(0, 10) !== text.slice(0, 10)) {
    throw new ContractViolation(path, 'format')
  }
  return text
}
