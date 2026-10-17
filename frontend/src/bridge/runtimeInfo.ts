// The only module that calls the generated runtime info binding.
import { GetRuntimeInfo } from '../../wailsjs/go/app/App'
import { callGo } from './call'
import type { Result } from './contract/errors'
import { runtimeInfo, type RuntimeInfo } from './contract/runtimeInfo'

/** The Go runtime and SQLite compiled into the running application. */
export function getRuntimeInfo(): Promise<Result<RuntimeInfo>> {
  return callGo(() => GetRuntimeInfo(), runtimeInfo)
}
