package filestore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/segments"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
)

var (
	// ErrRootChanged is returned once the save folder is not the folder the storage was made for: it
	// was deleted, moved or replaced, or its volume was detached. The engine would recreate the
	// folder, and write the payload, wherever it finds the path.
	ErrRootChanged = errors.New("the save folder is no longer the folder this storage was made for")
	// ErrStopped is returned by an operation that reaches the storage after Stop.
	ErrStopped = errors.New("the torrent storage is stopped")
	// ErrFileSystem is returned for a save folder on a file system the pinned engine cannot hash on.
	ErrFileSystem = errors.New("the engine cannot verify pieces on this file system")
	// ErrNotStopped is returned by Seal before Stop: while operations can still change files, no
	// record of them can be exact.
	ErrNotStopped = errors.New("the torrent storage is not stopped")
)

// operation kinds: only a mutation after Stop makes the session unclean; a refused read changes nothing.
const (
	read = iota
	mutate
)

// TorrentStorage is the storage of one torrent: the engine's file storage inside a guard. It
// implements storage.ClientImpl and has no Close, because the engine never closes a storage it was
// given; the owner calls Stop.
//
// Before every operation the guard checks that the save folder is still the one recorded at the
// start (device and inode), and refuses otherwise, without calling the engine. It records every
// failure in the session, counts piece reads, and, after Stop, refuses new operations and waits for
// those in flight.
type TorrentStorage struct {
	inner   storage.ClientImpl
	root    string
	rootDev uint64
	rootIno uint64
	session *Session
	reads   atomic.Int64
	lost    atomic.Pointer[error]

	mu      sync.RWMutex
	stopped bool
	flight  sync.WaitGroup

	trackMu sync.Mutex
	tracks  []fileTrack                // the torrent's files, in the engine's order
	index   segments.Index             // which files a byte range of the torrent falls in
	left    map[int]files.FileState    // file index → the file as this run last left it
	base    map[string]files.FileState // path → the file as an earlier run left it, trusted and kept
}

// fileTrack is one file of the torrent: where it is and which pieces it covers.
type fileTrack struct {
	path       string
	first, end int
}

var _ storage.ClientImpl = (*TorrentStorage)(nil)

// unsupportedFileSystems are the file systems on which the engine's classic file IO cannot hash a
// piece: it seeks with SEEK_DATA, which HFS+ does not support, so every piece fails its check and the
// download never completes (finding 21). APFS, exFAT and FAT work.
var unsupportedFileSystems = []string{"hfs"}

// newTorrentStorage guards inner for the save folder root, which must be an existing, clean,
// absolute folder.
func newTorrentStorage(inner storage.ClientImpl, root string, session *Session) (*TorrentStorage, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, fmt.Errorf("the save folder %q is not a clean absolute path", root)
	}
	info, err := os.Stat(root)
	var fsType string
	if err == nil {
		fsType, err = files.FileSystem(root)
	}
	if err != nil {
		return nil, fmt.Errorf("the save folder: %w", err)
	}
	if slices.Contains(unsupportedFileSystems, fsType) {
		return nil, fmt.Errorf("%w: %s (task 009, finding 21)", ErrFileSystem, fsType)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("the save folder %q is not a folder", root)
	}
	st := info.Sys().(*syscall.Stat_t)
	return &TorrentStorage{
		inner: inner, root: root, rootDev: uint64(st.Dev), rootIno: st.Ino, session: session,
		left: map[int]files.FileState{}, base: map[string]files.FileState{},
	}, nil
}

// enter admits one operation: not after Stop, and only while the save folder is the recorded one.
// The returned function ends the operation.
func (s *TorrentStorage) enter(kind int) (func(), error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stopped {
		if kind == mutate {
			s.session.Fail(ErrStopped)
		}
		return nil, ErrStopped
	}
	if err := s.checkRoot(); err != nil {
		return nil, err
	}
	s.flight.Add(1)
	return s.flight.Done, nil
}

// checkRoot reports whether the save folder is still the recorded one; a loss is permanent and
// recorded in the session once.
func (s *TorrentStorage) checkRoot() error {
	if lost := s.lost.Load(); lost != nil {
		return *lost
	}
	info, err := os.Stat(s.root)
	if err == nil {
		st := info.Sys().(*syscall.Stat_t)
		if uint64(st.Dev) == s.rootDev && st.Ino == s.rootIno {
			return nil
		}
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
// engine client is closed and before the payload is synced and fingerprinted: from then on no
// operation changes a file. It may be called again.
func (s *TorrentStorage) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	idle := make(chan struct{})
	go func() { s.flight.Wait(); close(idle) }()
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("operations of the torrent storage are still running: %w", ctx.Err())
	}
}

