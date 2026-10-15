import { describe, expect, it } from 'vitest'
import {
  encodeCancelImport,
  encodeConfirmImport,
  encodeRemoveTorrents,
  encodeSetFilesWanted,
  encodeTorrentSelection,
  encodeUpdateSettings,
} from './commands'
import { example } from '../../test/contractExamples'

const operationID = 'c0ffee00-1234-4abc-9def-0123456789ab'
const t1 = '3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b'
const t2 = '7d4e5f6a-1b2c-4d3e-8f4a-5b6c7d8e9f0a'
const pendingImportID = '5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9'

// Each encoder must produce the example the Go tests accept, and only its keys.
describe('command encoders', () => {
  it.each([
    ['pause-torrents.json', encodeTorrentSelection({ operationID, torrentIDs: [t2, t1] })],
    ['set-files-wanted-false.json', encodeSetFilesWanted({ operationID, torrentID: t1, fileIndexes: [0], wanted: false })],
    [
      'confirm-import.json',
      encodeConfirmImport({
        operationID,
        pendingImportID,
        previewVersion: 3,
        selectedFileIndexes: [0],
        destination: '/Users/owner/Downloads',
        startAfterAdding: true,
      }),
    ],
    ['cancel-import.json', encodeCancelImport({ pendingImportID })],
    ['remove-torrents-record-only.json', encodeRemoveTorrents({ operationID, torrentIDs: [t1], mode: 'recordOnly', deletionListVersion: null })],
    [
      'update-settings.json',
      encodeUpdateSettings({
        operationID,
        settings: {
          defaultSavePath: '/Users/owner/Downloads',
          theme: 'system',
          language: 'uk',
          downloadLimit: null,
          uploadLimit: 1048576n,
          globalUploadDisabled: false,
        },
      }),
    ],
  ])('%s', (file, encoded) => {
    expect(encoded).toStrictEqual(example(file))
  })

  it('drops properties the request does not have', () => {
    const extended = { pendingImportID, note: 'local only' }

    expect(encodeCancelImport(extended)).toStrictEqual({ pendingImportID })
  })
})
