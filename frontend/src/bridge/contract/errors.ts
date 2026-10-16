// AppError: the one error shape of the contract (ADR api-contract). Go returns it from every facade
// method; the bridge adds its own codes for failures Go never saw.
import { ContractViolation, nullable, string, stringMap, struct, type Decoder, type ViolationReason } from './decode'
import { uuid } from './ids'

export interface AppError {
  /** Stable "<area>.<problem>" code; never UI text. */
  code: string
  operationID: string | null
  torrentID: string | null
  /** Localization parameters. */
  params: Record<string, string>
}

export type Result<T> = { ok: true; value: T } | { ok: false; error: AppError }

export const appError: Decoder<AppError> = struct({
  code: string,
  operationID: nullable(uuid),
  torrentID: nullable(uuid),
  params: stringMap,
})

/** A Go answer that does not match the contract. */
export function invalidDto(path: string, reason: ViolationReason): AppError {
  return { code: 'contract.invalidDto', operationID: null, torrentID: null, params: { path, reason } }
}

/**
 * A call that failed before Go could answer with an AppError (Wails argument parsing, a missing
 * method). The text is kept for diagnostics only; it is not shown as the message.
 */
export function callFailed(detail: string): AppError {
  return { code: 'bridge.callFailed', operationID: null, torrentID: null, params: { detail } }
}

/** Decodes an untrusted Go answer; a contract violation becomes contract.invalidDto. */
export function decodeDto<T>(decoder: Decoder<T>, value: unknown): Result<T> {
  try {
    return { ok: true, value: decoder(value, '') }
  } catch (error) {
    if (error instanceof ContractViolation) return { ok: false, error: invalidDto(error.path, error.reason) }
    throw error
  }
}

/**
 * Maps the rejection of a generated binding to an AppError. The Wails runtime rejects with
 * new Error(text), and Go's error formatter makes that text the JSON of the AppError; anything else
 * is bridge.callFailed.
 */
export function fromRejection(reason: unknown): AppError {
  const text = reason instanceof Error ? reason.message : String(reason)
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return callFailed(text)
  }
  const decoded = decodeDto(appError, parsed)
  return decoded.ok ? decoded.value : callFailed(text)
}
