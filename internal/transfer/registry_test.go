package transfer

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/commandorder"
)

const (
	idA = contracts.TorrentID("0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d")
	idB = contracts.TorrentID("1b2c3d4e-5f6a-4b7c-9d8e-0f1a2b3c4d5e")
	idC = contracts.TorrentID("2c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f")
	idD = contracts.TorrentID("3d4e5f6a-7b8c-4d9e-8f0a-2b3c4d5e6f7a")
)

// applyGuard bounds a Reconcile that would otherwise repeat for ever; it never decides a result.
const applyGuard = 5 * time.Second

// bounded returns ctx with the apply guard's deadline, ended when the test ends.
func bounded(t *testing.T, ctx context.Context) context.Context {
	ctx, cancel := context.WithTimeout(ctx, applyGuard)
	t.Cleanup(cancel)
	return ctx
}

func newRegistry(globalUploadDisabled bool) *Registry {
	return NewRegistry(commandorder.New(), globalUploadDisabled)
}

// holding returns a context that holds keys of r's order until the test ends, as a granted turn's
// would. The order is shared, so a test holds each key at most once.
func holding(t *testing.T, r *Registry, keys ...commandorder.Key) context.Context {
	t.Helper()
	turn, err := r.order.Join(keys)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	if err := turn.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	t.Cleanup(turn.Leave)
	return turn.Holding(context.Background())
}

func torrentCtx(t *testing.T, r *Registry, id contracts.TorrentID) context.Context {
	return holding(t, r, commandorder.Torrent(id))
}

func lookup(t *testing.T, r *Registry, id contracts.TorrentID) View {
	t.Helper()
	view, err := r.Lookup(id)
	if err != nil {
		t.Fatalf("Lookup(%s): %v", id, err)
	}
	return view
}

// add adds a torrent in its own step, which ends before add returns.
func add(t *testing.T, r *Registry, id contracts.TorrentID, e Entry) {
	t.Helper()
	step := func(ctx context.Context) error { return r.Add(ctx, id, e) }
	if err := r.order.Run(context.Background(), []commandorder.Key{commandorder.Torrent(id)}, step); err != nil {
		t.Fatalf("Add(%s): %v", id, err)
	}
}

func mustNot(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s = %v, want %v", what, err, want)
	}
}

// settle records a clean outcome for every torrent's current revision, as their reconciles would.
func settle(t *testing.T, r *Registry) {
	t.Helper()
	for _, view := range r.All() {
		if !r.Record(view.ID, view.Revision, nil) {
			t.Fatalf("Record(%s, %d) refused", view.ID, view.Revision)
		}
	}
}

func TestANewEntryStartsAtItsFirstRevisionWithTheStoredGlobal(t *testing.T) {
	r := newRegistry(true)
	add(t, r, idA, Entry{ManualPaused: true, Phase: PhaseChecking})
	want := View{ID: idA, Input: Input{ManualPaused: true, GlobalUploadDisabled: true, Phase: PhaseChecking}, Revision: 1}
	if got := lookup(t, r, idA); got != want || !got.Pending() {
		t.Fatalf("Lookup = %+v (pending %v), want %+v, pending", got, got.Pending(), want)
	}
}

