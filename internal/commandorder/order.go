// Package commandorder runs the work that changes one torrent, or the settings, in the order it was
// accepted (STACK-03). Each piece of work takes a turn in the queue of every key it names; a turn
// runs when it is first in all of them. Joining all queues happens at once, so the order of joins is
// one total order: the oldest unfinished turn is always first in all its queues, and multi-key turns
// cannot deadlock each other.
//
// Keys have levels: torrent keys (0) and the settings key (1). A turn holds keys of one level, and a
// step may run nested steps only for keys of a lower level (a settings step may fan out to torrent
// steps; a torrent step may run none). Together with the total order this keeps every wait finite.
// The ADR transfer-status-model has the argument and the rules for callers.
package commandorder

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"

	"octopustorrent/internal/app/contracts"
)

// Key names what a piece of work changes.
type Key struct {
	level uint8
	name  string
}

const (
	torrentLevel  uint8 = 0
	settingsLevel uint8 = 1
)

// Settings is the key of every change to the stored settings, the global upload policy included.
var Settings = Key{level: settingsLevel, name: "settings"}

// Torrent is the key of every change to one torrent.
func Torrent(id contracts.TorrentID) Key { return Key{level: torrentLevel, name: string(id)} }

// Torrents returns the key of each torrent.
func Torrents(ids []contracts.TorrentID) []Key {
	keys := make([]Key, len(ids))
	for i, id := range ids {
		keys[i] = Torrent(id)
	}
	return keys
}

var (
	// ErrMixedLevels refuses a key set with the settings key and torrent keys: such a turn could wait
	// for a settings step whose nested torrent step waits for it.
	ErrMixedLevels = errors.New("commandorder: keys of two levels in one turn")
	// ErrHeld refuses a nested step that asks for keys at or above the level its caller holds:
	// waiting for them would deadlock.
	ErrHeld = errors.New("commandorder: a step asked for keys at or above the level it holds")
	// ErrLeft refuses Wait on a turn that has left: it is never granted again.
	ErrLeft = errors.New("commandorder: the turn has left")
)

// Order holds one queue per key. The zero value is not usable; call New.
type Order struct {
	mu     sync.Mutex
	queues map[Key][]*Turn
}

// New makes an empty order.
func New() *Order { return &Order{queues: map[Key][]*Turn{}} }

// Turn is one piece of work's place in the queues of its keys.
type Turn struct {
	order *Order
	keys  []Key // sorted, unique, one level

	// Guarded by order.mu.
	left    bool
	ready   bool               // readyCh is closed
	release context.CancelFunc // ends the context Holding made

	readyCh chan struct{} // closed once: when the turn is first in every queue, or when it leaves
	held    atomic.Bool   // between a granted Wait and Leave; read by Holds without the lock
}

// Join takes a place at the end of every key's queue at once and returns the turn. The keys are
// sorted and de-duplicated; a set spanning two levels is ErrMixedLevels and joins nothing. A turn
// without keys is ready at once.
func (o *Order) Join(keys []Key) (*Turn, error) {
	normalized, err := normalize(keys)
	if err != nil {
		return nil, err
	}
	return o.join(normalized), nil
}

func (o *Order) join(keys []Key) *Turn {
	t := &Turn{order: o, keys: keys, readyCh: make(chan struct{})}
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, k := range keys {
		o.queues[k] = append(o.queues[k], t)
	}
	o.signal(t)
	return t
}

// signal closes t's channel when t is first in every queue. It runs under o.mu, and only for a turn
// in the queues that just joined or just became first in one queue: such a turn was not first
// everywhere before and has not left, so its channel is still open and is closed exactly once.
func (o *Order) signal(t *Turn) {
	for _, k := range t.keys {
		if o.queues[k][0] != t {
			return
		}
	}
	t.ready = true
	close(t.readyCh)
}

