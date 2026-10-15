// What the UI shows for an AppError until localization (task 041) owns the texts: a message key,
// its parameters and an English fallback. Each task that adds an error code adds it here, to Go's
// contracts/errors.go, to testdata/vocabulary.json and to the ADR table.
import type { AppError } from './errors'

export interface ErrorDescription {
  key: string
  params: Record<string, string>
  fallback: string
}

// A Map, not an object literal: a code such as "constructor" must not find an inherited member.
const fallbacks = new Map<string, string>([
  ['request.invalid', 'The request was not valid.'],
  ['operation.idConflict', 'This action conflicts with an earlier one.'],
  ['operation.notFound', 'The result of this action is unknown; the current state is shown.'],
  ['operation.cancelled', 'The action was cancelled.'],
  ['internal.unexpected', 'Something went wrong.'],
  ['bridge.callFailed', 'The application did not answer.'],
  ['contract.invalidDto', 'The application sent data this window cannot read.'],
])

/** The codes with a message, in catalogue order: Go's codes, then the bridge's own. */
export function knownErrorCodes(): string[] {
  return [...fallbacks.keys()]
}

export function describeError(error: AppError): ErrorDescription {
  const fallback = fallbacks.get(error.code)
  if (fallback === undefined) {
    return { key: 'error.unknown', params: { code: error.code }, fallback: `Unexpected error (${error.code}).` }
  }
  // bridge.callFailed carries raw diagnostic text (local paths, Wails messages) not meant for display.
  const params = error.code === 'bridge.callFailed' ? {} : error.params
  return { key: `error.${error.code}`, params, fallback }
}
