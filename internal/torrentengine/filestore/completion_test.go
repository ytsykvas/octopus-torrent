package filestore

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/library"
)

var (
	hashA = [20]byte{1}
	hashB = [20]byte{2}
)

func key(h [20]byte, piece int) metainfo.PieceKey {
	return metainfo.PieceKey{InfoHash: h, Index: piece}
}

// doubleMarks is a test double for the library's marks store: it records every call and can be told
// to fail. The real store is used by the other tests of this file.
type doubleMarks struct {
	mu       sync.Mutex
	stored   map[[20]byte]map[int]bool
	loads    int
	batches  [][]library.Mark
	saved    chan struct{}
	loadErr  error
	saveErr  error
	closeErr error
	closed   int
}

func newDouble() *doubleMarks {
	return &doubleMarks{stored: map[[20]byte]map[int]bool{}, saved: make(chan struct{}, 100)}
}

func (d *doubleMarks) LoadMarks(_ context.Context, h [20]byte) (map[int]bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loads++
	if d.loadErr != nil {
		return nil, d.loadErr
	}
	out := map[int]bool{}
	for k, v := range d.stored[h] {
		out[k] = v
	}
	return out, nil
}

func (d *doubleMarks) SaveMarks(_ context.Context, marks []library.Mark) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.saveErr != nil {
		return d.saveErr
	}
	d.batches = append(d.batches, append([]library.Mark(nil), marks...))
	for _, m := range marks {
		if d.stored[m.InfoHash] == nil {
			d.stored[m.InfoHash] = map[int]bool{}
		}
		d.stored[m.InfoHash][m.Piece] = m.Complete
	}
	d.saved <- struct{}{}
	return nil
}

func (d *doubleMarks) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed++
	return d.closeErr
}

func (d *doubleMarks) batchSizes() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var sizes []int
	for _, b := range d.batches {
		sizes = append(sizes, len(b))
	}
	return sizes
}

// slowFlush keeps the timer out of the way of tests that count batches.
var slowFlush = Options{FlushEvery: time.Hour, FlushRows: 3}

func completion(t *testing.T, d *doubleMarks, o Options) (*Completion, *Session) {
	t.Helper()
	var s Session
	c := NewCompletion(d, &s, o)
	t.Cleanup(func() { _ = c.Close() })
	return c, &s
}

func TestEveryGetIsAnsweredFromMemoryAfterOneLoadPerTorrent(t *testing.T) {
	d := newDouble()
	d.stored[hashA] = map[int]bool{0: true, 1: false}
	c, _ := completion(t, d, slowFlush)
	for range 5 {
		for piece, want := range map[int]struct{ ok, complete bool }{0: {true, true}, 1: {true, false}, 2: {false, false}} {
			got, err := c.Get(key(hashA, piece))
			if err != nil || got.Ok != want.ok || got.Complete != want.complete || got.Err != nil {
				t.Fatalf("Get(%d) = %+v, %v; want ok=%v complete=%v", piece, got, err, want.ok, want.complete)
			}
		}
	}
	if _, err := c.Get(key(hashB, 0)); err != nil {
		t.Fatal(err)
	}
	if d.loads != 2 {
		t.Fatalf("%d loads for 2 torrents", d.loads)
	}
}

func TestAMarkIsInMemoryAtOnceAndWrittenInBatches(t *testing.T) {
	d := newDouble()
	c, _ := completion(t, d, slowFlush)
	for piece := range 7 {
		if err := c.Set(key(hashA, piece), piece%2 == 0); err != nil {
			t.Fatal(err)
		}
		got, err := c.Get(key(hashA, piece))
		if err != nil || !got.Ok || got.Complete != (piece%2 == 0) {
			t.Fatalf("Get after Set(%d) = %+v, %v", piece, got, err)
		}
	}
	// Three queued marks wake the writer, so something is written before Close.
	select {
	case <-d.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was written without Close")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, n := range d.batchSizes() {
		if n > 3 {
			t.Fatalf("a batch of %d rows exceeds the limit of 3", n)
		}
		total += n
	}
	if total != 7 || len(d.stored[hashA]) != 7 {
		t.Fatalf("%d rows written, %d stored; want 7", total, len(d.stored[hashA]))
	}
	if d.closed != 1 {
		t.Fatalf("the marks store was closed %d times", d.closed)
	}
}

