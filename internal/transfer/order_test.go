package transfer_test

// These scenarios compose the real pieces the later tasks build on — the operation ledger, the
// command order and the registry — the way the ADR transfer-status-model prescribes. Only the engine
// is a test double (engineDouble): it records the permissions it is asked for.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
	"octopustorrent/internal/commandorder"
	"octopustorrent/internal/transfer"
)

const (
	runtimeID = contracts.RuntimeID("8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e")
	idA       = contracts.TorrentID("0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d")
	idB       = contracts.TorrentID("1b2c3d4e-5f6a-4b7c-9d8e-0f1a2b3c4d5e")
	idC       = contracts.TorrentID("2c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f")
	idD       = contracts.TorrentID("3d4e5f6a-7b8c-4d9e-8f0a-2b3c4d5e6f7a")
)

// hangGuard bounds waits that would otherwise hang; it never decides a result.
const hangGuard = 10 * time.Second

// engineDouble is a test double of the engine's switches. It keeps the switch state each torrent
// was last given and the calls in order, and fails the test if it is ever asked to transfer for a
// torrent that is paused at that moment: Apply runs under the torrent's key, and the pause changes
// only under the same key, so the check is exact.
type engineDouble struct {
	t        *testing.T
	registry *transfer.Registry
	mu       sync.Mutex
	actual   map[contracts.TorrentID]transfer.Permissions
	calls    map[contracts.TorrentID][]transfer.Permissions
}

func newEngineDouble(t *testing.T, r *transfer.Registry) *engineDouble {
	return &engineDouble{t: t, registry: r, actual: map[contracts.TorrentID]transfer.Permissions{}, calls: map[contracts.TorrentID][]transfer.Permissions{}}
}

func (e *engineDouble) Apply(_ context.Context, id contracts.TorrentID, want transfer.Permissions) *contracts.AppError {
	if view, err := e.registry.Lookup(id); err == nil && view.Input.ManualPaused && want != (transfer.Permissions{}) {
		e.t.Errorf("asked to transfer %+v for %s while it is paused", want, id)
	}
	runtime.Gosched() // let other steps interleave with the engine's work
	e.mu.Lock()
	defer e.mu.Unlock()
	e.actual[id] = want
	e.calls[id] = append(e.calls[id], want)
	return nil
}

func (e *engineDouble) state(id contracts.TorrentID) (transfer.Permissions, []transfer.Permissions) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.actual[id], append([]transfer.Permissions(nil), e.calls[id]...)
}

// storeDouble stands for the stored intent (015, 025): the manual pause as SQLite would hold it.
type storeDouble struct {
	mu     sync.Mutex
	paused map[contracts.TorrentID]bool
}

func (d *storeDouble) save(id contracts.TorrentID, paused bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paused[id] = paused
}

func (d *storeDouble) stored(id contracts.TorrentID) (paused, found bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	paused, found = d.paused[id]
	return paused, found
}

// service is the composition the later tasks implement (025 pause/resume, 053 the global stop, 023
// the import, 031 the removal), reduced to the policy: the real engine and SQLite are out.
type service struct {
	t        *testing.T
	order    *commandorder.Order
	ledger   *operations.Ledger
	registry *transfer.Registry
	engine   *engineDouble
	store    *storeDouble
	ops      []contracts.OperationID
}

func newService(t *testing.T, globalUploadDisabled bool) *service {
	order := commandorder.New() // the runtime's one order: the ledger's and the registry's
	registry := transfer.NewRegistry(order, globalUploadDisabled)
	s := &service{
		t: t, order: order, ledger: operations.NewLedger(runtimeID, order), registry: registry,
		engine: newEngineDouble(t, registry), store: &storeDouble{paused: map[contracts.TorrentID]bool{}},
	}
	t.Cleanup(func() { _ = s.ledger.Shutdown(context.Background()) })
	return s
}

func refused(err error) *contracts.AppError {
	if err == nil {
		return nil
	}
	return contracts.NewAppError("test.refused", map[string]string{"cause": err.Error()})
}

// add enters a torrent as the import commit (023) does: in its own step, then its first reconcile.
func (s *service) add(id contracts.TorrentID, e transfer.Entry) error {
	return s.order.Run(context.Background(), []commandorder.Key{commandorder.Torrent(id)}, func(ctx context.Context) error {
		if err := s.registry.Add(ctx, id, e); err != nil {
			return err
		}
		return s.registry.Reconcile(ctx, id, s.engine)
	})
}