// Wait blocks until every earlier turn sharing a key has left, then grants the turn. If ctx ends
// first — or has ended by the time the turn is first — the turn leaves its queues and Wait returns
// ctx's error: a turn whose context ended is never granted. A turn that has left is not granted
// either: Wait returns ErrLeft.
func (t *Turn) Wait(ctx context.Context) error {
	select {
	case <-t.readyCh:
	case <-ctx.Done():
	}
	if err := ctx.Err(); err != nil {
		t.Leave()
		return err
	}
	o := t.order
	o.mu.Lock()
	defer o.mu.Unlock()
	if t.left {
		return ErrLeft
	}
	t.held.Store(true)
	return nil
}

// Leave ends the turn: it gives up its place in every queue, lets the next turn of each queue it
// was first in start, and cancels the context Holding made. Calling it again does nothing.
func (t *Turn) Leave() {
	o := t.order
	o.mu.Lock()
	if t.left {
		o.mu.Unlock()
		return
	}
	t.left = true
	t.held.Store(false)
	for _, k := range t.keys {
		queue := o.queues[k]
		i := slices.Index(queue, t)
		queue = slices.Delete(queue, i, i+1)
		if len(queue) == 0 {
			delete(o.queues, k)
			continue
		}
		o.queues[k] = queue
		if i == 0 {
			o.signal(queue[0])
		}
	}
	if !t.ready { // wake a Wait still blocked on the turn: it returns ErrLeft
		t.ready = true
		close(t.readyCh)
	}
	release := t.release
	o.mu.Unlock()
	if release != nil {
		release()
	}
}

type heldTurns struct{}

// Holding returns a context for the work of a granted turn: it carries the turn, so Holds and the
// nesting check of Run see its keys, and it is cancelled when the turn leaves, so a goroutine that
// kept it neither holds the keys nor keeps running on them. After Leave it is cancelled at once.
func (t *Turn) Holding(ctx context.Context) context.Context {
	child, cancel := context.WithCancel(ctx)
	o := t.order
	o.mu.Lock()
	if t.left {
		o.mu.Unlock()
		cancel()
	} else {
		earlier := t.release
		t.release = func() {
			if earlier != nil {
				earlier()
			}
			cancel()
		}
		o.mu.Unlock()
	}
	return context.WithValue(child, heldTurns{}, append(heldIn(ctx), t))
}

func heldIn(ctx context.Context) []*Turn {
	turns, _ := ctx.Value(heldTurns{}).([]*Turn)
	return slices.Clone(turns)
}

// Holds reports whether ctx belongs to a turn of this order that holds key now. A turn of another
// order does not count: its queues are not this order's, so it proves nothing about this order.
func (o *Order) Holds(ctx context.Context, key Key) bool {
	for _, t := range heldIn(ctx) {
		if t.order == o && t.held.Load() && slices.Contains(t.keys, key) {
			return true
		}
	}
	return false
}

// Run runs step once its turn for keys is granted, with a context from Holding, and returns the
// step's error. Called from inside a step, it may ask only for keys of a lower level than every key
// that step's ctx holds (ErrHeld otherwise). If ctx ends before the turn is granted, the step does
// not run and Run returns ctx's error. The turn is left when the step returns or panics.
func (o *Order) Run(ctx context.Context, keys []Key, step func(context.Context) error) error {
	normalized, err := normalize(keys)
	if err != nil {
		return err
	}
	if err := o.checkNesting(ctx, normalized); err != nil {
		return err
	}
	t := o.join(normalized)
	defer t.Leave()
	if err := t.Wait(ctx); err != nil {
		return err
	}
	return step(t.Holding(ctx))
}

func (o *Order) checkNesting(ctx context.Context, keys []Key) error {
	if len(keys) == 0 {
		return nil
	}
	for _, t := range heldIn(ctx) {
		if t.order == o && t.held.Load() && len(t.keys) > 0 && t.keys[0].level <= keys[0].level {
			return ErrHeld
		}
	}
	return nil
}

func normalize(keys []Key) ([]Key, error) {
	sorted := slices.Clone(keys)
	slices.SortFunc(sorted, func(a, b Key) int {
		return cmp.Or(cmp.Compare(a.level, b.level), cmp.Compare(a.name, b.name))
	})
	sorted = slices.Compact(sorted)
	if len(sorted) > 0 && sorted[0].level != sorted[len(sorted)-1].level {
		return nil, ErrMixedLevels
	}
	return sorted, nil
}