// Every change of a torrent happens in its command order (STACK-03): without the torrent's key, with
// another torrent's key, or with a context kept after its turn ended, nothing changes.
func TestEveryMutatorNeedsItsKey(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	other := torrentCtx(t, r, idB)
	turn, _ := r.order.Join([]commandorder.Key{commandorder.Torrent(idA)})
	_ = turn.Wait(context.Background())
	stale := turn.Holding(context.Background())
	turn.Leave()
	foreign := holding(t, newRegistry(false), commandorder.Torrent(idA), commandorder.Torrent(idC)) // another order's turn
	settingsElsewhere := holding(t, newRegistry(false), commandorder.Settings)
	for name, ctx := range map[string]context.Context{
		"no key": context.Background(), "another torrent's key": other, "an ended turn": stale,
		"a turn of another order": foreign, "the settings key of another order": settingsElsewhere,
	} {
		mustNot(t, name+": Add", r.Add(ctx, idC, Entry{Phase: PhaseReady}), ErrNotHeld)
		mustNot(t, name+": CheckIntentChange", r.CheckIntentChange(ctx, idA), ErrNotHeld)
		mustNot(t, name+": SetManualPaused", r.SetManualPaused(ctx, idA, true), ErrNotHeld)
		mustNot(t, name+": SetPhase", r.SetPhase(ctx, idA, PhaseChecking), ErrNotHeld)
		mustNot(t, name+": SetPayloadFault", r.SetPayloadFault(ctx, idA, true), ErrNotHeld)
		mustNot(t, name+": BeginRemoval", r.BeginRemoval(ctx, idA), ErrNotHeld)
		mustNot(t, name+": Forget", r.Forget(ctx, idA), ErrNotHeld)
		mustNot(t, name+": Reconcile", r.Reconcile(ctx, idA, &recordingSwitches{}), ErrNotHeld)
		_, err := r.SetGlobalUploadDisabled(ctx, true)
		mustNot(t, name+": SetGlobalUploadDisabled", err, ErrNotHeld)
	}
	if got := lookup(t, r, idA); got.Revision != 1 || got.Input != (Input{Phase: PhaseReady}) {
		t.Fatalf("a refused call changed the entry: %+v", got)
	}
}

func TestCallsForAnUnknownTorrentAreRefused(t *testing.T) {
	r := newRegistry(false)
	ctx := torrentCtx(t, r, idA)
	mustNot(t, "CheckIntentChange", r.CheckIntentChange(ctx, idA), ErrUnknown)
	mustNot(t, "SetManualPaused", r.SetManualPaused(ctx, idA, true), ErrUnknown)
	mustNot(t, "SetPhase", r.SetPhase(ctx, idA, PhaseChecking), ErrUnknown)
	mustNot(t, "SetPayloadFault", r.SetPayloadFault(ctx, idA, true), ErrUnknown)
	mustNot(t, "BeginRemoval", r.BeginRemoval(ctx, idA), ErrUnknown)
	mustNot(t, "Forget", r.Forget(ctx, idA), ErrUnknown)
	mustNot(t, "Reconcile", r.Reconcile(ctx, idA, &recordingSwitches{}), ErrUnknown)
	_, err := r.Lookup(idA)
	mustNot(t, "Lookup", err, ErrUnknown)
	if r.Record(idA, 1, nil) {
		t.Error("Record stored an outcome for an unknown torrent")
	}
}

func TestAddRefusesAKnownTorrentAndAnInvalidPhase(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	mustNot(t, "second Add", r.Add(torrentCtx(t, r, idA), idA, Entry{Phase: PhaseReady}), ErrExists)
	mustNot(t, "Add with an unknown phase", r.Add(torrentCtx(t, r, idB), idB, Entry{Phase: Phase(99)}), ErrInvalidPhase)
	mustNot(t, "Add without a phase", r.Add(torrentCtx(t, r, idD), idD, Entry{}), ErrInvalidPhase)
	// A removal that was unfinished at a crash is restored as removing (028, 029).
	add(t, r, idC, Entry{Phase: PhaseRemoving})
	if got := lookup(t, r, idC).Input.Phase; got != PhaseRemoving {
		t.Fatalf("restored phase = %d, want removing", got)
	}
}

