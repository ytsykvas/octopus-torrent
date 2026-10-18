package filestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// innerStorage is a storage under a Deferred that records what reaches it.
type innerStorage struct {
	err     error  // returned by OpenTorrent
	noClose bool   // the opened torrent has no Close
	noPiece bool   // the opened torrent has no Piece
	reader  bool   // the opened torrent offers its own NewReader
	during  func() // runs inside OpenTorrent, before it returns
	opened  int
	pieces  int
	closed  int
}

func (s *innerStorage) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	s.opened++
	if s.during != nil {
		s.during()
	}
	if s.err != nil {
		return storage.TorrentImpl{}, s.err
	}
	impl := storage.TorrentImpl{Piece: func(metainfo.Piece) storage.PieceImpl {
		s.pieces++
		return nil
	}}
	if !s.noClose {
		impl.Close = func() error { s.closed++; return nil }
	}
	if s.noPiece {
		impl.Piece = nil
	}
	if s.reader {
		impl.NewReader = func() storage.TorrentReader { return nil }
	}
	return impl, nil
}

func openedDeferred(t *testing.T) (*Deferred, storage.TorrentImpl, *metainfo.Info) {
	t.Helper()
	_, mi := named(t, "v1-single")
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	d := NewDeferred()
	impl, err := d.OpenTorrent(context.Background(), &info, mi.HashInfoBytes())
	if err != nil {
		t.Fatal(err)
	}
	return d, impl, &info
}

func TestADeferredStorageRefusesEveryPieceOperationUntilAttached(t *testing.T) {
	d, impl, info := openedDeferred(t)
	p := impl.Piece(info.Piece(0))
	if c := p.Completion(); !c.Ok || c.Complete || c.Err != nil {
		t.Errorf("Completion = %+v, want known and not complete, so the engine checks nothing", c)
	}
	if _, err := p.ReadAt(make([]byte, 1), 0); !errors.Is(err, ErrNotAttached) {
		t.Errorf("ReadAt = %v", err)
	}
	if _, err := p.WriteAt([]byte{1}, 0); !errors.Is(err, ErrNotAttached) {
		t.Errorf("WriteAt = %v", err)
	}
	if err := p.MarkComplete(); !errors.Is(err, ErrNotAttached) {
		t.Errorf("MarkComplete = %v", err)
	}
	if err := p.MarkNotComplete(); !errors.Is(err, ErrNotAttached) {
		t.Errorf("MarkNotComplete = %v", err)
	}
	if got := d.Touched(); got != 4 {
		t.Errorf("Touched = %d, want the 4 refused operations (Completion is not one)", got)
	}
	if err := impl.Close(); err != nil {
		t.Errorf("Close before the attach = %v", err)
	}
}

func TestAttachOpensTheFinalStorageAndThenDelegates(t *testing.T) {
	d, impl, info := openedDeferred(t)
	inner := &innerStorage{}
	if err := d.Attach(context.Background(), inner); err != nil {
		t.Fatal(err)
	}
	impl.Piece(info.Piece(0))
	if err := d.Attach(context.Background(), &innerStorage{}); !errors.Is(err, ErrAttached) {
		t.Errorf("second Attach = %v, want ErrAttached", err)
	}
	if err := impl.Close(); err != nil {
		t.Fatal(err)
	}
	if inner.opened != 1 || inner.pieces != 1 || inner.closed != 1 || d.Touched() != 0 {
		t.Errorf("inner opened %d, pieces %d, closed %d; touched %d", inner.opened, inner.pieces, inner.closed, d.Touched())
	}
}

func TestAFailedAttachLeavesTheStorageDetached(t *testing.T) {
	d, impl, info := openedDeferred(t)
	if err := d.Attach(context.Background(), Refusing()); !errors.Is(err, ErrNoSaveFolder) {
		t.Fatalf("Attach to a refusing storage = %v, want its error", err)
	}
	if _, err := impl.Piece(info.Piece(0)).WriteAt([]byte{1}, 0); !errors.Is(err, ErrNotAttached) {
		t.Errorf("after a failed attach WriteAt = %v, want ErrNotAttached", err)
	}
	inner := &innerStorage{noClose: true}
	if err := d.Attach(context.Background(), inner); err != nil {
		t.Fatalf("a later attach = %v", err)
	}
	if err := impl.Close(); err != nil || inner.opened != 1 {
		t.Errorf("Close of an inner torrent without Close = %v, opened %d", err, inner.opened)
	}
}

