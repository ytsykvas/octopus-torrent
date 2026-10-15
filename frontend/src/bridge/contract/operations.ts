// Accepted is not completed: a mutation answers with the accepted status, and its final state is
// read again with GetOperation or seen in the full state — an event is never the only evidence.
import { ContractViolation, keyPath, nullable, oneOf, refine, struct, type Decoder } from './decode'
import { operationKinds, operationStates, type OperationKind, type OperationState } from './enums'
import { appError, type AppError } from './errors'
import { uuid, type OperationID, type RuntimeID } from './ids'

export interface OperationStatus {
  operationID: OperationID
  /** The runtime that accepted the operation. */
  runtimeID: RuntimeID
  kind: OperationKind
  state: OperationState
  /** Set exactly when state is failed or cancelled. */
  error: AppError | null
}

export const operationStatus: Decoder<OperationStatus> = refine(
  struct({
    operationID: uuid,
    runtimeID: uuid,
    kind: oneOf(operationKinds),
    state: oneOf(operationStates),
    error: nullable(appError),
  }),
  (status, path) => {
    const ended = status.state === 'failed' || status.state === 'cancelled'
    if (ended !== (status.error !== null)) throw new ContractViolation(keyPath(path, 'error'), ended ? 'null' : 'value')
  },
)
