package commandorder

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
)

const (
	idA = contracts.TorrentID("0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d")
	idB = contracts.TorrentID("1b2c3d4e-5f6a-4b7c-9d8e-0f1a2b3c4d5e")
	idC = contracts.TorrentID("2c3d4e5f-6a7b-4c8d-ae9f-1a2b3c4d5e6f")
)

// hangGuard bounds a test that would otherwise hang on a deadlock; it never decides a result.
const hangGuard = 10 * time.Second

func join(t *testing.T, o *Order, keys ...Key) *Turn {
	t.Helper()
	turn, err := o.Join(keys)
	if err != nil {
		t.Fatalf("Join(%v): %v", keys, err)
	}
	return turn
}

// isReady reports, without waiting, whether the turn is first in every one of its queues.
func isReady(turn *Turn) bool {
	select {
	case <-turn.readyCh:
		return true
	default:
		return false
	}
}

func mustWait(t *testing.T, turn *Turn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	if err := turn.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestTurnsOfOneKeyRunInJoinOrder(t *testing.T) {
	o := New()
	first := join(t, o, Torrent(idA))
	second := join(t, o, Torrent(idA))
	if !isReady(first) || isReady(second) {
		t.Fatalf("ready: first %v, second %v; want only the first", isReady(first), isReady(second))
	}
	mustWait(t, first)

	var mu sync.Mutex
	var log []string
	note := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		log = append(log, s)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		mustWait(t, second)
		note("second runs")
		second.Leave()
	}()
	note("first ends")
	first.Leave()
	<-done
	if want := []string{"first ends", "second runs"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
}

func TestTurnsOfDisjointKeysDoNotWaitForEachOther(t *testing.T) {
	o := New()
	a, b := join(t, o, Torrent(idA)), join(t, o, Torrent(idB))
	if !isReady(a) || !isReady(b) {
		t.Fatal("a turn waits for a turn with other keys")
	}
}

func TestAMultiKeyTurnWaitsForEveryKey(t *testing.T) {
	o := New()
	a, b := join(t, o, Torrent(idA)), join(t, o, Torrent(idB))
	both := join(t, o, Torrent(idB), Torrent(idA))
	a.Leave()
	if isReady(both) {
		t.Fatal("ready while the second key is still held")
	}
	b.Leave()
	if !isReady(both) {
		t.Fatal("not ready after every earlier holder left")
	}
}

func TestATurnWithoutKeysIsReadyAtOnce(t *testing.T) {
	o := New()
	join(t, o, Torrent(idA))
	if !isReady(join(t, o)) {
		t.Fatal("a turn without keys waits")
	}
}

func TestKeysAreSortedAndDeduplicated(t *testing.T) {
	o := New()
	turn := join(t, o, Torrent(idB), Torrent(idA), Torrent(idB))
	if want := []Key{Torrent(idA), Torrent(idB)}; !slices.Equal(turn.keys, want) {
		t.Fatalf("keys = %v, want %v", turn.keys, want)
	}
	if !isReady(turn) {
		t.Fatal("a repeated key made the turn wait for itself")
	}
}

func TestTorrentsMakesOneKeyPerTorrent(t *testing.T) {
	if got, want := Torrents([]contracts.TorrentID{idA, idB}), []Key{Torrent(idA), Torrent(idB)}; !slices.Equal(got, want) {
		t.Fatalf("Torrents = %v, want %v", got, want)
	}
	if Torrent(idA) == Torrent(idB) || Torrent(idA) == Settings {
		t.Fatal("different keys compare equal")
	}
}

// A turn holds keys of one level only: a set with the settings key and a torrent key could wait for
// a settings step whose torrent step waits for it.
func TestAKeySetOfTwoLevelsIsRefused(t *testing.T) {
	o := New()
	if _, err := o.Join([]Key{Settings, Torrent(idA)}); !errors.Is(err, ErrMixedLevels) {
		t.Fatalf("Join = %v, want ErrMixedLevels", err)
	}
	if !isReady(join(t, o, Torrent(idA))) || !isReady(join(t, o, Settings)) {
		t.Fatal("a refused set left a turn in a queue")
	}
}

// Cancelling a turn that is not first in its queue must not signal the first one again (a second
// close of its channel would panic) and must not let the next one jump ahead.
func TestACancelledTurnBehindTheHeadLeavesTheOrderIntact(t *testing.T) {
	o := New()
	head := join(t, o, Torrent(idA))
	cancelled := join(t, o, Torrent(idA))
	last := join(t, o, Torrent(idA))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cancelled.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if !isReady(head) || isReady(last) {
		t.Fatalf("ready: head %v, last %v; want only the head", isReady(head), isReady(last))
	}
	head.Leave()
	if !isReady(last) {
		t.Fatal("the last turn did not get its turn")
	}
}

// A cancelled turn can be first in one queue and waiting in another; leaving releases the first.
func TestACancelledTurnReleasesTheQueueItWasFirstIn(t *testing.T) {
	o := New()
	holder := join(t, o, Torrent(idA))
	both := join(t, o, Torrent(idA), Torrent(idB)) // waits in A, first in B
	next := join(t, o, Torrent(idB))
	if isReady(next) {
		t.Fatal("next is ready while the multi-key turn is first in B")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := both.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if !isReady(next) {
		t.Fatal("B is still blocked by a cancelled turn")
	}
	holder.Leave()
}

func TestLeaveIsIdempotent(t *testing.T) {
	o := New()
	first := join(t, o, Torrent(idA))
	second := join(t, o, Torrent(idA))
	third := join(t, o, Torrent(idA))
	first.Leave()
	first.Leave()
	if !isReady(second) || isReady(third) {
		t.Fatalf("ready: second %v, third %v; a repeated Leave released a turn twice", isReady(second), isReady(third))
	}
}

// Wait never grants a turn whose context ended, even when the turn is already first.
func TestWaitWithAnEndedContextLeavesTheTurn(t *testing.T) {
	o := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	turn := join(t, o, Torrent(idA))
	if err := turn.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if !isReady(join(t, o, Torrent(idA))) {
		t.Fatal("the refused turn still holds its key")
	}
}

// Many goroutines with crossing key sets: no deadlock, and never two holders of one key at once.
func TestCrossingKeySetsNeverDeadlockAndNeverShareAKey(t *testing.T) {
	o := New()
	sets := [][]Key{
		{Torrent(idA), Torrent(idB)}, {Torrent(idB), Torrent(idA)}, {Torrent(idA)},
		{Torrent(idB), Torrent(idC)}, {Torrent(idC), Torrent(idA), Torrent(idB)}, {Torrent(idC)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	var mu sync.Mutex
	inUse := map[Key]bool{}
	var group sync.WaitGroup
	for i := range 300 {
		keys := sets[i%len(sets)]
		group.Go(func() {
			err := o.Run(ctx, keys, func(context.Context) error {
				mu.Lock()
				for _, k := range keys {
					if inUse[k] {
						t.Errorf("two holders of %v", k)
					}
					inUse[k] = true
				}
				mu.Unlock()
				mu.Lock()
				for _, k := range keys {
					inUse[k] = false
				}
				mu.Unlock()
				return nil
			})
			if err != nil {
				t.Errorf("Run(%v): %v", keys, err)
			}
		})
	}
	group.Wait()
	if len(o.queues) != 0 {
		t.Fatalf("queues left behind: %v", o.queues)
	}
}

func TestRunHoldsItsKeysOnlyWhileTheStepRuns(t *testing.T) {
	o := New()
	failure := errors.New("step failed")
	var kept context.Context
	err := o.Run(context.Background(), []Key{Torrent(idA)}, func(ctx context.Context) error {
		kept = ctx
		if !o.Holds(ctx, Torrent(idA)) || o.Holds(ctx, Torrent(idB)) || o.Holds(ctx, Settings) {
			t.Error("the step holds the wrong keys")
		}
		return failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("Run = %v, want the step's error", err)
	}
	if o.Holds(kept, Torrent(idA)) {
		t.Fatal("a context kept after the step still holds its key")
	}
	if kept.Err() == nil {
		t.Fatal("the step's context was not cancelled when the turn ended")
	}
	if o.Holds(context.Background(), Torrent(idA)) {
		t.Fatal("a plain context holds a key")
	}
}

// A settings step may run torrent steps; a torrent step may run none, and a settings step may not
// ask for the settings key again: each would wait for a turn that waits for it.
func TestRunNestsOnlyDownward(t *testing.T) {
	o := New()
	err := o.Run(context.Background(), []Key{Settings}, func(settings context.Context) error {
		if err := o.Run(settings, []Key{Settings}, nil); !errors.Is(err, ErrHeld) {
			t.Errorf("settings in settings = %v, want ErrHeld", err)
		}
		return o.Run(settings, []Key{Torrent(idA)}, func(torrent context.Context) error {
			if !o.Holds(torrent, Settings) || !o.Holds(torrent, Torrent(idA)) {
				t.Error("a nested step does not hold both keys")
			}
			for _, keys := range [][]Key{{Torrent(idB)}, {Settings}} {
				if err := o.Run(torrent, keys, nil); !errors.Is(err, ErrHeld) {
					t.Errorf("Run(%v) in a torrent step = %v, want ErrHeld", keys, err)
				}
			}
			ran := false
			if err := o.Run(torrent, nil, func(context.Context) error { ran = true; return nil }); err != nil || !ran {
				t.Errorf("a keyless step in a torrent step: %v, ran %v", err, ran)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunWithAnEndedContextNeverRunsTheStep(t *testing.T) {
	o := New()
	holder := join(t, o, Torrent(idA))
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error)
	go func() {
		waiting <- o.Run(ctx, []Key{Torrent(idA)}, func(context.Context) error {
			t.Error("a step ran after its context ended")
			return nil
		})
	}()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if !isReady(holder) {
		t.Fatal("the holder lost its turn")
	}
	holder.Leave()
	if !isReady(join(t, o, Torrent(idA))) {
		t.Fatal("the cancelled step still holds its key")
	}
}

func TestRunRefusesKeysOfTwoLevels(t *testing.T) {
	if err := New().Run(context.Background(), []Key{Torrent(idA), Settings}, nil); !errors.Is(err, ErrMixedLevels) {
		t.Fatalf("Run = %v, want ErrMixedLevels", err)
	}
}

// A turn of another order proves nothing about this one: Holds ignores it, and so does the nesting
// check (the two orders share no queue, so a step of one may run a step of the other).
func TestATurnOfAnotherOrderDoesNotCount(t *testing.T) {
	mine, other := New(), New()
	err := other.Run(context.Background(), []Key{Torrent(idA)}, func(ctx context.Context) error {
		if mine.Holds(ctx, Torrent(idA)) || !other.Holds(ctx, Torrent(idA)) {
			t.Error("Holds counted a turn of another order")
		}
		return mine.Run(ctx, []Key{Torrent(idA)}, func(inner context.Context) error {
			if !mine.Holds(inner, Torrent(idA)) {
				t.Error("the nested step of the other order does not hold its key")
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestEveryHoldingContextEndsWhenTheTurnLeaves(t *testing.T) {
	o := New()
	turn := join(t, o, Torrent(idA))
	mustWait(t, turn)
	first, second := turn.Holding(context.Background()), turn.Holding(context.Background())
	turn.Leave()
	if first.Err() == nil || second.Err() == nil {
		t.Fatal("a holding context outlived its turn")
	}
	if late := turn.Holding(context.Background()); late.Err() == nil || o.Holds(late, Torrent(idA)) {
		t.Fatal("a holding context made after Leave is still live")
	}
}

// A turn that has left is never granted again (security review L4): a later Wait refuses it, so a
// context kept from its step does not hold the key while the next turn does.
func TestATurnThatLeftIsNeverGrantedAgain(t *testing.T) {
	t.Run("after it ran", func(t *testing.T) {
		o := New()
		turn := join(t, o, Torrent(idA))
		mustWait(t, turn)
		kept := turn.Holding(context.Background())
		turn.Leave()
		next := join(t, o, Torrent(idA))
		mustWait(t, next)
		defer next.Leave()
		if err := turn.Wait(context.Background()); !errors.Is(err, ErrLeft) {
			t.Fatalf("Wait after Leave = %v, want ErrLeft", err)
		}
		if o.Holds(kept, Torrent(idA)) {
			t.Fatal("a context of the left turn holds the key while the next turn runs")
		}
	})
	t.Run("while it was queued", func(t *testing.T) {
		o := New()
		head := join(t, o, Torrent(idA))
		defer head.Leave()
		queued := join(t, o, Torrent(idA))
		queued.Leave()
		ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
		defer cancel()
		if err := queued.Wait(ctx); !errors.Is(err, ErrLeft) {
			t.Fatalf("Wait of a queued turn that left = %v, want ErrLeft at once", err)
		}
	})
}
