// The runtime the application was compiled with (Go DTO app.RuntimeInfo in internal/app/app.go).
import { string, struct, type Decoder } from './decode'

/** Go runtime and SQLite compiled into the running application. */
export interface RuntimeInfo {
  goVersion: string
  os: string
  arch: string
  sqliteVersion: string
}

export const runtimeInfo: Decoder<RuntimeInfo> = struct({
  goVersion: string,
  os: string,
  arch: string,
  sqliteVersion: string,
})
