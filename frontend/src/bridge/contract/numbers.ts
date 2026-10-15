// Every 64-bit quantity — bytes, bytes per second, seconds, revisions — crosses the bridge as a
// decimal string and is a bigint here: JavaScript numbers are exact only up to 2^53 − 1.
import { ContractViolation, string, type Decoder } from './decode'

export const MAX_UINT64 = 2n ** 64n - 1n

// No sign, spaces, exponent or leading zeros: the only form Go writes and accepts.
const decimalText = /^(0|[1-9][0-9]*)$/

/** A decimal string of an unsigned 64-bit integer. */
export const decimal: Decoder<bigint> = (value, path) => {
  const text = string(value, path)
  if (!decimalText.test(text)) throw new ContractViolation(path, 'format')
  const number = BigInt(text)
  if (number > MAX_UINT64) throw new ContractViolation(path, 'range')
  return number
}

/** The wire text of an unsigned 64-bit integer. A value outside that range is a programming error. */
export function encodeDecimal(value: bigint): string {
  if (value < 0n || value > MAX_UINT64) throw new RangeError(`${value} is not an unsigned 64-bit integer`)
  return value.toString()
}
