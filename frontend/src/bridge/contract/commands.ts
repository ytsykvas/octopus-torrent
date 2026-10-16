// Command requests the frontend sends (ADR api-contract). Go decodes them strictly — a missing,
// null, unknown or mistyped key is request.invalid — so each encoder writes exactly the keys of its
// request, whatever else the value carries at runtime. Every non-trivial mutation carries an
// operation ID made once per user intention (newOperationID) and reused on retry.
import type { RemoveMode } from './enums'
import type { OperationID, PendingImportID, TorrentID } from './ids'
import { encodeSettings, type Settings } from './settings'

/** Pause or resume (the facade method says which). */
export interface TorrentSelectionRequest {
  operationID: OperationID
  torrentIDs: TorrentID[]
}

/** Select or skip files of one torrent; a folder is sent as the indexes of its files. */
export interface SetFilesWantedRequest {
  operationID: OperationID
  torrentID: TorrentID
  fileIndexes: number[]
  wanted: boolean
}

export interface ConfirmImportRequest {
  operationID: OperationID
  pendingImportID: PendingImportID
  previewVersion: number
  selectedFileIndexes: number[]
  destination: string
  startAfterAdding: boolean
}

/** Discarding a preview changes nothing in the library: no operation ID. */
export interface CancelImportRequest {
  pendingImportID: PendingImportID
}

/**
 * With files, deletionPlanID is the ID Go issued with the file list the user confirmed (bound to
 * these torrents and files, usable once); for a record-only removal it is null.
 */
export interface RemoveTorrentsRequest {
  operationID: OperationID
  torrentIDs: TorrentID[]
  mode: RemoveMode
  deletionPlanID: string | null
}

export interface UpdateSettingsRequest {
  operationID: OperationID
  settings: Settings
}

export function encodeTorrentSelection(r: TorrentSelectionRequest): Record<string, unknown> {
  return { operationID: r.operationID, torrentIDs: [...r.torrentIDs] }
}

export function encodeSetFilesWanted(r: SetFilesWantedRequest): Record<string, unknown> {
  return { operationID: r.operationID, torrentID: r.torrentID, fileIndexes: [...r.fileIndexes], wanted: r.wanted }
}

export function encodeConfirmImport(r: ConfirmImportRequest): Record<string, unknown> {
  return {
    operationID: r.operationID,
    pendingImportID: r.pendingImportID,
    previewVersion: r.previewVersion,
    selectedFileIndexes: [...r.selectedFileIndexes],
    destination: r.destination,
    startAfterAdding: r.startAfterAdding,
  }
}

export function encodeCancelImport(r: CancelImportRequest): Record<string, unknown> {
  return { pendingImportID: r.pendingImportID }
}

export function encodeRemoveTorrents(r: RemoveTorrentsRequest): Record<string, unknown> {
  return {
    operationID: r.operationID,
    torrentIDs: [...r.torrentIDs],
    mode: r.mode,
    deletionPlanID: r.deletionPlanID,
  }
}

export function encodeUpdateSettings(r: UpdateSettingsRequest): Record<string, unknown> {
  return { operationID: r.operationID, settings: encodeSettings(r.settings) }
}
