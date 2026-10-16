// The shared contract examples (internal/app/contracts/testdata): the Go tests read the same files,
// so one set of examples is the contract for both languages.
import type { Decoder } from '../bridge/contract/decode'
import { decodeDto } from '../bridge/contract/errors'

export interface ManifestEntry {
  file: string
  type: string
  direction: 'toFrontend' | 'toGo'
  valid: boolean
  path?: string
}

const files: Record<string, unknown> = import.meta.glob('../../../internal/app/contracts/testdata/*.json', {
  eager: true,
  import: 'default',
})

export function example(name: string): unknown {
  const value = files[`../../../internal/app/contracts/testdata/${name}`]
  if (value === undefined) throw new Error(`no contract example ${name}`)
  return value
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/** An object inside an example, to derive a variant from with a spread. */
export function record(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) throw new Error(`not an object: ${JSON.stringify(value)}`)
  return value
}

/** An example decoded by the production decoder; a failure fails the test. */
export function decodedExample<T>(decoder: Decoder<T>, name: string): T {
  const result = decodeDto(decoder, example(name))
  if (!result.ok) throw new Error(`${name}: ${JSON.stringify(result.error)}`)
  return result.value
}

function isManifestEntry(value: unknown): value is ManifestEntry {
  return (
    isRecord(value) &&
    typeof value.file === 'string' &&
    typeof value.type === 'string' &&
    (value.direction === 'toFrontend' || value.direction === 'toGo') &&
    typeof value.valid === 'boolean' &&
    (value.path === undefined || typeof value.path === 'string')
  )
}

const entries = example('manifest.json')
if (!Array.isArray(entries) || !entries.every(isManifestEntry)) throw new Error('manifest.json does not match ManifestEntry')

export const manifest: readonly ManifestEntry[] = entries
