package filestore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/library"
)

// CompletionFile is the name of the marks database inside the data folder.
const CompletionFile = "piece-completion.sqlite"

// ErrClosed is returned by a call that reaches the completion after Close. The engine's background
// work is not over when its Close returns, so such calls happen; each one makes the session unclean.
var ErrClosed = errors.New("the piece completion is closed")

// Marks is what Completion needs from the marks store. *library.CompletionStore is the real one.
type Marks interface {
	LoadMarks(ctx context.Context, infoHash [20]byte) (map[int]bool, error)
	SaveMarks(ctx context.Context, marks []library.Mark) error
	Close() error
}

// Options tune how marks are read and written; the zero value gives the defaults.
type Options struct {
	FlushEvery time.Duration // default 250 ms
	FlushRows  int           // default 256: the most rows of one batch; this many queued marks wake the writer at once
	Timeout    time.Duration // default 30 s: one read of a torrent's marks, or one batch
}

func (o Options) withDefaults() Options {
	if o.FlushEvery == 0 {
		o.FlushEvery = 250 * time.Millisecond
	}
	if o.FlushRows == 0 {
		o.FlushRows = 256
	}
	if o.Timeout == 0 {
		o.Timeout = 30 * time.Second
	}
	return o
}

// Completion is the engine's piece-completion store (storage.PieceCompletion) over our SQLite file.
//
//   - A torrent's marks are read once; every Get is answered from memory.
//   - Set changes memory at once and queues the row; one goroutine writes the queue in batches, because a
//     durable commit costs 4–11 ms whatever its size (task 009, stage A).
//   - A failed write is sticky: Err, every later Set and Close return it, and nothing is written
//     after it. The engine only logs a failed Set, so this is how the failure reaches the application.
//   - Marks are advisory. Distrust makes pieces unknown until the engine verifies them again.
type Completion struct {
	marks   Marks
	session *Session
	opts    Options

	mu         sync.Mutex
	loads      map[[20]byte]*load
	distrusted map[[20]byte]map[int]bool
	pending    []library.Mark
	err        error
	closed     bool

	flushMu sync.Mutex // keeps batches in order: a later mark of a piece must be written after an earlier one
	kick    chan struct{}
	quit    chan struct{}
	stopped chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// OpenCompletion opens the marks database in the data folder, which must exist.
func OpenCompletion(ctx context.Context, dataDir string, session *Session, opts Options) (*Completion, error) {
	store, err := library.OpenCompletion(ctx, filepath.Join(dataDir, CompletionFile))
	if err != nil {
		return nil, fmt.Errorf("open the piece completion in %s: %w", dataDir, err)
	}
	return NewCompletion(store, session, opts), nil
}

// NewCompletion starts the writer over a marks store. Close it to stop the writer and close the store.
func NewCompletion(marks Marks, session *Session, opts Options) *Completion {
	c := &Completion{
		marks: marks, session: session, opts: opts.withDefaults(),
		loads:      map[[20]byte]*load{},
		distrusted: map[[20]byte]map[int]bool{},
		kick:       make(chan struct{}, 1),
		quit:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	go c.writer()
	return c
}

var _ storage.PieceCompletion = (*Completion)(nil)

// Persistent is false: the engine then does not sync the whole payload file for every completed
// piece, which costs more than four times the transfer (task 009, stage A). The payload is synced
// once at a clean quit, and a crash is verified anyway.
func (c *Completion) Persistent() bool {
	return false
}

// Distrust makes the pieces unknown until the engine verifies them: Get answers "not known" and the
// engine hashes the piece. A Set (the engine's result) ends the distrust. Call it before the torrent
// is added.
func (c *Completion) Distrust(infoHash [20]byte, pieces []int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.distrusted[infoHash] == nil {
		c.distrusted[infoHash] = map[int]bool{}
	}
	for _, p := range pieces {
		c.distrusted[infoHash][p] = true
	}
}

// load is the first read of one torrent's marks. Whoever asks first reads; the others wait for it, with no
// lock held, so a slow read of one torrent holds up no other.
type load struct {
	done  chan struct{}
	marks map[int]bool
	err   error
}

// Get answers from memory; the first call for a torrent reads its marks. A store that cannot be read
// is an error, the same for every later call, which makes the engine disallow download.
func (c *Completion) Get(pk metainfo.PieceKey) (storage.Completion, error) {
	l, err := c.torrent(pk.InfoHash)
	if err != nil {
		return storage.Completion{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		c.session.Fail(ErrClosed)
		return storage.Completion{}, ErrClosed
	}
	if c.distrusted[pk.InfoHash][pk.Index] {
		return storage.Completion{}, nil
	}
	complete, known := l.marks[pk.Index]
	return storage.Completion{Ok: known, Complete: complete}, nil
}

// Set records the engine's verdict on a piece.
func (c *Completion) Set(pk metainfo.PieceKey, complete bool) error {
	l, err := c.torrent(pk.InfoHash)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.session.Fail(ErrClosed)
		return ErrClosed
	}
	l.marks[pk.Index] = complete
	delete(c.distrusted[pk.InfoHash], pk.Index)
	c.pending = append(c.pending, library.Mark{InfoHash: pk.InfoHash, Piece: pk.Index, Complete: complete})
	wake, failed := len(c.pending) >= c.opts.FlushRows, c.err
	c.mu.Unlock()
	if wake {
		select {
		case c.kick <- struct{}{}:
		default:
		}
	}
	return failed
}

// torrent returns a torrent's marks, reading them on first use. It holds no lock while it reads.
func (c *Completion) torrent(infoHash [20]byte) (*load, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		c.session.Fail(ErrClosed)
		return nil, ErrClosed
	}
	l, found := c.loads[infoHash]
	if !found {
		l = &load{done: make(chan struct{})}
		c.loads[infoHash] = l
	}
	c.mu.Unlock()
	if found {
		<-l.done
		return l, l.err
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.opts.Timeout)
	defer cancel()
	l.marks, l.err = c.marks.LoadMarks(ctx, infoHash)
	if l.err != nil {
		l.err = fmt.Errorf("read the piece marks: %w", l.err)
		c.session.Fail(l.err)
	}
	close(l.done)
	return l, l.err
}

// Unresolved lists the pieces of the torrent that were distrusted and that the engine has not yet
// verified: their marks in the store are stale. A record sealed while any is left must not describe
// their files as trusted.
func (c *Completion) Unresolved(infoHash [20]byte) []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	pieces := make([]int, 0, len(c.distrusted[infoHash]))
	for p := range c.distrusted[infoHash] {
		pieces = append(pieces, p)
	}
	slices.Sort(pieces)
	return pieces
}

