// The only module that calls the engine spike's generated bindings (task 007; task 034 deletes the
// spike). It is the spike's own bridge, built on the product's callGo and contract decoders; the
// product's bridge never imports it.
import { Pause, Resume, Snapshot, Start } from '../../spike/wailsjs/go/enginespike/API'
import { EventsOn, WindowReloadApp } from '../../spike/wailsjs/runtime/runtime'
import { callGo } from '../bridge/call'
import { integer, oneOf, string, struct } from '../bridge/contract/decode'
import { decodeDto, type AppError, type Result } from '../bridge/contract/errors'
import { decimal } from '../bridge/contract/numbers'

/** The Wails event that carries every published Transfer (enginespike.Event in Go). */
export const transferEvent = 'enginespike:transfer'

export const transferStates = ['idle', 'running', 'paused', 'complete', 'failed'] as const

/** One published state of the spike; revision orders them (enginespike.Transfer in Go). */
export const transfer = struct({
  revision: decimal,
  state: oneOf(transferStates),
  bytesCompleted: decimal,
  length: decimal,
  payloadRead: decimal,
  payloadWritten: decimal,
  peers: integer(0),
  clientsStarted: integer(0),
  recoveries: integer(0),
  error: string,
})

export type Transfer = ReturnType<typeof transfer>

export interface SpikeBridge {
  start: () => Promise<Result<Transfer>>
  pause: () => Promise<Result<Transfer>>
  resume: () => Promise<Result<Transfer>>
  snapshot: () => Promise<Result<Transfer>>
  /** Calls onTransfer with every decoded event, onError for an event that breaks the contract. */
  subscribe: (onTransfer: (value: Transfer) => void, onError: (error: AppError) => void) => () => void
  reload: () => void
}

export const spikeBridge: SpikeBridge = {
  start: () => callGo(() => Start(), transfer),
  pause: () => callGo(() => Pause(), transfer),
  resume: () => callGo(() => Resume(), transfer),
  snapshot: () => callGo(() => Snapshot(), transfer),
  subscribe: (onTransfer, onError) =>
    EventsOn(transferEvent, (data: unknown) => {
      const decoded = decodeDto(transfer, data)
      if (decoded.ok) onTransfer(decoded.value)
      else onError(decoded.error)
    }),
  reload: () => WindowReloadApp(),
}