// OpenTorrent opens the engine's storage for the torrent, guarded. The engine calls it more than
// once per added torrent. A storage that offers more than Piece and Close is refused: the guard
// could not cover what it does not wrap.
func (s *TorrentStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (storage.TorrentImpl, error) {
	done, err := s.enter(mutate)
	if err != nil {
		return storage.TorrentImpl{}, err
	}
	defer done()
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
	if err := s.track(info); err != nil {
		return storage.TorrentImpl{}, errors.Join(err, closeImpl())
	}
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl {
			return &guardedPiece{s: s, inner: impl.Piece(p), offset: p.Offset(), length: p.Length()}
		},
		Close: closeImpl,
	}, nil
}

// track learns the torrent's files from its info, once.
func (s *TorrentStorage) track(info *metainfo.Info) error {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	if s.tracks != nil {
		return nil
	}
	_, paths, err := Layout(s.root, info)
	if err != nil {
		return err
	}
	tracks := make([]fileTrack, len(paths))
	for i, f := range info.UpvertedFiles() {
		tracks[i] = fileTrack{paths[i], f.BeginPieceIndex(info.PieceLength), f.EndPieceIndex(info.PieceLength)}
	}
	s.tracks, s.index = tracks, info.FileSegmentsIndex()
	return nil
}

// touched records the files that the byte range [start, start+length) of the torrent falls in as the
// run now leaves them: a write, or the verification of a piece, is the last thing this run did to
// them. An outside edit made later changes the fingerprint and shows at the next start.
func (s *TorrentStorage) touched(start, length int64) {
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	for i := range s.index.LocateIter(segments.Extent{Start: start, Length: length}) {
		fp, err := files.Stat(s.tracks[i].path)
		if err != nil {
			delete(s.left, i)
			continue
		}
		s.left[i] = files.FileState{Path: s.tracks[i].path, Present: true, Fingerprint: fp,
			FirstPiece: s.tracks[i].first, EndPiece: s.tracks[i].end}
	}
}

// Baseline keeps the state an earlier run left for the files that decision trusts, so that a file
// this run never touches is still described at the next start as the earlier run left it, and an
// outside edit made in between is not taken for the run's own work. Call it before the torrent is added.
func (s *TorrentStorage) Baseline(rec *files.Record, decision files.Decision) {
	if rec == nil {
		return
	}
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	for _, f := range rec.Files {
		if slices.Contains(decision.Trusted, f.Path) {
			s.base[f.Path] = f
		}
	}
}

// Seal syncs every payload file this run wrote and returns the record the next start decides by.
// Call it after Stop and after the marks are closed. A file that cannot be synced makes the run
// unclean. The record is Clean only if the whole session had no storage problem.
func (s *TorrentStorage) Seal() (files.Record, error) {
	s.mu.RLock()
	stopped := s.stopped
	s.mu.RUnlock()
	if !stopped {
		return files.Record{}, ErrNotStopped
	}
	s.trackMu.Lock()
	defer s.trackMu.Unlock()
	var errs []error
	fail := func(err error) {
		errs = append(errs, err)
		s.session.Fail(err)
	}
	var rec files.Record
	if root, err := files.Stat(s.root); err != nil {
		fail(fmt.Errorf("the save folder: %w", err))
	} else {
		rec.Dev = root.Dev
	}
	var err error
	if rec.FileSystem, err = files.FileSystem(s.root); err != nil {
		fail(fmt.Errorf("the file system of the save folder: %w", err))
	}
	for i, t := range s.tracks {
		state := files.FileState{Path: t.path, FirstPiece: t.first, EndPiece: t.end}
		if left, ok := s.left[i]; ok {
			if err := syncFile(t.path); err != nil {
				fail(err)
			} else {
				state = left
			}
		} else if base, ok := s.base[t.path]; ok {
			state = base
		}
		rec.Files = append(rec.Files, state)
	}
	rec.Clean = s.session.Clean()
	return rec, errors.Join(errs...)
}

// syncFile flushes a file to stable storage (F_FULLFSYNC on macOS).
func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
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
	done, err := p.s.enter(read)
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
	done, err := p.s.enter(read)
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
	done, err := p.s.enter(mutate)
	if err != nil {
		return 0, err
	}
	defer done()
	n, err := p.inner.WriteAt(b, off)
	p.s.touched(p.offset+off, int64(len(b)))
	return n, p.s.note(err)
}

func (p *guardedPiece) MarkComplete() error {
	done, err := p.s.enter(mutate)
	if err != nil {
		return err
	}
	defer done()
	err = p.s.note(p.inner.MarkComplete())
	p.s.touched(p.offset, p.length)
	return err
}

func (p *guardedPiece) MarkNotComplete() error {
	done, err := p.s.enter(mutate)
	if err != nil {
		return err
	}
	defer done()
	return p.s.note(p.inner.MarkNotComplete())
}

// Completion does not reach the engine's storage once the folder is gone: it would mark the pieces
// of every file it cannot find as incomplete, in our marks.
func (p *guardedPiece) Completion() storage.Completion {
	done, err := p.s.enter(read)
	if err != nil {
		return storage.Completion{Err: err}
	}
	defer done()
	c := p.inner.Completion()
	p.s.note(c.Err)
	return c
}
