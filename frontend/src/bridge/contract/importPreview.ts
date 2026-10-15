// The preview shown before an import is confirmed (ADD-02, ADD-03, ADD-07, ADD-09).
import { array, boolean, integer, nullable, oneOf, string, struct, type Decoder } from './decode'
import { uuid, type PendingImportID, type TorrentID } from './ids'
import { importSources, type ImportSource } from './library'
import { decimal } from './numbers'

export const importStates = ['waitingForMetadata', 'ready', 'failed', 'cancelled', 'expired'] as const
export type ImportState = (typeof importStates)[number]

export interface PreviewFile {
  index: number
  path: string
  sizeBytes: bigint
  /** The initial choice. */
  selected: boolean
}

export interface ImportPreview {
  pendingImportID: PendingImportID
  /** A confirm repeats it, so a preview that changed meanwhile is not confirmed blindly. */
  previewVersion: number
  source: ImportSource
  state: ImportState
  /** name and totalBytes are null and files empty while a magnet waits for metadata. */
  name: string | null
  files: PreviewFile[]
  totalBytes: bigint | null
  /** The proposed absolute save folder; null when none is chosen yet. */
  destination: string | null
  /** Set when the torrent is already in the library. */
  existingTorrentID: TorrentID | null
}

export const importPreview: Decoder<ImportPreview> = struct({
  pendingImportID: uuid,
  previewVersion: integer(1),
  source: oneOf(importSources),
  state: oneOf(importStates),
  name: nullable(string),
  files: array(struct({ index: integer(0), path: string, sizeBytes: decimal, selected: boolean })),
  totalBytes: nullable(decimal),
  destination: nullable(string),
  existingTorrentID: nullable(uuid),
})
