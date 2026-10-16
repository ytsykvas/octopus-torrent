// The only module that calls the generated operation bindings.
import { GetOperation } from '../../wailsjs/go/app/App'
import { callGo } from './call'
import type { Result } from './contract/errors'
import type { OperationID } from './contract/ids'
import { operationStatus, type OperationStatus } from './contract/operations'

/**
 * The current status of an operation. operation.notFound naming another runtime than the one that
 * accepted it means the outcome is unknown: read the full state, never resend the command.
 */
export function getOperation(operationID: OperationID): Promise<Result<OperationStatus>> {
  return callGo(() => GetOperation(operationID), operationStatus)
}
