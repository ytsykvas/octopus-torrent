// testdata/vocabulary.json is the one list of every closed enumeration and error code; the Go tests
// compare their lists with the same file, so neither side can add a value alone.
import { describe, expect, it } from 'vitest'
import {
  importSources,
  importStates,
  languages,
  operationKinds,
  operationStates,
  removeModes,
  themes,
  torrentStatuses,
} from './enums'
import { knownErrorCodes } from './errorMessages'
import { example, record } from '../../test/contractExamples'

const vocabulary = record(example('vocabulary.json'))

describe('shared vocabulary', () => {
  it.each([
    ['torrentStatuses', torrentStatuses],
    ['importSources', importSources],
    ['importStates', importStates],
    ['themes', themes],
    ['languages', languages],
    ['removeModes', removeModes],
    ['operationKinds', operationKinds],
    ['operationStates', operationStates],
  ])('%s match Go', (name, values) => {
    expect(values).toStrictEqual(vocabulary[name])
  })

  it('every Go and bridge error code has a message', () => {
    const codes = [vocabulary.goErrorCodes, vocabulary.bridgeErrorCodes].flat()

    expect(knownErrorCodes()).toStrictEqual(codes)
  })
})