func TestAChangeBumpsTheRevisionAndARepeatDoesNot(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	ctx := torrentCtx(t, r, idA)
	steps := []struct {
		name string
		call func() error
		want PolicyRevision
	}{
		{"pause", func() error { return r.SetManualPaused(ctx, idA, true) }, 2},
		{"pause again", func() error { return r.SetManualPaused(ctx, idA, true) }, 2},
		{"checking", func() error { return r.SetPhase(ctx, idA, PhaseChecking) }, 3},
		{"checking again", func() error { return r.SetPhase(ctx, idA, PhaseChecking) }, 3},
		{"fault", func() error { return r.SetPayloadFault(ctx, idA, true) }, 4},
		{"fault again", func() error { return r.SetPayloadFault(ctx, idA, true) }, 4},
	}
	for _, step := range steps {
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := lookup(t, r, idA).Revision; got != step.want {
			t.Fatalf("after %s the revision is %d, want %d", step.name, got, step.want)
		}
	}
	mustNot(t, "SetPhase(removing)", r.SetPhase(ctx, idA, PhaseRemoving), ErrInvalidPhase)
	mustNot(t, "SetPhase(unknown)", r.SetPhase(ctx, idA, Phase(99)), ErrInvalidPhase)
	mustNot(t, "SetPhase(none)", r.SetPhase(ctx, idA, Phase(0)), ErrInvalidPhase)
	if got := lookup(t, r, idA).Input; got != (Input{ManualPaused: true, Phase: PhaseChecking, PayloadFault: true}) {
		t.Fatalf("Input = %+v", got)
	}
}

func TestRemovalFreezesTheIntentAndClearsAnOlderFault(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady, PayloadFault: true})
	ctx := torrentCtx(t, r, idA)
	if err := r.BeginRemoval(ctx, idA); err != nil {
		t.Fatalf("BeginRemoval: %v", err)
	}
	view := lookup(t, r, idA)
	if view.Input.Phase != PhaseRemoving || view.Input.PayloadFault || view.Revision != 2 {
		t.Fatalf("after BeginRemoval: %+v", view)
	}
	if err := r.BeginRemoval(ctx, idA); err != nil || lookup(t, r, idA).Revision != 2 {
		t.Fatalf("a repeated BeginRemoval: %v, revision %d", err, lookup(t, r, idA).Revision)
	}
	mustNot(t, "the check before a resume is stored", r.CheckIntentChange(ctx, idA), ErrRemoving)
	mustNot(t, "resume while removing", r.SetManualPaused(ctx, idA, false), ErrRemoving)
	mustNot(t, "pause while removing", r.SetManualPaused(ctx, idA, true), ErrRemoving)
	mustNot(t, "SetPhase while removing", r.SetPhase(ctx, idA, PhaseReady), ErrRemoving)
	// Deleting the files failed (032): the record stays stopped and shows the error.
	if err := r.SetPayloadFault(ctx, idA, true); err != nil {
		t.Fatalf("SetPayloadFault while removing: %v", err)
	}
	in := lookup(t, r, idA).Input
	if Decide(in) != (Permissions{}) || DisplayStatus(in, Progress{WantedFiles: 1}) != contracts.StatusError {
		t.Fatalf("a failed deletion: %+v → %+v, %s", in, Decide(in), DisplayStatus(in, Progress{WantedFiles: 1}))
	}
}

// Once forgotten, a torrent never comes back in this runtime: no call can change or re-add it.
func TestForgetLeavesATombstone(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	add(t, r, idB, Entry{Phase: PhaseReady})
	ctx := torrentCtx(t, r, idA)
	mustNot(t, "Forget before removal", r.Forget(ctx, idA), ErrNotRemoving)
	if err := r.BeginRemoval(ctx, idA); err != nil {
		t.Fatalf("BeginRemoval: %v", err)
	}
	if err := r.Forget(ctx, idA); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	mustNot(t, "Add", r.Add(ctx, idA, Entry{Phase: PhaseReady}), ErrRemoved)
	mustNot(t, "CheckIntentChange", r.CheckIntentChange(ctx, idA), ErrRemoved)
	mustNot(t, "SetManualPaused", r.SetManualPaused(ctx, idA, false), ErrRemoved)
	mustNot(t, "SetPhase", r.SetPhase(ctx, idA, PhaseReady), ErrRemoved)
	mustNot(t, "SetPayloadFault", r.SetPayloadFault(ctx, idA, true), ErrRemoved)
	mustNot(t, "BeginRemoval", r.BeginRemoval(ctx, idA), ErrRemoved)
	mustNot(t, "Forget", r.Forget(ctx, idA), ErrRemoved)
	mustNot(t, "Reconcile", r.Reconcile(ctx, idA, &recordingSwitches{}), ErrRemoved)
	_, err := r.Lookup(idA)
	mustNot(t, "Lookup", err, ErrRemoved)
	if r.Record(idA, 2, nil) {
		t.Error("Record stored an outcome for a removed torrent")
	}
	if views := r.All(); len(views) != 1 || views[0].ID != idB {
		t.Fatalf("All = %+v, want only the other torrent", views)
	}
}

