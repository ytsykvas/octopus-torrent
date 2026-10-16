import { useCallback, useEffect, useState } from 'react'
import type { AppError, Result } from '../bridge/contract/errors'
import type { SpikeBridge, Transfer } from './bridge'

interface SpikeAppProps {
  bridge: SpikeBridge
}

function percent(t: Transfer): bigint {
  return t.length === 0n ? 0n : (t.bytesCompleted * 100n) / t.length
}

function describe(error: AppError): string {
  return `${error.code}: ${Object.values(error.params).join(' ')}`
}

/**
 * The engine spike's page: a real transfer between two loopback engine clients, read through the
 * spike's DTO, binding and event. It subscribes before it asks for the snapshot, so no event is lost
 * in between, and it keeps only the transfer with the highest revision, whichever way it arrived:
 * a slow reply never puts an older state back on the screen.
 */
export function SpikeApp({ bridge }: SpikeAppProps) {
  const [current, setCurrent] = useState<Transfer | null>(null)
  const [problem, setProblem] = useState<AppError | null>(null)

  const apply = useCallback((next: Transfer) => {
    setCurrent((shown) => (shown !== null && shown.revision >= next.revision ? shown : next))
  }, [])

  const settle = useCallback(
    (result: Result<Transfer>) => {
      if (result.ok) apply(result.value)
      else setProblem(result.error)
    },
    [apply],
  )

  useEffect(() => {
    let active = true
    const unsubscribe = bridge.subscribe(apply, setProblem)
    void bridge.snapshot().then((result) => {
      if (active) settle(result)
    })
    return () => {
      active = false
      unsubscribe()
    }
  }, [bridge, apply, settle])

  const state = current?.state
  async function send(command: () => Promise<Result<Transfer>>) {
    settle(await command())
  }

  return (
    <main>
      <h1>Engine spike</h1>
      <p>
        State: <strong>{state ?? 'not loaded'}</strong>
      </p>
      {current !== null && (
        <>
          <p>
            {`${current.bytesCompleted} / ${current.length} bytes (${percent(current)}%)`}
          </p>
          <p>{`${current.peers} peer${current.peers === 1 ? '' : 's'}, ${current.clientsStarted} engine clients started`}</p>
        </>
      )}
      <p>
        <button type="button" disabled={state !== 'idle' && state !== 'failed'} onClick={() => void send(bridge.start)}>
          Start
        </button>{' '}
        <button type="button" disabled={state !== 'running'} onClick={() => void send(bridge.pause)}>
          Pause
        </button>{' '}
        <button type="button" disabled={state !== 'paused'} onClick={() => void send(bridge.resume)}>
          Resume
        </button>{' '}
        <button type="button" onClick={() => bridge.reload()}>
          Reload UI
        </button>
      </p>
      {current?.state === 'failed' && <p role="alert">Run failed: {current.error}</p>}
      {problem !== null && <p role="alert">{describe(problem)}</p>}
    </main>
  )
}