// Closed reports whether Close has run.
func (c *Completion) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Err is the first failed write of marks, or nil.
func (c *Completion) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *Completion) writer() {
	defer close(c.stopped)
	tick := time.NewTicker(c.opts.FlushEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-c.kick:
		case <-c.quit:
			return
		}
		c.flush()
	}
}

// flush writes the queued marks in batches of at most FlushRows, until none is queued. After a failed
// write nothing more is written.
func (c *Completion) flush() {
	c.flushMu.Lock()
	defer c.flushMu.Unlock()
	for {
		c.mu.Lock()
		n := min(len(c.pending), c.opts.FlushRows)
		batch, failed := c.pending[:n:n], c.err != nil
		c.pending = c.pending[n:]
		c.mu.Unlock()
		if len(batch) == 0 || failed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.opts.Timeout)
		err := c.marks.SaveMarks(ctx, batch)
		cancel()
		if err != nil {
			err = fmt.Errorf("write the piece marks: %w", err)
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			c.session.Fail(err)
			return
		}
	}
}

// Close stops the writer, writes what is queued, and closes the marks store. Later calls of Get and
// Set return ErrClosed. Close may be called again and returns the same result.
func (c *Completion) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.quit)
		<-c.stopped
		c.flush()
		c.mu.Lock()
		failed := c.err
		c.mu.Unlock()
		closeErr := c.marks.Close()
		if closeErr != nil {
			c.session.Fail(fmt.Errorf("close the piece marks: %w", closeErr))
		}
		c.closeErr = errors.Join(failed, closeErr)
	})
	return c.closeErr
}