// The engine reads a torrent's optional storage fields from the Deferred, never from the attached
// storage, so a final storage may offer only Piece and Close, the guard's own rule.
func TestAttachRefusesAStorageItCannotDelegateTo(t *testing.T) {
	for _, inner := range []*innerStorage{{noPiece: true}, {reader: true}, {reader: true, noClose: true}} {
		d, impl, info := openedDeferred(t)
		wantClosed := 1
		if inner.noClose {
			wantClosed = 0
		}
		if err := d.Attach(context.Background(), inner); err == nil || inner.closed != wantClosed {
			t.Errorf("Attach of %+v = %v, closed %d; want an error and the opened torrent closed", *inner, err, inner.closed)
		}
		if _, err := impl.Piece(info.Piece(0)).WriteAt([]byte{1}, 0); !errors.Is(err, ErrNotAttached) {
			t.Errorf("after a refused attach WriteAt = %v, want ErrNotAttached", err)
		}
	}
}

func TestAttachWithoutAStorageIsRefused(t *testing.T) {
	d, impl, info := openedDeferred(t)
	if err := d.Attach(context.Background(), nil); !errors.Is(err, ErrNoFinalStorage) {
		t.Fatalf("Attach(nil) = %v, want ErrNoFinalStorage", err)
	}
	if _, err := impl.Piece(info.Piece(0)).WriteAt([]byte{1}, 0); !errors.Is(err, ErrNotAttached) {
		t.Errorf("after Attach(nil) WriteAt = %v, want ErrNotAttached", err)
	}
}

func TestAttachBeforeTheMetadataIsRefused(t *testing.T) {
	inner := &innerStorage{}
	if err := NewDeferred().Attach(context.Background(), inner); !errors.Is(err, ErrNotOpened) || inner.opened != 0 {
		t.Errorf("Attach before OpenTorrent = %v, inner opened %d", err, inner.opened)
	}
}

// The engine closes the torrent's storage when the torrent is dropped or the client closes. An attach
// that comes after must not open the user's folder for a torrent that no longer exists.
func TestAClosedDeferredStorageOpensNoFinalStorage(t *testing.T) {
	d, impl, _ := openedDeferred(t)
	if err := impl.Close(); err != nil {
		t.Fatal(err)
	}
	inner := &innerStorage{}
	if err := d.Attach(context.Background(), inner); !errors.Is(err, ErrDeferredClosed) || inner.opened != 0 {
		t.Errorf("Attach after Close = %v, inner opened %d", err, inner.opened)
	}

	d, impl, _ = openedDeferred(t)
	inner = &innerStorage{}
	if err := d.Attach(context.Background(), inner); err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(impl.Close(), impl.Close()); err != nil || inner.closed != 1 {
		t.Errorf("two Closes after the attach = %v, inner closed %d times, want once", err, inner.closed)
	}
}

// The final storage's open does file work in the user's folder and can stall on a slow volume. The
// engine closes the torrent under its client lock, so the open must not hold the Deferred's lock: a
// Close during the open returns at once, and the attach that finishes after it closes what it opened
// and attaches nothing. A second Attach during the open is refused.
func TestAnAttachInProgressBlocksNeitherCloseNorAnotherAttach(t *testing.T) {
	d, impl, info := openedDeferred(t)
	var closeErr, secondErr error
	inner := &innerStorage{}
	inner.during = func() {
		secondErr = d.Attach(context.Background(), &innerStorage{})
		done := make(chan error, 1)
		go func() { done <- impl.Close() }()
		select {
		case closeErr = <-done:
		case <-time.After(time.Second):
			closeErr = errors.New("Close waited for the attach's open")
		}
	}
	if err := d.Attach(context.Background(), inner); !errors.Is(err, ErrDeferredClosed) {
		t.Errorf("an attach whose torrent was closed during the open = %v, want ErrDeferredClosed", err)
	}
	if closeErr != nil || !errors.Is(secondErr, ErrAttaching) {
		t.Errorf("during the open: Close = %v, a second Attach = %v (want nil, ErrAttaching)", closeErr, secondErr)
	}
	if inner.closed != 1 {
		t.Errorf("the storage opened for a closed torrent was closed %d times, want once", inner.closed)
	}
	if _, err := impl.Piece(info.Piece(0)).WriteAt([]byte{1}, 0); !errors.Is(err, ErrNotAttached) {
		t.Errorf("after the late attach WriteAt = %v, want ErrNotAttached", err)
	}

	// A failed open ends the attach in progress, so another storage can be tried.
	d, _, _ = openedDeferred(t)
	if err := d.Attach(context.Background(), &innerStorage{err: ErrNoSaveFolder}); !errors.Is(err, ErrNoSaveFolder) {
		t.Fatalf("a failed open = %v", err)
	}
	if err := d.Attach(context.Background(), &innerStorage{}); err != nil {
		t.Errorf("an attach after a failed one = %v", err)
	}
}
