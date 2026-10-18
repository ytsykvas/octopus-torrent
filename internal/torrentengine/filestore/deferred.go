package filestore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var (
	// ErrNotAttached is every piece operation of a Deferred before its final storage is attached:
	// no payload is read, written or marked before the import is confirmed.
	ErrNotAttached = errors.New("the torrent's storage is not attached yet")
	// ErrAttached is a second Attach: the final storage of a torrent is chosen once.
	ErrAttached = errors.New("the deferred storage is already attached")
	// ErrAttaching is an Attach while another one is opening its storage.
	ErrAttaching = errors.New("another storage is being attached")
	// ErrNotOpened is an Attach before the engine opened the storage: there is no metadata yet.
	ErrNotOpened = errors.New("the deferred storage has no metadata yet")
	// ErrNoFinalStorage is an Attach without a storage to attach.
	ErrNoFinalStorage = errors.New("no storage to attach")
	// ErrDeferredClosed is an Attach after the engine closed the torrent (Drop, client shutdown): the
	// user's folder is not opened for a torrent that no longer exists.
	ErrDeferredClosed = errors.New("the torrent was closed before its storage was attached")
)

// Deferred is the storage of a torrent whose folder is not chosen yet, such as a magnet waiting for
// the user to confirm the import. The engine opens it when the metadata arrives; it creates nothing,
// reports every piece as known and not complete (so the engine checks nothing) and refuses every
// read, write and mark, counting them. Attach opens the final storage with the same metadata and
// makes every later piece operation go there: the engine looks a piece's storage up on every access,
// so the torrent needs neither a Drop nor a second add. One Deferred serves one torrent.
//
// The engine opens the storage with an info before it applies it, and fails SetInfoBytes when the
// open fails. OpenTorrent therefore runs an admission rule first: an info it refuses is never
// applied, whichever add path set it on the torrent.
type Deferred struct {
	admit     func(*metainfo.Info) error
	mu        sync.Mutex // orders OpenTorrent, Close and each step of Attach; never held during file work
	info      *metainfo.Info
	infoHash  metainfo.Hash
	closed    bool
	attaching bool                                // an Attach is opening its storage
	inner     atomic.Pointer[storage.TorrentImpl] // read without mu on every piece access
	touched   atomic.Int64
}

// NewDeferred returns a Deferred with nothing attached that admits only the info admit accepts. A
// nil admit is a programming error and panics here, not later under the engine's lock.
func NewDeferred(admit func(*metainfo.Info) error) *Deferred {
	if admit == nil {
		panic("filestore: NewDeferred needs an admission rule")
	}
	return &Deferred{admit: admit}
}

// OpenTorrent admits the info, records it and returns a torrent that goes to the final storage once
// it is attached. A refused info is not recorded, and the refusal is the engine's error.
func (d *Deferred) OpenTorrent(_ context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.admit(info); err != nil {
		return storage.TorrentImpl{}, err
	}
	d.info, d.infoHash = info, infoHash
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			if in := d.inner.Load(); in != nil {
				return in.Piece(p)
			}
			return detachedPiece{d}
		},
		Close: d.close,
	}, nil
}

// close is the torrent's Close: the engine calls it when the torrent is dropped or the client closes.
// It closes the attached storage once and refuses every later Attach.
func (d *Deferred) close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	if in := d.inner.Load(); in != nil && in.Close != nil {
		return in.Close()
	}
	return nil
}

// Attach opens final with the recorded metadata and makes it the storage of every later piece
// operation. Its error is returned and leaves the Deferred detached, so another storage can be tried.
// The open runs without the lock: it does file work in the user's folder, and the engine closes the
// torrent under its client lock. A torrent closed during the open gets nothing attached; what was
// opened for it is closed.
func (d *Deferred) Attach(ctx context.Context, final storage.ClientImpl) error {
	info, infoHash, err := d.beginAttach(final)
	if err != nil {
		return err
	}
	impl, err := final.OpenTorrent(ctx, info, infoHash)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.attaching = false
	if err != nil {
		return err
	}
	// The engine reads a torrent's optional fields from the Deferred, so only Piece and Close can be
	// passed on: the guard's own rule.
	refusal := unsupported(impl)
	if impl.Piece == nil {
		refusal = "no Piece"
	}
	switch {
	case refusal != "":
		return errors.Join(fmt.Errorf("the final storage offers %s, which a deferred storage cannot pass on", refusal), closeImpl(impl))
	case d.closed:
		return errors.Join(ErrDeferredClosed, closeImpl(impl))
	}
	d.inner.Store(&impl)
	return nil
}

// beginAttach admits one Attach and returns the metadata to open the final storage with.
func (d *Deferred) beginAttach(final storage.ClientImpl) (*metainfo.Info, metainfo.Hash, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case final == nil:
		return nil, metainfo.Hash{}, ErrNoFinalStorage
	case d.closed:
		return nil, metainfo.Hash{}, ErrDeferredClosed
	case d.inner.Load() != nil:
		return nil, metainfo.Hash{}, ErrAttached
	case d.attaching:
		return nil, metainfo.Hash{}, ErrAttaching
	case d.info == nil:
		return nil, metainfo.Hash{}, ErrNotOpened
	}
	d.attaching = true
	return d.info, d.infoHash, nil
}

func closeImpl(impl storage.TorrentImpl) error {
	if impl.Close == nil {
		return nil
	}
	return impl.Close()
}

// Touched is the number of piece operations refused before the attach. The engine only logs a
// storage error, so this count is how a test proves that nothing was read, written or marked.
func (d *Deferred) Touched() int64 {
	return d.touched.Load()
}

// detachedPiece is a piece of a Deferred before the attach.
type detachedPiece struct{ d *Deferred }

func (p detachedPiece) refuse() error {
	p.d.touched.Add(1)
	return ErrNotAttached
}

func (p detachedPiece) ReadAt([]byte, int64) (int, error)  { return 0, p.refuse() }
func (p detachedPiece) WriteAt([]byte, int64) (int, error) { return 0, p.refuse() }
func (p detachedPiece) MarkComplete() error                { return p.refuse() }
func (p detachedPiece) MarkNotComplete() error             { return p.refuse() }

// Completion reports the piece as known and not complete, so the engine queues no check that would
// read it.
func (detachedPiece) Completion() storage.Completion { return storage.Completion{Ok: true} }