func (s *service) start(kind contracts.OperationKind, keys []commandorder.Key, mutation operations.Mutation) contracts.OperationID {
	id := contracts.OperationID(fmt.Sprintf("00000000-0000-4000-8000-%012d", len(s.ops)+1))
	if _, problem := s.ledger.Start(id, kind, string(id), keys, mutation); problem != nil {
		s.t.Fatalf("Start: %+v", problem)
	}
	s.ops = append(s.ops, id)
	return id
}

// setPaused is a pause or a resume of the given torrents. Every torrent is handled: a refusal of one
// (unknown, removed, being removed) is reported for that torrent, in the params under its ID, and
// never stops the others.
func (s *service) setPaused(paused bool, ids ...contracts.TorrentID) contracts.OperationID {
	kind := contracts.OperationResumeTorrents
	if paused {
		kind = contracts.OperationPauseTorrents
	}
	return s.start(kind, commandorder.Torrents(ids), func(ctx context.Context) *contracts.AppError {
		refusals := map[string]string{}
		for _, id := range ids {
			if err := s.setPausedOne(ctx, id, paused); err != nil {
				refusals[string(id)] = err.Error()
			}
		}
		if len(refusals) > 0 {
			return contracts.NewAppError("test.refused", refusals)
		}
		return nil
	})
}

// setPausedOne checks the change, stores it (persist first), then records and applies it: a change
// the registry would refuse is never written.
func (s *service) setPausedOne(ctx context.Context, id contracts.TorrentID, paused bool) error {
	if err := s.registry.CheckIntentChange(ctx, id); err != nil {
		return err
	}
	s.store.save(id, paused)
	if err := s.registry.SetManualPaused(ctx, id, paused); err != nil {
		return err
	}
	return s.registry.Reconcile(ctx, id, s.engine)
}

// setGlobal is the settings operation of a new global revision: it records the value, then
// reconciles every torrent whose permissions change, each in its own order, in parallel.
func (s *service) setGlobal(disabled bool, between func()) contracts.OperationID {
	return s.start(contracts.OperationUpdateSettings, []commandorder.Key{commandorder.Settings}, func(ctx context.Context) *contracts.AppError {
		changed, err := s.registry.SetGlobalUploadDisabled(ctx, disabled)
		if err != nil {
			return refused(err)
		}
		if between != nil {
			between()
		}
		var group sync.WaitGroup
		failures := make(chan error, len(changed))
		for _, id := range changed {
			group.Go(func() {
				failures <- s.order.Run(ctx, []commandorder.Key{commandorder.Torrent(id)}, func(step context.Context) error {
					return s.registry.Reconcile(step, id, s.engine)
				})
			})
		}
		group.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				return refused(err)
			}
		}
		return nil
	})
}

// finish waits until every accepted operation has its final state and returns those states.
func (s *service) finish() map[contracts.OperationID]contracts.OperationStatus {
	s.t.Helper()
	states := map[contracts.OperationID]contracts.OperationStatus{}
	deadline := time.Now().Add(hangGuard)
	for _, id := range s.ops {
		for {
			status, problem := s.ledger.Get(id)
			if problem != nil {
				s.t.Fatalf("Get(%s): %+v", id, problem)
			}
			if status.State.Finished() {
				states[id] = status
				break
			}
			if time.Now().After(deadline) {
				s.t.Fatalf("operation %s is still %s", id, status.State)
			}
			time.Sleep(time.Millisecond)
		}
	}
	return states
}

func (s *service) mustAdd(id contracts.TorrentID, e transfer.Entry) {
	s.t.Helper()
	if err := s.add(id, e); err != nil {
		s.t.Fatalf("add %s: %v", id, err)
	}
}

func (s *service) expectApplied(id contracts.TorrentID, want transfer.Permissions) {
	s.t.Helper()
	view, err := s.registry.Lookup(id)
	if err != nil {
		s.t.Fatalf("Lookup(%s): %v", id, err)
	}
	if got, _ := s.engine.state(id); got != want || got != transfer.Decide(view.Input) || view.Pending() {
		s.t.Fatalf("%s: the engine has %+v, the registry decides %+v (pending %v), want %+v", id, got, transfer.Decide(view.Input), view.Pending(), want)
	}
}

