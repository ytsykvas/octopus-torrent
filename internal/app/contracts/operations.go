package contracts

// OperationStatus is the answer to a non-trivial mutation and to GetOperation. Accepting a command
// returns it with State accepted; the final state is read again by operation ID or seen in the full
// state — an event is never the only evidence of completion. RuntimeID is the run that accepted
// the operation. Error is set exactly when State is failed or cancelled.
type OperationStatus struct {
	OperationID OperationID    `json:"operationID"`
	RuntimeID   RuntimeID      `json:"runtimeID"`
	Kind        OperationKind  `json:"kind"`
	State       OperationState `json:"state"`
	Error       *AppError      `json:"error"`
}
