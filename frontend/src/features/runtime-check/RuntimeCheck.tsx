import { useState } from 'react'
import type { RuntimeInfo } from '../../bridge/runtimeInfo'

type CheckState =
  | { kind: 'idle' }
  | { kind: 'loading' }
  | { kind: 'success'; info: RuntimeInfo }
  | { kind: 'error'; message: string }

interface RuntimeCheckProps {
  loadRuntimeInfo: () => Promise<RuntimeInfo>
}

function statusText(state: CheckState): string {
  switch (state.kind) {
    case 'idle':
      return 'Not checked yet'
    case 'loading':
      return 'Checking…'
    case 'success':
      return `Go ${state.info.goVersion} · ${state.info.os}/${state.info.arch} · SQLite ${state.info.sqliteVersion}`
    case 'error':
      return ''
  }
}

/** Calls the Go facade on demand and shows its answer; proves the React → Go path. */
export function RuntimeCheck({ loadRuntimeInfo }: RuntimeCheckProps) {
  const [state, setState] = useState<CheckState>({ kind: 'idle' })

  async function check() {
    setState({ kind: 'loading' })
    try {
      setState({ kind: 'success', info: await loadRuntimeInfo() })
    } catch (error) {
      setState({ kind: 'error', message: error instanceof Error ? error.message : String(error) })
    }
  }

  return (
    <section aria-labelledby="runtime-check-title">
      <h2 id="runtime-check-title">Go connection</h2>
      <button type="button" onClick={() => void check()}>
        Check Go connection
      </button>
      <p>
        <output>{statusText(state)}</output>
      </p>
      {state.kind === 'error' && <p role="alert">Go call failed: {state.message}</p>}
    </section>
  )
}
