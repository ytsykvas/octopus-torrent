package transfer

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/commandorder"
)

// PolicyRevision counts the changes of one torrent's policy inputs in this runtime. It is compared
// only within one runtime and is not the snapshot revision of the bridge.
type PolicyRevision uint64

// Entry is the stored state a torrent enters the registry with: from the import (023) or the
// restore (029).
type Entry struct {
	ManualPaused bool
	Phase        Phase
	PayloadFault bool
}

// View is a copy of one torrent's state at one moment.
type View struct {
	ID       contracts.TorrentID
	Input    Input
	Revision PolicyRevision
	// Applied is the last revision whose outcome the engine reported; 0 means none yet.
	Applied PolicyRevision
	// Problem is the engine's problem for Applied; nil means it was applied.
	Problem *contracts.AppError
}

// Pending reports whether the current revision has no outcome yet.
func (v View) Pending() bool { return v.Applied != v.Revision }

// Switches sets one torrent's payload switches in the engine. The engine adapter implements it
// (018): it owns the engine's actual switch state and calls a switch only when it differs from want.
// Apply runs while the step holds the torrent's key, so it must be bounded: it returns when ctx ends
// and puts its own deadline on every engine call, or one hung call blocks every later command for
// the torrent (and a settings save waiting for it).
type Switches interface {
	Apply(ctx context.Context, id contracts.TorrentID, want Permissions) *contracts.AppError
}

var (
	// ErrNotHeld refuses a change made outside its command order: the context does not hold the
	// torrent's key (or the settings key) now.
	ErrNotHeld = errors.New("transfer: the change does not hold its command-order key")
	// ErrUnknown: no torrent with this ID was added in this runtime.
	ErrUnknown = errors.New("transfer: unknown torrent")
	// ErrRemoved: the torrent was removed in this runtime; it never comes back.
	ErrRemoved = errors.New("transfer: the torrent was removed")
	// ErrRemoving: the removal has started; the torrent's intent and phase no longer change.
	ErrRemoving = errors.New("transfer: the torrent is being removed")
	// ErrNotRemoving: Forget is only for a torrent whose removal has started.
	ErrNotRemoving = errors.New("transfer: the torrent's removal has not started")
	// ErrExists: Add of a torrent that is already in the registry.
	ErrExists = errors.New("transfer: the torrent is already added")
	// ErrInvalidPhase: a phase outside the declared ones, or PhaseRemoving outside BeginRemoval.
	ErrInvalidPhase = errors.New("transfer: invalid phase")
)

// Registry is the in-memory owner of every torrent's transfer policy state in this runtime. It
// mirrors what the service already made durable — a mutator is called only after its SQLite write
// succeeded — plus the runtime-only phase and fault. Every change of a torrent requires its key of
// the runtime's command order in ctx (commandorder.Torrent, or commandorder.Settings for the global
// value), so changes happen in acceptance order. Readers need no key.
type Registry struct {
	order        *commandorder.Order // the runtime's one order, shared with the operation ledger
	mu           sync.Mutex
	global       bool // the stored "stop upload for all torrents"
	shuttingDown bool
	entries      map[contracts.TorrentID]*record
	removed      map[contracts.TorrentID]struct{} // tombstones of this runtime
}

// record is the stored state of one torrent.
type record struct {
	manualPaused bool
	phase        Phase
	fault        bool
	revision     PolicyRevision
	applied      PolicyRevision
	problem      *contracts.AppError
}

// inStep reports whether the engine applied the current revision without a problem.
func (e *record) inStep() bool { return e.applied == e.revision && e.problem == nil }

// NewRegistry makes an empty registry. order is the runtime's one command order, the one the
// operation ledger has: a key held in another order is refused. globalUploadDisabled is the stored
// value; there is no default, so a restore never starts with upload allowed by accident.
func NewRegistry(order *commandorder.Order, globalUploadDisabled bool) *Registry {
	return &Registry{order: order, global: globalUploadDisabled, entries: map[contracts.TorrentID]*record{}, removed: map[contracts.TorrentID]struct{}{}}
}