// Commands accepted in a random order (the seed is logged to reproduce a failure) run with every
// interleaving the scheduler finds. Whatever the interleaving, each torrent ends with the latest
// accepted pause or resume and the latest accepted global value, applied and not pending, and the
// engine is never asked to transfer for a paused torrent.
func TestConcurrentCommandsEndWithTheLatestAcceptedIntentAndGlobalValue(t *testing.T) {
	for round := range 5 {
		seed := uint64(time.Now().UnixNano()) + uint64(round)
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, seed))
			s := newService(t, false)
			ids := []contracts.TorrentID{idA, idB, idC}
			paused := map[contracts.TorrentID]bool{}
			for _, id := range ids {
				s.mustAdd(id, transfer.Entry{Phase: transfer.PhaseReady})
			}
			global := false
			for range 120 {
				switch n := rng.IntN(10); {
				case n < 2:
					global = rng.IntN(2) == 0
					s.setGlobal(global, nil)
				case n < 4:
					pause := rng.IntN(2) == 0
					s.setPaused(pause, idA, idB) // a multi-torrent command
					paused[idA], paused[idB] = pause, pause
				default:
					id, pause := ids[rng.IntN(len(ids))], rng.IntN(2) == 0
					s.setPaused(pause, id)
					paused[id] = pause
				}
			}
			for id, status := range s.finish() {
				if status.State != contracts.OperationSucceeded {
					t.Fatalf("operation %s: %+v", id, status)
				}
			}
			for _, id := range ids {
				want := transfer.Decide(transfer.Input{ManualPaused: paused[id], GlobalUploadDisabled: global, Phase: transfer.PhaseReady})
				s.expectApplied(id, want)
				if stored, found := s.store.stored(id); found && stored != paused[id] {
					t.Fatalf("%s: the store has paused=%v, the latest accepted command says %v", id, stored, paused[id])
				}
			}
		})
	}
}

// A torrent added while the global value changes ends with the new value either way: added before
// the change, the fan-out reconciles it; added after the change but before the fan-out, its own
// first reconcile reads the new value.
func TestATorrentAddedAroundAGlobalChangeGetsTheNewValue(t *testing.T) {
	t.Run("added before the change", func(t *testing.T) {
		s := newService(t, false)
		s.mustAdd(idD, transfer.Entry{Phase: transfer.PhaseReady})
		s.setGlobal(true, nil)
		s.finish()
		s.expectApplied(idD, transfer.Permissions{Download: true})
	})
	t.Run("added between the change and the fan-out", func(t *testing.T) {
		s := newService(t, false)
		s.mustAdd(idA, transfer.Entry{Phase: transfer.PhaseReady})
		globalSet, added := make(chan struct{}), make(chan struct{})
		s.setGlobal(true, func() {
			close(globalSet)
			<-added
		})
		select {
		case <-globalSet:
		case <-time.After(hangGuard):
			t.Fatalf("the settings operation never recorded the value: %+v", s.finish())
		}
		s.mustAdd(idD, transfer.Entry{Phase: transfer.PhaseReady})
		close(added)
		s.finish()
		s.expectApplied(idD, transfer.Permissions{Download: true})
		s.expectApplied(idA, transfer.Permissions{Download: true})
	})
}

