// The only module that calls the generated Wails bindings for runtime info.
// UI code depends on this own DTO, never on the generated classes.
import { GetRuntimeInfo } from '../../wailsjs/go/app/App'

/** Go runtime and SQLite compiled into the running application (Go DTO app.RuntimeInfo). */
export interface RuntimeInfo {
  goVersion: string
  os: string
  arch: string
  sqliteVersion: string
}

export async function getRuntimeInfo(): Promise<RuntimeInfo> {
  const { goVersion, os, arch, sqliteVersion } = await GetRuntimeInfo()
  return { goVersion, os, arch, sqliteVersion }
}
