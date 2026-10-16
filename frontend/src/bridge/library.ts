// The only module that calls the generated library bindings.
import { GetLibrarySnapshot } from '../../wailsjs/go/app/App'
import { callGo } from './call'
import type { Result } from './contract/errors'
import { librarySnapshot, type LibrarySnapshot } from './contract/library'

/** The full library state, authoritative for its runtime (STACK-02). */
export function getLibrarySnapshot(): Promise<Result<LibrarySnapshot>> {
  return callGo(() => GetLibrarySnapshot(), librarySnapshot)
}