func TestAMarkIsWrittenWithinTheFlushIntervalWithoutClose(t *testing.T) {
	d := newDouble()
	c, _ := completion(t, d, Options{FlushEvery: 10 * time.Millisecond, FlushRows: 100})
	if err := c.Set(key(hashA, 4), true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("the mark was not written by the timer")
	}
	if got := d.stored[hashA]; !got[4] {
		t.Fatalf("stored %v", got)
	}
}

func TestALaterMarkOfAPieceWinsInTheStore(t *testing.T) {
	d := newDouble()
	c, _ := completion(t, d, slowFlush)
	for _, complete := range []bool{true, false, true, false} {
		if err := c.Set(key(hashA, 9), complete); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if d.stored[hashA][9] {
		t.Fatal("an earlier mark overwrote a later one")
	}
}

func TestAFailedWriteIsStickyAndReportedByEveryLaterCall(t *testing.T) {
	d := newDouble()
	failure := errors.New("the disk is full")
	d.saveErr = failure
	c, s := completion(t, d, Options{FlushEvery: time.Hour, FlushRows: 1})
	if err := c.Set(key(hashA, 0), true); err != nil {
		t.Fatalf("the first Set cannot know about the write yet: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for c.Err() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the failed write was not reported")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !errors.Is(c.Err(), failure) {
		t.Fatalf("Err = %v", c.Err())
	}
	if err := c.Set(key(hashA, 1), true); !errors.Is(err, failure) {
		t.Fatalf("Set after a failed write = %v, want the failure", err)
	}
	// the piece stays complete in memory: the engine only logs the error
	if got, _ := c.Get(key(hashA, 1)); !got.Ok || !got.Complete {
		t.Fatalf("Get = %+v", got)
	}
	d.mu.Lock()
	d.saveErr = nil // the store recovers, but a failed session does not write again
	d.mu.Unlock()
	_ = c.Set(key(hashA, 2), true)
	if err := c.Close(); !errors.Is(err, failure) {
		t.Fatalf("Close = %v, want the failed write", err)
	}
	if len(d.batches) != 0 {
		t.Fatalf("a failed session wrote %v", d.batches)
	}
	if s.Clean() {
		t.Fatal("a failed write left the session clean")
	}
}

func TestAMarksStoreThatCannotLoadIsAnExplicitError(t *testing.T) {
	d := newDouble()
	d.loadErr = errors.New("the store is unreadable")
	c, s := completion(t, d, slowFlush)
	if _, err := c.Get(key(hashA, 0)); !errors.Is(err, d.loadErr) {
		t.Fatalf("Get = %v, want the load failure", err)
	}
	if err := c.Set(key(hashA, 0), true); !errors.Is(err, d.loadErr) {
		t.Fatalf("Set = %v, want the load failure", err)
	}
	if s.Clean() {
		t.Fatal("an unreadable store left the session clean")
	}
}

func TestCloseFlushesWaitsAndMayBeRepeated(t *testing.T) {
	d := newDouble()
	d.closeErr = errors.New("checkpoint blocked")
	c, _ := completion(t, d, slowFlush)
	if err := c.Set(key(hashA, 0), true); err != nil {
		t.Fatal(err)
	}
	first := c.Close()
	if !errors.Is(first, d.closeErr) {
		t.Fatalf("Close = %v, want the store's close error", first)
	}
	if d.stored[hashA][0] != true {
		t.Fatal("Close did not flush")
	}
	if second := c.Close(); second != first {
		t.Fatalf("a second Close = %v, want %v", second, first)
	}
	if d.closed != 1 {
		t.Fatalf("closed %d times", d.closed)
	}
}

func TestACallAfterCloseIsRefusedAndMakesTheSessionUnclean(t *testing.T) {
	d := newDouble()
	c, s := completion(t, d, slowFlush)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if !s.Clean() {
		t.Fatal("a quiet Close left the session unclean")
	}
	if _, err := c.Get(key(hashA, 0)); !errors.Is(err, ErrClosed) {
		t.Fatalf("Get after Close = %v", err)
	}
	if err := c.Set(key(hashA, 0), true); !errors.Is(err, ErrClosed) {
		t.Fatalf("Set after Close = %v", err)
	}
	if s.Clean() || len(s.Problems()) != 2 {
		t.Fatalf("session problems = %v", s.Problems())
	}
}

func TestADistrustedPieceIsUnknownUntilItIsVerifiedAgain(t *testing.T) {
	d := newDouble()
	d.stored[hashA] = map[int]bool{0: true, 1: true, 2: true}
	c, _ := completion(t, d, slowFlush)
	c.Distrust(hashA, []int{1, 2})
	if got, _ := c.Get(key(hashA, 0)); !got.Ok || !got.Complete {
		t.Fatalf("a trusted piece = %+v", got)
	}
	for _, piece := range []int{1, 2} {
		if got, _ := c.Get(key(hashA, piece)); got.Ok {
			t.Fatalf("distrusted piece %d = %+v, want unknown", piece, got)
		}
	}
	if err := c.Set(key(hashA, 1), true); err != nil { // the engine verified the piece
		t.Fatal(err)
	}
	if got, _ := c.Get(key(hashA, 1)); !got.Ok || !got.Complete {
		t.Fatalf("a verified piece = %+v", got)
	}
	if got, _ := c.Get(key(hashA, 2)); got.Ok {
		t.Fatalf("the other distrusted piece = %+v", got)
	}
	c.Distrust(hashB, []int{0}) // before the torrent was ever read
	if got, _ := c.Get(key(hashB, 0)); got.Ok {
		t.Fatalf("a distrusted piece of an unread torrent = %+v", got)
	}
}

func TestThisCompletionAsksTheEngineNotToSyncEveryPiece(t *testing.T) {
	c, _ := completion(t, newDouble(), slowFlush)
	if c.Persistent() {
		t.Fatal("Persistent must be false: a clean quit syncs the payload once (task 009, stage A)")
	}
}

func TestCompletionIsSafeForConcurrentUse(t *testing.T) {
	d := newDouble()
	c, _ := completion(t, d, Options{FlushEvery: time.Millisecond, FlushRows: 5})
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for piece := range 50 {
				_ = c.Set(key(hashA, g*50+piece), true)
				_, _ = c.Get(key(hashA, piece))
			}
		}()
	}
	wg.Wait()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if len(d.stored[hashA]) != 400 {
		t.Fatalf("%d marks stored, want 400", len(d.stored[hashA]))
	}
}