// Add enters a torrent at revision 1. Any phase is accepted, PhaseRemoving included, for a removal
// that was unfinished at a crash.
func (r *Registry) Add(ctx context.Context, id contracts.TorrentID, e Entry) error {
	if !r.order.Holds(ctx, commandorder.Torrent(id)) {
		return ErrNotHeld
	}
	if !e.Phase.valid() {
		return ErrInvalidPhase
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, gone := r.removed[id]; gone {
		return ErrRemoved
	}
	if _, found := r.entries[id]; found {
		return ErrExists
	}
	r.entries[id] = &record{manualPaused: e.ManualPaused, phase: e.Phase, fault: e.PayloadFault, revision: 1}
	return nil
}

// SetManualPaused records the user's pause or resume.
func (r *Registry) SetManualPaused(ctx context.Context, id contracts.TorrentID, paused bool) error {
	return r.change(ctx, id, false, func(e *record) bool {
		changed := e.manualPaused != paused
		e.manualPaused = paused
		return changed
	})
}

// CheckIntentChange returns, without changing anything, the refusal SetManualPaused would return
// now, or nil. The service calls it under the torrent's key before it stores the intent (persist
// first): only a step holding that key changes the torrent, so the answer stays true until the
// step ends, and a resume queued behind a removal is refused before anything is written.
func (r *Registry) CheckIntentChange(ctx context.Context, id contracts.TorrentID) error {
	return r.change(ctx, id, false, func(*record) bool { return false })
}

// SetPhase records the work the application starts or ends on the torrent. PhaseRemoving is set
// only by BeginRemoval.
func (r *Registry) SetPhase(ctx context.Context, id contracts.TorrentID, phase Phase) error {
	if !phase.valid() || phase == PhaseRemoving {
		return ErrInvalidPhase
	}
	return r.change(ctx, id, false, func(e *record) bool {
		changed := e.phase != phase
		e.phase = phase
		return changed
	})
}

// SetPayloadFault records that the torrent's files can (not) be used safely. It also works while
// removing: a failed deletion keeps the record stopped and shows the error.
func (r *Registry) SetPayloadFault(ctx context.Context, id contracts.TorrentID, fault bool) error {
	return r.change(ctx, id, true, func(e *record) bool {
		changed := e.fault != fault
		e.fault = fault
		return changed
	})
}

// BeginRemoval moves the torrent to PhaseRemoving for good: afterwards its intent and phase no
// longer change. An older fault is cleared, so a fault while removing means the deletion failed.
// Calling it again does nothing.
func (r *Registry) BeginRemoval(ctx context.Context, id contracts.TorrentID) error {
	return r.change(ctx, id, true, func(e *record) bool {
		if e.phase == PhaseRemoving {
			return false
		}
		e.phase, e.fault = PhaseRemoving, false
		return true
	})
}

// Forget drops a torrent whose removal finished and leaves a tombstone: every later call for it,
// Add included, is ErrRemoved.
func (r *Registry) Forget(ctx context.Context, id contracts.TorrentID) error {
	if !r.order.Holds(ctx, commandorder.Torrent(id)) {
		return ErrNotHeld
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.find(id)
	if err != nil {
		return err
	}
	if e.phase != PhaseRemoving {
		return ErrNotRemoving
	}
	delete(r.entries, id)
	r.removed[id] = struct{}{}
	return nil
}

// SetGlobalUploadDisabled records the stored global upload value and returns, sorted, the torrents
// to reconcile, each in its own command order: those whose permissions change (their revisions
// move) and those the engine is not in step with — a failed or an unfinished apply — so saving the
// same value again retries them. Nothing to reconcile is nil.
func (r *Registry) SetGlobalUploadDisabled(ctx context.Context, disabled bool) ([]contracts.TorrentID, error) {
	if !r.order.Holds(ctx, commandorder.Settings) {
		return nil, ErrNotHeld
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var reconcile []contracts.TorrentID
	for id, e := range r.entries {
		changes := Decide(r.inputWith(e, r.global)) != Decide(r.inputWith(e, disabled))
		if changes {
			e.revision++
		}
		if changes || !e.inStep() {
			reconcile = append(reconcile, id)
		}
	}
	r.global = disabled
	slices.Sort(reconcile)
	return reconcile, nil
}

// BeginShutdown stops payload for every torrent from now on, without changing any stored intent. It
// needs no key: it is one-way and never enables anything. Every revision moves, so outcomes of
// work still running are discarded.
func (r *Registry) BeginShutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shuttingDown {
		return
	}
	r.shuttingDown = true
	for _, e := range r.entries {
		e.revision++
	}
}

// Record stores the engine's outcome for revision rev, only if rev is still the torrent's current
// revision. It returns false for a late outcome (a newer revision exists), an unknown or a removed
// torrent: such an outcome is discarded.
func (r *Registry) Record(id contracts.TorrentID, rev PolicyRevision, problem *contracts.AppError) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, found := r.entries[id]
	if !found || e.revision != rev {
		return false
	}
	e.applied, e.problem = rev, clone(problem)
	return true
}

// Lookup returns the torrent's state now.
func (r *Registry) Lookup(id contracts.TorrentID) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.find(id)
	if err != nil {
		return View{}, err
	}
	return r.view(id, e), nil
}

