package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
)

var (
	// ErrRootChanged is returned once the save folder is not the folder the storage was made for: it
	// was deleted, moved or replaced, or its volume was detached or swapped. The engine would recreate
	// the folder, and write the payload, wherever it finds the path.
	ErrRootChanged = errors.New("the save folder is no longer the folder this storage was made for")
	// ErrStopped is returned by an operation that reaches the storage after Stop.
	ErrStopped = errors.New("the torrent storage is stopped")
	// ErrFileSystem is returned for a save folder on a file system the pinned engine cannot hash on.
	ErrFileSystem = errors.New("the engine cannot verify pieces on this file system")
	// ErrNotStopped is returned by Seal before Stop has drained the operations in flight: while they
	// can still change files, no record of them can be exact.
	ErrNotStopped = errors.New("the torrent storage is not stopped")
	// ErrNotClosed is returned by Seal while the piece marks are open: their last failure is not known.
	ErrNotClosed = errors.New("the piece marks are not closed")
	// ErrUnsafePath is returned for a torrent whose path leads through a symlink, or onto an existing
	// file that the engine would truncate.
	ErrUnsafePath = errors.New("a path of the torrent is not safe to write")
)

// unsupportedFileSystems are the file systems on which the engine's classic file IO cannot hash a
// piece: it seeks with SEEK_DATA, which HFS+ does not support, so every piece fails its check and the
// download never completes (finding 21). APFS, exFAT and FAT work.
var unsupportedFileSystems = []string{"hfs"}

// opKind says what an operation does to the files: only a mutation after Stop makes the session
// unclean; a refused read changes nothing.
type opKind int

const (
	opRead opKind = iota
	opMutate
)

// marksState is what the guard asks of the piece marks to seal a record: the pieces still distrusted
// (their marks are stale), and whether the marks are closed. *Completion has both.
type marksState interface {
	Unresolved(infoHash [20]byte) []int
	Closed() bool
}

// TorrentStorage is the storage of one torrent: the engine's file storage inside a guard. It
// implements storage.ClientImpl and has no Close, because the engine never closes a storage it was
// given; the owner calls Stop.
//
// Before every operation the guard checks that the save folder is still the one recorded at the start
// (device, inode and creation time), and refuses otherwise, without calling the engine. The check and
// the engine's own open are not one step: when the folder disappears between them, one chunk write or
// one folder creation can still reach the old path. The guard records every failure in the session,
// counts piece reads, and, after Stop, refuses new operations.
type TorrentStorage struct {
	inner   storage.ClientImpl
	root    string
	rootID  files.Identity
	session *Session
	marks   marksState // nil when the completion does not report its state
	reads   atomic.Int64
	lost    atomic.Pointer[error]
	tr      *tracker

	mu       sync.RWMutex // orders flight.Add against Stop; never held while the engine runs
	stopped  bool
	flight   sync.WaitGroup
	idleOnce sync.Once
	idle     chan struct{}
	drained  atomic.Bool
}

var _ storage.ClientImpl = (*TorrentStorage)(nil)

// newGuard guards inner for the save folder root, which must be an existing, clean, absolute folder.
func newGuard(inner storage.ClientImpl, root string, session *Session, marks marksState) (*TorrentStorage, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("the save folder %q is not a clean absolute path", root)
	}
	id, err := files.IdentityOf(root)
	var fsType string
	if err == nil {
		fsType, err = files.FileSystem(root)
	}
	if err != nil {
		return nil, fmt.Errorf("the save folder: %w", err)
	}
	if slices.Contains(unsupportedFileSystems, fsType) {
		return nil, fmt.Errorf("%w: %s", ErrFileSystem, fsType)
	}
	if !id.Dir {
		return nil, fmt.Errorf("the save folder %q is not a folder", root)
	}
	return &TorrentStorage{inner: inner, root: root, rootID: id, session: session, marks: marks, tr: newTracker()}, nil
}

// enter admits one operation: not after Stop, and only while the save folder is the recorded one.
// The returned function ends the operation. The folder is checked outside the lock, so a stalled
// volume cannot keep Stop from starting.
func (s *TorrentStorage) enter(kind opKind) (func(), error) {
	s.mu.RLock()
	if s.stopped {
		s.mu.RUnlock()
		if kind == opMutate {
			s.session.Fail(ErrStopped)
		}
		return nil, ErrStopped
	}
	s.flight.Add(1)
	s.mu.RUnlock()
	if err := s.checkRoot(); err != nil {
		s.flight.Done()
		return nil, err
	}
	return s.flight.Done, nil
}

// checkRoot reports whether the save folder is still the recorded one; a loss is permanent and
// recorded in the session once.
func (s *TorrentStorage) checkRoot() error {
	if lost := s.lost.Load(); lost != nil {
		return *lost
	}
	now, err := files.IdentityOf(s.root)
	if err == nil && now == s.rootID {
		return nil
	}
	if err == nil {
		err = errors.New("another folder is there now")
	}
	err = fmt.Errorf("%w: %s: %v", ErrRootChanged, s.root, err)
	if s.lost.CompareAndSwap(nil, &err) {
		s.session.Fail(err)
	}
	return *s.lost.Load()
}

// note records a failure of the engine's storage; a short read (io.EOF) is the engine's own signal
// that data is missing and not a storage problem.
func (s *TorrentStorage) note(err error) error {
	if err != nil && !errors.Is(err, io.EOF) {
		s.session.Fail(err)
	}
	return err
}

