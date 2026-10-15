// The library state: full snapshots and deltas (ADR api-contract). Field meanings and units are
// documented on the Go DTOs in internal/app/contracts/library.go.
import {
  ContractViolation,
  array,
  boolean,
  integer,
  keyPath,
  nullable,
  oneOf,
  refine,
  string,
  struct,
  timestamp,
  type Decoder,
} from './decode'
import { appError, type AppError } from './errors'
import { infohashV1, infohashV2, uuid, type RuntimeID, type TorrentID } from './ids'
import { decimal } from './numbers'

/** Display status vocabulary; which one wins is the transfer policy's decision (tasks 013, 027). */
export const torrentStatuses = [
  'waitingForMetadata',
  'checking',
  'downloading',
  'paused',
  'completed',
  'seeding',
  'noFilesSelected',
  'error',
  'deleting',
  'relinking',
] as const
export type TorrentStatus = (typeof torrentStatuses)[number]

export const importSources = ['file', 'finder', 'drop', 'magnet', 'url', 'watchFolder'] as const
export type ImportSource = (typeof importSources)[number]

export interface TorrentFile {
  /** Engine file index, 0…fileCount − 1. */
  index: number
  /** Relative, "/"-separated, without the torrent's root folder; untrusted text. */
  path: string
  sizeBytes: bigint
  completedBytes: bigint
  wanted: boolean
}

export interface TorrentSnapshot {
  id: TorrentID
  infohashV1: string | null
  infohashV2: string | null
  name: string
  status: TorrentStatus
  manualPaused: boolean
  uploadAllowed: boolean
  /** null until the metadata is known; 0n means known and empty. */
  totalBytes: bigint | null
  wantedBytes: bigint | null
  completedBytes: bigint
  downloadBytesPerSecond: bigint
  uploadBytesPerSecond: bigint
  peerCount: number
  /** null when not computable; 0n means under a second. */
  etaSeconds: bigint | null
  savePath: string
  source: ImportSource
  /** RFC 3339 UTC. */
  addedAt: string
  fileCount: number | null
  selectedFileCount: number | null
  files: TorrentFile[]
  errors: AppError[]
}

export interface LibrarySnapshot {
  runtimeID: RuntimeID
  revision: bigint
  torrents: TorrentSnapshot[]
}

export interface LibraryDelta {
  runtimeID: RuntimeID
  baseRevision: bigint
  revision: bigint
  upserted: TorrentSnapshot[]
  removed: TorrentID[]
}

const torrentFile: Decoder<TorrentFile> = struct({
  index: integer(0),
  path: string,
  sizeBytes: decimal,
  completedBytes: decimal,
  wanted: boolean,
})

export const torrentSnapshot: Decoder<TorrentSnapshot> = refine(
  struct({
    id: uuid,
    infohashV1: nullable(infohashV1),
    infohashV2: nullable(infohashV2),
    name: string,
    status: oneOf(torrentStatuses),
    manualPaused: boolean,
    uploadAllowed: boolean,
    totalBytes: nullable(decimal),
    wantedBytes: nullable(decimal),
    completedBytes: decimal,
    downloadBytesPerSecond: decimal,
    uploadBytesPerSecond: decimal,
    peerCount: integer(0),
    etaSeconds: nullable(decimal),
    savePath: string,
    source: oneOf(importSources),
    addedAt: timestamp,
    fileCount: nullable(integer(0)),
    selectedFileCount: nullable(integer(0)),
    files: array(torrentFile),
    errors: array(appError),
  }),
  (torrent, path) => {
    if (torrent.infohashV1 === null && torrent.infohashV2 === null) {
      throw new ContractViolation(keyPath(path, 'infohashV2'), 'null')
    }
    const fileCount = torrent.fileCount ?? 0
    const outside = torrent.files.findIndex((file) => file.index >= fileCount)
    if (outside !== -1) throw new ContractViolation(keyPath(path, `files[${outside}].index`), 'range')
  },
)

export const librarySnapshot: Decoder<LibrarySnapshot> = struct({
  runtimeID: uuid,
  revision: decimal,
  torrents: array(torrentSnapshot),
})

/** A delta always moves the library forward: revision > baseRevision. */
export const libraryDelta: Decoder<LibraryDelta> = refine(
  struct({
    runtimeID: uuid,
    baseRevision: decimal,
    revision: decimal,
    upserted: array(torrentSnapshot),
    removed: array(uuid),
  }),
  (delta, path) => {
    if (delta.revision <= delta.baseRevision) throw new ContractViolation(keyPath(path, 'revision'), 'range')
  },
)