func TestTheRealStoreKeepsMarksAcrossAClosedAndReopenedCompletion(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var s Session
	c, err := OpenCompletion(ctx, dir, &s, slowFlush)
	if err != nil {
		t.Fatal(err)
	}
	for piece := range 5 {
		if err := c.Set(key(hashA, piece), piece != 3); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := filepathStat(filepath.Join(dir, CompletionFile)); err != nil {
		t.Fatal(err)
	}
	again, err := OpenCompletion(ctx, dir, &s, slowFlush)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	for piece := range 5 {
		got, err := again.Get(key(hashA, piece))
		if err != nil || !got.Ok || got.Complete != (piece != 3) {
			t.Fatalf("after a reopen Get(%d) = %+v, %v", piece, got, err)
		}
	}
}

func TestACompletionThatCannotBeOpenedIsAnErrorAndNeverFallsBackToMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var s Session
	for name, dir := range map[string]string{
		"a missing folder":      filepath.Join(t.TempDir(), "no", "such"),
		"a relative folder":     "relative",
		"a path that is a file": writeFile(t),
	} {
		c, err := OpenCompletion(ctx, dir, &s, slowFlush)
		if err == nil || c != nil {
			t.Fatalf("%s: OpenCompletion = %v, %v; want an error and no store", name, c, err)
		}
	}
}

func TestTheDefaultsOfTheOptionsAreSetWhereNoneIsGiven(t *testing.T) {
	got := Options{}.withDefaults()
	if got.FlushEvery != 250*time.Millisecond || got.FlushRows != 256 || got.WriteTimeout != 30*time.Second {
		t.Fatalf("defaults = %+v", got)
	}
	given := Options{FlushEvery: time.Second, FlushRows: 7, WriteTimeout: time.Minute}
	if given.withDefaults() != given {
		t.Fatalf("given options were changed: %+v", given.withDefaults())
	}
}