func TestTheGlobalChangeReturnsTheTorrentsWhosePermissionsChange(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idD, Entry{Phase: PhaseReady})                     // ready: upload changes
	add(t, r, idB, Entry{ManualPaused: true, Phase: PhaseReady}) // paused: off either way
	add(t, r, idC, Entry{Phase: PhaseChecking})                  // checking: off either way
	add(t, r, idA, Entry{Phase: PhaseReady})                     // ready: upload changes
	settle(t, r)                                                 // every torrent's first reconcile has run
	settings := holding(t, r, commandorder.Settings)             // the settings key, as the settings operation holds it
	changed, err := r.SetGlobalUploadDisabled(settings, true)
	if err != nil || !slices.Equal(changed, []contracts.TorrentID{idA, idD}) {
		t.Fatalf("SetGlobalUploadDisabled(true) = %v, %v; want [A D]", changed, err)
	}
	for id, want := range map[contracts.TorrentID]PolicyRevision{idA: 2, idB: 1, idC: 1, idD: 2} {
		view := lookup(t, r, id)
		if view.Revision != want || !view.Input.GlobalUploadDisabled {
			t.Errorf("%s: %+v, want revision %d and the stop", id, view, want)
		}
	}
	settle(t, r) // the fan-out applied the change
	if changed, err := r.SetGlobalUploadDisabled(settings, true); err != nil || changed != nil {
		t.Fatalf("the same value again = %v, %v; want nothing to apply", changed, err)
	}
	if changed, _ := r.SetGlobalUploadDisabled(settings, false); !slices.Equal(changed, []contracts.TorrentID{idA, idD}) {
		t.Fatalf("SetGlobalUploadDisabled(false) = %v, want [A D]", changed)
	}
	// Re-enabling upload does not start a paused torrent.
	if got := Decide(lookup(t, r, idB).Input); got != (Permissions{}) {
		t.Fatalf("the paused torrent got %+v", got)
	}
}

func TestShutdownStopsEveryTorrentAndKeepsTheIntent(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	add(t, r, idB, Entry{ManualPaused: true, Phase: PhaseReady})
	r.BeginShutdown()
	r.BeginShutdown()
	for id, paused := range map[contracts.TorrentID]bool{idA: false, idB: true} {
		view := lookup(t, r, id)
		if view.Revision != 2 || !view.Input.ShuttingDown || view.Input.ManualPaused != paused || Decide(view.Input) != (Permissions{}) {
			t.Errorf("%s after shutdown: %+v", id, view)
		}
	}
}

func TestRecordKeepsOnlyTheOutcomeOfTheCurrentRevision(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	if !r.Record(idA, 1, nil) || lookup(t, r, idA).Pending() {
		t.Fatal("the outcome of the current revision was not stored")
	}
	if err := r.SetManualPaused(torrentCtx(t, r, idA), idA, true); err != nil {
		t.Fatalf("SetManualPaused: %v", err)
	}
	if !lookup(t, r, idA).Pending() {
		t.Fatal("a new revision without an outcome is not pending")
	}
	if r.Record(idA, 1, nil) {
		t.Fatal("a late outcome of an older revision was stored")
	}
	problem := contracts.NewAppError("engine.applyFailed", map[string]string{"switch": "upload"}).WithTorrent(idA)
	if !r.Record(idA, 2, problem) {
		t.Fatal("the outcome of the current revision was refused")
	}
	view := lookup(t, r, idA)
	if view.Pending() || view.Applied != 2 || view.Problem.Code != "engine.applyFailed" {
		t.Fatalf("after Record: %+v", view)
	}
	problem.Params["switch"] = "changed by the caller"
	*problem.TorrentID = idB
	view.Problem.Params["switch"] = "changed by a reader"
	*view.Problem.TorrentID = idC
	if got := lookup(t, r, idA).Problem; got.Params["switch"] != "upload" || *got.TorrentID != idA || got.OperationID != nil {
		t.Fatalf("the stored problem is shared: %+v", got)
	}
}