// Reads is the number of piece reads (ReadAt and WriteTo) that reached the engine's storage. A
// torrent whose marks were trusted is not read until a peer asks for data.
func (s *TorrentStorage) Reads() int64 {
	return s.reads.Load()
}

// Stop refuses new operations and waits, until ctx ends, for those in flight. Call it after the
// engine client is closed and before the payload is synced and fingerprinted. If ctx ends first, the
// operations still running make the session unclean and Seal refuses until they have finished. Stop
// may be called again.
func (s *TorrentStorage) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.idleOnce.Do(func() {
		s.idle = make(chan struct{})
		go func() {
			s.flight.Wait()
			s.drained.Store(true)
			close(s.idle)
		}()
	})
	select {
	case <-s.idle:
		return nil
	case <-ctx.Done():
		err := fmt.Errorf("operations of the torrent storage are still running: %w", ctx.Err())
		s.session.Fail(err)
		return err
	}
}

// OpenTorrent opens the engine's storage for the torrent, guarded. The engine calls it more than
// once per added torrent, and on every start. A torrent whose path leads through a symlink, or onto
// a file the engine would truncate, is refused before the engine sees it. A storage that offers
// more than Piece and Close is refused: the guard could not cover what it does not wrap.
func (s *TorrentStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	done, err := s.enter(opMutate)
	if err != nil {
		return storage.TorrentImpl{}, err
	}
	defer done()
	ranges, err := Ranges(s.root, info)
	if err != nil {
		return storage.TorrentImpl{}, err
	}
	if err := checkPaths(s.root, info, ranges); err != nil {
		return storage.TorrentImpl{}, err
	}
	if err := s.tr.learn(ranges, info, infoHash); err != nil {
		return storage.TorrentImpl{}, err
	}
	impl, err := s.inner.OpenTorrent(ctx, info, infoHash)
	if err != nil {
		s.session.Fail(err)
		return storage.TorrentImpl{}, err
	}
	closeImpl := func() error {
		if impl.Close == nil {
			return nil
		}
		return impl.Close()
	}
	if extra := unsupported(impl); extra != "" {
		return storage.TorrentImpl{}, errors.Join(fmt.Errorf("the engine storage offers %s, which the guard does not cover", extra), closeImpl())
	}
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return &guardedPiece{s: s, inner: impl.Piece(p), offset: p.Offset(), length: p.Length()}
		},
		Close: closeImpl,
	}, nil
}

func unsupported(impl storage.TorrentImpl) string {
	switch {
	case impl.PieceWithHash != nil:
		return "PieceWithHash"
	case impl.Capacity != nil:
		return "Capacity"
	case impl.NewReader != nil:
		return "NewReader"
	case impl.NewPieceReader != nil:
		return "NewPieceReader"
	}
	return ""
}

// guardedPiece is one piece of the engine's storage behind the guard.
type guardedPiece struct {
	s      *TorrentStorage
	inner  storage.PieceImpl
	offset int64 // of the piece in the torrent
	length int64
}

func (p *guardedPiece) ReadAt(b []byte, off int64) (int, error) {
	done, err := p.s.enter(opRead)
	if err != nil {
		return 0, err
	}
	defer done()
	p.s.reads.Add(1)
	n, err := p.inner.ReadAt(b, off)
	return n, p.s.note(err)
}

// WriteTo is how the engine reads a whole piece to hash it or to send it.
func (p *guardedPiece) WriteTo(w io.Writer) (int64, error) {
	done, err := p.s.enter(opRead)
	if err != nil {
		return 0, err
	}
	defer done()
	p.s.reads.Add(1)
	if wt, ok := p.inner.(io.WriterTo); ok {
		n, err := wt.WriteTo(w)
		if errors.Is(err, io.ErrShortWrite) {
			// Classic file IO ends a piece that stops inside its file this way; the engine only logs
			// it and the hash is right (finding 17). It is not a storage problem.
			return n, err
		}
		return n, p.s.note(err)
	}
	n, err := io.Copy(w, io.NewSectionReader(p.inner, 0, p.length))
	return n, p.s.note(err)
}

func (p *guardedPiece) WriteAt(b []byte, off int64) (int, error) {
	done, err := p.s.enter(opMutate)
	if err != nil {
		return 0, err
	}
	defer done()
	start := p.offset + off
	p.s.tr.beforeWrite(start, int64(len(b)))
	n, err := p.inner.WriteAt(b, off)
	p.s.tr.afterWrite(start, int64(len(b)))
	return n, p.s.note(err)
}

func (p *guardedPiece) MarkComplete() error {
	done, err := p.s.enter(opMutate)
	if err != nil {
		return err
	}
	defer done()
	err = p.s.note(p.inner.MarkComplete())
	p.s.tr.verified(p.offset, p.length)
	return err
}

func (p *guardedPiece) MarkNotComplete() error {
	done, err := p.s.enter(opMutate)
	if err != nil {
		return err
	}
	defer done()
	return p.s.note(p.inner.MarkNotComplete())
}

// Completion does not reach the engine's storage once the folder is gone: it would mark the pieces
// of every file it cannot find as incomplete, in our marks.
func (p *guardedPiece) Completion() storage.Completion {
	done, err := p.s.enter(opRead)
	if err != nil {
		return storage.Completion{Err: err}
	}
	defer done()
	c := p.inner.Completion()
	p.s.note(c.Err)
	return c
}
