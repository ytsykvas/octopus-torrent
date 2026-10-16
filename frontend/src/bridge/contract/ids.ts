// Identifiers are version 4 UUIDs in lower-case canonical text. Go makes torrent, pending-import and
// runtime IDs; the frontend makes operation IDs, once per user intention, and repeats one when it
// retries so the mutation runs once.
import { ContractViolation, string, type Decoder } from './decode'

export type TorrentID = string
export type PendingImportID = string
export type RuntimeID = string
export type OperationID = string

const uuidV4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

function matching(pattern: RegExp): Decoder<string> {
  return (value, path) => {
    const text = string(value, path)
    if (!pattern.test(text)) throw new ContractViolation(path, 'format')
    return text
  }
}

export const uuid: Decoder<string> = matching(uuidV4)

/** A v1 infohash: the SHA-1 of the info dictionary in lower-case hex. */
export const infohashV1: Decoder<string> = matching(/^[0-9a-f]{40}$/)

/** A v2 infohash: the full SHA-256 in lower-case hex. */
export const infohashV2: Decoder<string> = matching(/^[0-9a-f]{64}$/)

/**
 * A new operation ID for one user intention: a version 4 UUID built from crypto.getRandomValues,
 * like Go's NewUUID. crypto.randomUUID needs a secure context, which the packaged app's wails://
 * scheme is not declared to be.
 */
export function newOperationID(): OperationID {
  const hex = Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte, index) => {
    if (index === 6) byte = (byte & 0x0f) | 0x40 // version 4
    if (index === 8) byte = (byte & 0x3f) | 0x80 // RFC 4122 variant
    return byte.toString(16).padStart(2, '0')
  }).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