// All returns every torrent's state at one moment, in ID order.
func (r *Registry) All() []View {
	r.mu.Lock()
	defer r.mu.Unlock()
	views := make([]View, 0, len(r.entries))
	for _, id := range slices.Sorted(maps.Keys(r.entries)) {
		views = append(views, r.view(id, r.entries[id]))
	}
	return views
}

// Reconcile is the apply step. Holding the torrent's key, it reads the torrent's state now (never a
// value captured when a command was accepted), asks switches for Decide of it, and records the
// outcome for the revision it read. If that revision moved while the engine worked (a global change,
// or the quit), the outcome is discarded and the newer state is applied, until one is recorded: nil
// means the engine was in step when its outcome was recorded. Only steps without the torrent's key
// move its revision, and each such change is one user or quit action, so the repeats end. It
// returns the engine's problem, a refusal of the registry, or ctx's error once ctx has ended.
func (r *Registry) Reconcile(ctx context.Context, id contracts.TorrentID, switches Switches) error {
	if !r.order.Holds(ctx, commandorder.Torrent(id)) {
		return ErrNotHeld
	}
	for {
		view, err := r.Lookup(id)
		if err != nil {
			return err
		}
		problem := switches.Apply(ctx, id, Decide(view.Input))
		if r.Record(id, view.Revision, problem) {
			if problem != nil {
				return problem
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err // the torrent stays pending; the quit, or the next start, applies it
		}
	}
}

// change applies one mutation of a torrent under its key and bumps the revision when mutate reports
// a change. A torrent being removed accepts only the mutations that allow it (whileRemoving).
func (r *Registry) change(ctx context.Context, id contracts.TorrentID, whileRemoving bool, mutate func(*record) bool) error {
	if !r.order.Holds(ctx, commandorder.Torrent(id)) {
		return ErrNotHeld
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, err := r.find(id)
	if err != nil {
		return err
	}
	if e.phase == PhaseRemoving && !whileRemoving {
		return ErrRemoving
	}
	if mutate(e) {
		e.revision++
	}
	return nil
}

func (r *Registry) find(id contracts.TorrentID) (*record, error) {
	if _, gone := r.removed[id]; gone {
		return nil, ErrRemoved
	}
	e, found := r.entries[id]
	if !found {
		return nil, ErrUnknown
	}
	return e, nil
}

func (r *Registry) inputWith(e *record, globalUploadDisabled bool) Input {
	return Input{ManualPaused: e.manualPaused, GlobalUploadDisabled: globalUploadDisabled, Phase: e.phase, PayloadFault: e.fault, ShuttingDown: r.shuttingDown}
}

func (r *Registry) view(id contracts.TorrentID, e *record) View {
	return View{ID: id, Input: r.inputWith(e, r.global), Revision: e.revision, Applied: e.applied, Problem: clone(e.problem)}
}

// clone copies a problem so that neither the caller nor a reader shares the stored one.
func clone(problem *contracts.AppError) *contracts.AppError {
	if problem == nil {
		return nil
	}
	copied := *problem
	copied.OperationID, copied.TorrentID = clonePointer(problem.OperationID), clonePointer(problem.TorrentID)
	copied.Params = maps.Clone(problem.Params)
	return &copied
}

func clonePointer[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
