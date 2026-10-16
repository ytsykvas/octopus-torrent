// Saved user preferences (SET-01, SET-04, SET-06, SET-08, TR-04).
import { boolean, nullable, oneOf, struct, string, type Decoder } from './decode'
import { languages, themes, type Language, type Theme } from './enums'
import { decimal, encodeDecimal } from './numbers'

export interface Settings {
  /** Absolute folder for future imports; null = not chosen yet. */
  defaultSavePath: string | null
  theme: Theme
  language: Language
  /** Bytes per second; null = unlimited. 0n is not a limit (Go rejects it): see globalUploadDisabled. */
  downloadLimit: bigint | null
  uploadLimit: bigint | null
  globalUploadDisabled: boolean
}

export const settings: Decoder<Settings> = struct({
  defaultSavePath: nullable(string),
  theme: oneOf(themes),
  language: oneOf(languages),
  downloadLimit: nullable(decimal),
  uploadLimit: nullable(decimal),
  globalUploadDisabled: boolean,
})

function encodeLimit(bytes: bigint | null): string | null {
  return bytes === null ? null : encodeDecimal(bytes)
}

/** The wire form Go decodes. */
export function encodeSettings(value: Settings): Record<string, unknown> {
  return {
    defaultSavePath: value.defaultSavePath,
    theme: value.theme,
    language: value.language,
    downloadLimit: encodeLimit(value.downloadLimit),
    uploadLimit: encodeLimit(value.uploadLimit),
    globalUploadDisabled: value.globalUploadDisabled,
  }
}