// Resume, then remove, then resume again. The first resume's answer arrives late: it is recorded
// with that resume's revision inside the removal step, after the removal's stop, and again after the
// removal finished, and both times it is discarded. The second resume is refused; after the
// removal's own stop the engine is asked nothing; the torrent never comes back.
func TestALateAnswerAfterRemovalIsDiscardedAndTheTorrentNeverComesBack(t *testing.T) {
	s := newService(t, false)
	s.mustAdd(idA, transfer.Entry{ManualPaused: true, Phase: transfer.PhaseReady})
	var resumedAt transfer.PolicyRevision
	s.start(contracts.OperationResumeTorrents, commandorder.Torrents([]contracts.TorrentID{idA}), func(ctx context.Context) *contracts.AppError {
		if err := s.registry.SetManualPaused(ctx, idA, false); err != nil {
			return refused(err)
		}
		view, _ := s.registry.Lookup(idA)
		resumedAt = view.Revision // its confirmation arrives later, outside the step
		return refused(s.registry.Reconcile(ctx, idA, s.engine))
	})
	lateDuringRemoval := make(chan bool, 1)
	callsAtStop := 0
	s.start(contracts.OperationRemoveTorrents, commandorder.Torrents([]contracts.TorrentID{idA}), func(ctx context.Context) *contracts.AppError {
		if err := s.registry.BeginRemoval(ctx, idA); err != nil {
			return refused(err)
		}
		if err := s.registry.Reconcile(ctx, idA, s.engine); err != nil {
			return refused(err)
		}
		_, calls := s.engine.state(idA)
		callsAtStop = len(calls)
		lateDuringRemoval <- s.registry.Record(idA, resumedAt, nil)
		return refused(s.registry.Forget(ctx, idA))
	})
	second := s.setPaused(false, idA)
	states := s.finish()

	select {
	case recorded := <-lateDuringRemoval:
		if recorded {
			t.Error("a late answer was recorded after the removal began")
		}
	default:
		t.Fatalf("the removal never reached its stop: %+v", states)
	}
	if s.registry.Record(idA, resumedAt, nil) {
		t.Error("a late answer was recorded after the removal finished")
	}
	if got := states[second]; got.State != contracts.OperationFailed || got.Error.Params[string(idA)] != transfer.ErrRemoved.Error() {
		t.Fatalf("the resume after the removal = %+v, want it refused as removed", got)
	}
	if actual, calls := s.engine.state(idA); actual != (transfer.Permissions{}) || len(calls) != callsAtStop {
		t.Fatalf("after the removal the engine has %+v and was called %d times, %d at the stop", actual, len(calls), callsAtStop)
	}
	if _, err := s.registry.Lookup(idA); !errors.Is(err, transfer.ErrRemoved) {
		t.Fatalf("Lookup = %v, want ErrRemoved", err)
	}
	if err := s.add(idA, transfer.Entry{Phase: transfer.PhaseReady}); !errors.Is(err, transfer.ErrRemoved) {
		t.Fatalf("a late import of the removed torrent = %v, want ErrRemoved", err)
	}
	if views := s.registry.All(); len(views) != 0 {
		t.Fatalf("All = %+v, want no torrent", views)
	}
}

// A pause of several torrents pauses every one it can: an ID that is stale or unknown — first in the
// list, where a loop that stops at the first refusal would skip the rest — is refused alone
// (security review M2).
func TestABulkPauseHandlesEveryTorrentAndRefusesOnlyTheBadOne(t *testing.T) {
	s := newService(t, false)
	s.mustAdd(idB, transfer.Entry{Phase: transfer.PhaseReady})
	s.mustAdd(idC, transfer.Entry{Phase: transfer.PhaseReady})
	op := s.setPaused(true, idA, idB, idC) // idA was never added
	got := s.finish()[op]
	if got.State != contracts.OperationFailed || len(got.Error.Params) != 1 || got.Error.Params[string(idA)] != transfer.ErrUnknown.Error() {
		t.Fatalf("the bulk pause = %+v, want only the unknown torrent refused", got)
	}
	s.expectApplied(idB, transfer.Permissions{})
	s.expectApplied(idC, transfer.Permissions{})
}

// A resume queued behind a removal whose deletion failed (the record stays removing, 032) is refused
// before it is stored (security review M3): the stored intent never says "resumed" for a torrent the
// runtime keeps stopped, so the next start cannot bring it back running.
func TestAResumeOfATorrentBeingRemovedIsRefusedBeforeItIsStored(t *testing.T) {
	s := newService(t, false)
	s.mustAdd(idA, transfer.Entry{ManualPaused: true, Phase: transfer.PhaseReady})
	s.store.save(idA, true) // the import stored the pause
	s.start(contracts.OperationRemoveTorrents, commandorder.Torrents([]contracts.TorrentID{idA}), func(ctx context.Context) *contracts.AppError {
		if err := s.registry.BeginRemoval(ctx, idA); err != nil {
			return refused(err)
		}
		if err := s.registry.Reconcile(ctx, idA, s.engine); err != nil {
			return refused(err)
		}
		if err := s.registry.SetPayloadFault(ctx, idA, true); err != nil { // moving the files to the Trash failed
			return refused(err)
		}
		return refused(s.registry.Reconcile(ctx, idA, s.engine))
	})
	resume := s.setPaused(false, idA)
	got := s.finish()[resume]
	if got.State != contracts.OperationFailed || got.Error.Params[string(idA)] != transfer.ErrRemoving.Error() {
		t.Fatalf("the resume = %+v, want it refused as removing", got)
	}
	if paused, _ := s.store.stored(idA); !paused {
		t.Fatal("the refused resume was stored")
	}
	s.expectApplied(idA, transfer.Permissions{})
}