func TestAllReturnsEveryTorrentInIDOrderAtOneMoment(t *testing.T) {
	r := newRegistry(true)
	add(t, r, idB, Entry{ManualPaused: true, Phase: PhaseReady})
	add(t, r, idA, Entry{Phase: PhaseReady})
	views := r.All()
	if len(views) != 2 || views[0].ID != idA || views[1].ID != idB || !views[0].Input.GlobalUploadDisabled || !views[1].Input.ManualPaused {
		t.Fatalf("All = %+v", views)
	}
}

// recordingSwitches is a test double of the engine's switches: it records what it is asked and
// answers problem; during runs inside Apply, to change the registry while the engine works.
type recordingSwitches struct {
	calls   []Permissions
	problem *contracts.AppError
	during  func()
}

func (s *recordingSwitches) Apply(_ context.Context, _ contracts.TorrentID, want Permissions) *contracts.AppError {
	s.calls = append(s.calls, want)
	if s.during != nil {
		s.during()
	}
	return s.problem
}

func TestReconcileAppliesTheCurrentPolicyAndRecordsTheOutcome(t *testing.T) {
	r := newRegistry(true)
	add(t, r, idA, Entry{Phase: PhaseReady})
	ctx := torrentCtx(t, r, idA)
	switches := &recordingSwitches{}
	if err := r.Reconcile(ctx, idA, switches); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !slices.Equal(switches.calls, []Permissions{{Download: true}}) || lookup(t, r, idA).Pending() {
		t.Fatalf("Apply calls %+v, view %+v", switches.calls, lookup(t, r, idA))
	}
	switches.problem = contracts.NewAppError("engine.applyFailed", nil)
	err := r.Reconcile(ctx, idA, switches)
	var problem *contracts.AppError
	if !errors.As(err, &problem) || problem.Code != "engine.applyFailed" || lookup(t, r, idA).Problem.Code != "engine.applyFailed" {
		t.Fatalf("a failed apply: %v, view %+v", err, lookup(t, r, idA))
	}
}

// The revision moves while the engine applies the old state (a global change, or the quit): that
// outcome is discarded and Reconcile applies the newer state, until one is recorded (security review
// L1). A nil result therefore means the engine was in step when the outcome was recorded.
func TestReconcileAppliesAgainUntilTheCurrentRevisionIsRecorded(t *testing.T) {
	t.Run("a global change", func(t *testing.T) {
		r := newRegistry(false)
		add(t, r, idA, Entry{Phase: PhaseReady})
		settings := holding(t, r, commandorder.Settings)
		ctx := bounded(t, torrentCtx(t, r, idA))
		switches := &recordingSwitches{}
		switches.during = func() {
			switches.during = nil // once
			if _, err := r.SetGlobalUploadDisabled(settings, true); err != nil {
				t.Errorf("SetGlobalUploadDisabled: %v", err)
			}
		}
		if err := r.Reconcile(ctx, idA, switches); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		want := []Permissions{{Download: true, Upload: true}, {Download: true}}
		if view := lookup(t, r, idA); view.Pending() || view.Applied != 2 || !slices.Equal(switches.calls, want) {
			t.Fatalf("after the overtaken apply: %+v, calls %+v; want %+v", view, switches.calls, want)
		}
	})
	t.Run("the quit", func(t *testing.T) {
		r := newRegistry(false)
		add(t, r, idA, Entry{Phase: PhaseReady})
		switches := &recordingSwitches{}
		switches.during = func() {
			switches.during = nil
			r.BeginShutdown()
		}
		if err := r.Reconcile(bounded(t, torrentCtx(t, r, idA)), idA, switches); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if view := lookup(t, r, idA); view.Pending() || switches.calls[len(switches.calls)-1] != (Permissions{}) {
			t.Fatalf("after the quit: %+v, calls %+v; want the stop applied", view, switches.calls)
		}
	})
	t.Run("a context that ends meanwhile", func(t *testing.T) {
		r := newRegistry(false)
		add(t, r, idA, Entry{Phase: PhaseReady})
		settings := holding(t, r, commandorder.Settings)
		ctx, cancel := context.WithCancel(torrentCtx(t, r, idA))
		switches := &recordingSwitches{during: func() {
			cancel()
			if _, err := r.SetGlobalUploadDisabled(settings, true); err != nil {
				t.Errorf("SetGlobalUploadDisabled: %v", err)
			}
		}}
		if err := r.Reconcile(ctx, idA, switches); !errors.Is(err, context.Canceled) {
			t.Fatalf("Reconcile = %v, want context.Canceled", err)
		}
		if view := lookup(t, r, idA); !view.Pending() || len(switches.calls) != 1 {
			t.Fatalf("after the ended context: %+v, calls %+v; want pending after one apply", view, switches.calls)
		}
	})
}

