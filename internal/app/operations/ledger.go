// Package operations runs the non-trivial mutations the frontend asks for, once per operation ID.
// Accepting a command and completing it are separate: Start returns at once with the accepted
// status, and the final state is read again with Get (or seen in the full state), so a response
// lost on the bridge never hides the outcome and a retry never repeats the mutation.
package operations

import (
	"context"
	"sync"

	"octopustorrent/internal/app/contracts"
)

// Mutation performs one operation. It returns nil when the change took effect, an error with
// contracts.CodeOperationCancelled when it stopped before taking effect because ctx was cancelled,
// or any other error when it failed. Only the mutation knows whether it already committed, so a
// late cancellation does not undo a finished change.
type Mutation func(ctx context.Context) *contracts.AppError

// Ledger records the operations of one runtime in memory and is the only owner of their
// goroutines. Entries are never evicted: an evicted ID would let a retry run its mutation again.
// One entry is a few hundred bytes per human command; persistence of import and deletion
// operations across runs belongs to the Operations table (task 014).
type Ledger struct {
	runtimeID contracts.RuntimeID
	root      context.Context
	stop      context.CancelFunc
	running   sync.WaitGroup

	mu      sync.Mutex
	stopped bool
	entries map[contracts.OperationID]*entry
}

type entry struct {
	fingerprint string
	status      contracts.OperationStatus
	cancel      context.CancelFunc
}

// NewLedger makes the ledger of the runtime runtimeID.
func NewLedger(runtimeID contracts.RuntimeID) *Ledger {
	root, stop := context.WithCancel(context.Background())
	return &Ledger{runtimeID: runtimeID, root: root, stop: stop, entries: map[contracts.OperationID]*entry{}}
}

// RuntimeID is the runtime whose operations this ledger records.
func (l *Ledger) RuntimeID() contracts.RuntimeID { return l.runtimeID }

// Start accepts the operation id and runs mutation in its own goroutine. fingerprint identifies the
// request (contracts.Fingerprint). The same id with the same kind and fingerprint returns the
// current status without running anything; with another kind or fingerprint it is
// operation.idConflict. After Shutdown nothing starts: operation.cancelled.
func (l *Ledger) Start(id contracts.OperationID, kind contracts.OperationKind, fingerprint string, mutation Mutation) (contracts.OperationStatus, *contracts.AppError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, found := l.entries[id]; found {
		if existing.status.Kind != kind || existing.fingerprint != fingerprint {
			return contracts.OperationStatus{}, contracts.NewAppError(contracts.CodeOperationIDConflict, nil).WithOperation(id)
		}
		return existing.status, nil
	}
	if l.stopped {
		return contracts.OperationStatus{}, contracts.NewAppError(contracts.CodeOperationCancelled, nil).WithOperation(id)
	}
	ctx, cancel := context.WithCancel(l.root)
	e := &entry{
		fingerprint: fingerprint,
		status:      contracts.OperationStatus{OperationID: id, RuntimeID: l.runtimeID, Kind: kind, State: contracts.OperationAccepted},
		cancel:      cancel,
	}
	l.entries[id] = e
	l.running.Go(func() {
		defer cancel()
		l.setState(e, contracts.OperationRunning, nil)
		problem := mutation(ctx)
		switch {
		case problem == nil:
			l.setState(e, contracts.OperationSucceeded, nil)
		case problem.Code == contracts.CodeOperationCancelled:
			l.setState(e, contracts.OperationCancelled, withOperation(problem, id))
		default:
			l.setState(e, contracts.OperationFailed, withOperation(problem, id))
		}
	})
	return e.status, nil
}

// Get returns the current status of the operation, or operation.notFound naming this runtime.
func (l *Ledger) Get(id contracts.OperationID) (contracts.OperationStatus, *contracts.AppError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, found := l.entries[id]
	if !found {
		return contracts.OperationStatus{}, l.notFound(id)
	}
	return e.status, nil
}

// Cancel asks the operation to stop and returns its current status. Whether it ends cancelled is
// decided by its mutation; a finished operation keeps its final state.
func (l *Ledger) Cancel(id contracts.OperationID) (contracts.OperationStatus, *contracts.AppError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, found := l.entries[id]
	if !found {
		return contracts.OperationStatus{}, l.notFound(id)
	}
	e.cancel()
	return e.status, nil
}

// Shutdown refuses new operations, cancels the running ones and waits until their goroutines end
// or ctx ends, whichever comes first; in the second case it returns ctx's error. Statuses stay
// readable afterwards.
func (l *Ledger) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	l.stopped = true
	l.mu.Unlock()
	l.stop()
	done := make(chan struct{})
	go func() {
		l.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Ledger) setState(e *entry, state contracts.OperationState, problem *contracts.AppError) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.status.State, e.status.Error = state, problem
}

func (l *Ledger) notFound(id contracts.OperationID) *contracts.AppError {
	return contracts.NewAppError(contracts.CodeOperationNotFound, map[string]string{"runtimeID": string(l.runtimeID)}).WithOperation(id)
}

// withOperation returns a copy of problem that names the operation; the mutation's own value is
// left as it was.
func withOperation(problem *contracts.AppError, id contracts.OperationID) *contracts.AppError {
	named := *problem
	return named.WithOperation(id)
}
