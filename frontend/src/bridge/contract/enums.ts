// Closed enumerations of the contract, in the order of testdata/vocabulary.json (checked by both
// test suites). An unknown value is a contract violation; adding one is a contract change.

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

export const importStates = ['waitingForMetadata', 'ready', 'failed', 'cancelled', 'expired'] as const
export type ImportState = (typeof importStates)[number]

export const themes = ['system', 'light', 'dark'] as const
export type Theme = (typeof themes)[number]

export const languages = ['en', 'uk', 'es', 'de', 'fr', 'pt'] as const
export type Language = (typeof languages)[number]

export const removeModes = ['recordOnly', 'withFiles'] as const
export type RemoveMode = (typeof removeModes)[number]

export const operationKinds = [
  'pauseTorrents',
  'resumeTorrents',
  'setFilesWanted',
  'confirmImport',
  'removeTorrents',
  'updateSettings',
] as const
export type OperationKind = (typeof operationKinds)[number]

/** In lifecycle order: accepted → running → one of the three final states. */
export const operationStates = ['accepted', 'running', 'succeeded', 'failed', 'cancelled'] as const
export type OperationState = (typeof operationStates)[number]