// Saving the global value again retries every torrent the engine is not in step with — a failed or
// an unfinished apply — without a new revision (security review M1): an unconfirmed upload stop is
// never left alone because the value did not change.
func TestSavingTheGlobalValueRetriesTheTorrentsNotInStep(t *testing.T) {
	r := newRegistry(false)
	for _, id := range []contracts.TorrentID{idA, idB, idC, idD} {
		add(t, r, id, Entry{Phase: PhaseReady})
	}
	add(t, r, "4e5f6a7b-8c9d-4e0f-9a1b-3c4d5e6f7a8b", Entry{ManualPaused: true, Phase: PhaseReady})
	settle(t, r)
	settings := holding(t, r, commandorder.Settings)
	if _, err := r.SetGlobalUploadDisabled(settings, true); err != nil {
		t.Fatalf("SetGlobalUploadDisabled: %v", err)
	}
	notConfirmed := contracts.NewAppError("engine.uploadStopNotConfirmed", nil)
	r.Record(idA, 2, nil)          // stopped
	r.Record(idB, 2, notConfirmed) // the stop was not confirmed
	r.Record(idD, 2, nil)          // stopped; idC has no outcome yet
	again, err := r.SetGlobalUploadDisabled(settings, true)
	if err != nil || !slices.Equal(again, []contracts.TorrentID{idB, idC}) {
		t.Fatalf("the same value again = %v, %v; want [B C]", again, err)
	}
	if lookup(t, r, idB).Revision != 2 || lookup(t, r, idC).Revision != 2 {
		t.Fatal("a retry made a new revision")
	}
	// A change of the value returns the retries too, also those whose permissions stay the same.
	r.Record(idC, 2, nil)
	pausedFailed := contracts.TorrentID("4e5f6a7b-8c9d-4e0f-9a1b-3c4d5e6f7a8b")
	r.Record(pausedFailed, 1, contracts.NewAppError("engine.applyFailed", nil))
	changed, _ := r.SetGlobalUploadDisabled(settings, false)
	if want := []contracts.TorrentID{idA, idB, idC, idD, pausedFailed}; !slices.Equal(changed, want) {
		t.Fatalf("a changed value = %v, want %v", changed, want)
	}
	if lookup(t, r, pausedFailed).Revision != 1 {
		t.Fatal("the paused torrent got a new revision although its permissions did not change")
	}
}

// CheckIntentChange answers, without changing anything, what SetManualPaused would answer: the
// service asks it before it stores the intent (persist first), so a resume queued behind a removal is
// refused before anything is written (security review M3).
func TestCheckIntentChangeChangesNothing(t *testing.T) {
	r := newRegistry(false)
	add(t, r, idA, Entry{Phase: PhaseReady})
	ctx := torrentCtx(t, r, idA)
	before := lookup(t, r, idA)
	if err := r.CheckIntentChange(ctx, idA); err != nil {
		t.Fatalf("CheckIntentChange: %v", err)
	}
	if after := lookup(t, r, idA); after != before {
		t.Fatalf("the check changed the torrent: %+v, was %+v", after, before)
	}
}
