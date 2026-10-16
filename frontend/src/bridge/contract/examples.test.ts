import { describe, expect, it } from 'vitest'
import { appError, decodeDto } from './errors'
import { libraryDelta, librarySnapshot, torrentSnapshot } from './library'
import { importPreview } from './importPreview'
import { settings } from './settings'
import { operationStatus } from './operations'
import type { Decoder } from './decode'
import { decodedExample, example, manifest } from '../../test/contractExamples'

const decoders: Record<string, Decoder<unknown>> = {
  LibrarySnapshot: librarySnapshot,
  LibraryDelta: libraryDelta,
  TorrentSnapshot: torrentSnapshot,
  ImportPreview: importPreview,
  Settings: settings,
  OperationStatus: operationStatus,
  AppError: appError,
}

function decoderFor(type: string): Decoder<unknown> {
  const decoder = decoders[type]
  if (decoder === undefined) throw new Error(`no decoder for ${type}`)
  return decoder
}

const toFrontend = manifest.filter((entry) => entry.direction === 'toFrontend')

describe('shared contract examples', () => {
  it('has examples for every DTO the frontend decodes', () => {
    expect(new Set(toFrontend.map((entry) => entry.type))).toStrictEqual(new Set(Object.keys(decoders)))
  })

  it.each(toFrontend.filter((entry) => entry.valid))('decodes the valid example $file', ({ file, type }) => {
    const result = decodeDto(decoderFor(type), example(file))

    expect(result.ok).toBe(true)
  })

  it.each(toFrontend.filter((entry) => !entry.valid))('rejects $file at $path', ({ file, type, path }) => {
    const result = decodeDto(decoderFor(type), example(file))

    expect(result).toMatchObject({ ok: false, error: { code: 'contract.invalidDto', params: { path } } })
  })
})

describe('64-bit quantities', () => {
  it('decode to exact bigints beyond the safe integer range', () => {
    const snapshot = decodedExample(librarySnapshot, 'library-snapshot.json')
    const [first, second] = snapshot.torrents

    expect(snapshot.revision).toBe(9007199254740992n)
    expect(first?.totalBytes).toBe(18446744073709551615n)
    expect(first?.wantedBytes).toBe(9007199254740993n)
    expect(first?.etaSeconds).toBe(0n)
    expect(second?.totalBytes).toBeNull()
    expect(second?.etaSeconds).toBeNull()
  })
})
