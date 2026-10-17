package filestore

import (
	"errors"
	"fmt"
	"sync"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/segments"

	"octopustorrent/internal/files"
)

// tracker remembers, for the torrent's files, what the run last left on disk, so that the next start
// can tell the run's own work from an outside edit.
//
// A file is "as the run left it" after each of the run's own writes and after the engine verified a
// piece. Before a write, the tracker compares the file with that state: if the file changed in between
// (and no other write to it is running, which would explain the change), someone else edited it, and the
// file is tainted: it is not recorded as present, so the next start verifies it. The comparison cannot be
// made while writes to the same file overlap, nor for a file the run has not touched yet and the earlier
// run did not leave trusted.
type tracker struct {
	mu       sync.Mutex
	bound    bool
	infoHash metainfo.Hash
	ranges   []files.FileRange          // the torrent's files, in the engine's order
	index    segments.Index             // which files a byte range of the torrent falls in
	left     map[int]files.FileState    // file index → the file as this run last left it
	writers  map[int]int                // file index → writes in progress
	tainted  map[int]bool               // file index → changed by someone else during the run
	base     map[string]files.FileState // path → the file as an earlier run left it, trusted or missing
}

func newTracker() *tracker {
	return &tracker{
		left: map[int]files.FileState{}, writers: map[int]int{}, tainted: map[int]bool{}, base: map[string]files.FileState{},
	}
}

// learn binds the tracker to the torrent. A tracker serves one torrent: another infohash is refused.
func (t *tracker) learn(ranges []files.FileRange, info *metainfo.Info, infoHash metainfo.Hash) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.bound {
		if t.infoHash != infoHash {
			return errors.New("a torrent storage serves one torrent, and this is another")
		}
		return nil
	}
	t.bound, t.infoHash, t.ranges, t.index = true, infoHash, ranges, info.FileSegmentsIndex()
	return nil
}

// reference is the state a file should be in before the run writes to it again, if the run knows it.
func (t *tracker) reference(i int) (files.FileState, bool) {
	if left, ok := t.left[i]; ok {
		return left, true
	}
	base, ok := t.base[t.ranges[i].Path]
	return base, ok && base.Present
}

// beforeWrite checks the files the byte range falls in against what the run left, and counts the write.
func (t *tracker) beforeWrite(start, length int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.index.LocateIter(segments.Extent{Start: start, Length: length}) {
		if t.writers[i] == 0 {
			if ref, ok := t.reference(i); ok {
				if cur, err := files.Stat(t.ranges[i].Path); err != nil || cur != ref.Fingerprint {
					t.tainted[i] = true
				}
			}
		}
		t.writers[i]++
	}
}

// afterWrite records the files the byte range falls in as the run now leaves them.
func (t *tracker) afterWrite(start, length int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.index.LocateIter(segments.Extent{Start: start, Length: length}) {
		t.writers[i]--
		t.leave(i)
	}
}

// verified records the files a verified piece falls in as the run now leaves them.
func (t *tracker) verified(start, length int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.index.LocateIter(segments.Extent{Start: start, Length: length}) {
		t.leave(i)
	}
}

// leave fingerprints a file as the run leaves it. t.mu is held.
func (t *tracker) leave(i int) {
	fp, err := files.Stat(t.ranges[i].Path)
	if err != nil {
		delete(t.left, i)
		return
	}
	t.left[i] = files.FileState{Path: t.ranges[i].Path, Present: true, Fingerprint: fp}
}

// baseline keeps the state an earlier run left for the files the decision trusts, and for the files it
// reports missing, so that a file this run never touches is described at the next start as the earlier
// run left it: an outside edit made in between, or a file still missing, is not taken for the run's work.
func (t *tracker) baseline(rec *files.Record, decision files.Decision) {
	if rec == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, f := range rec.Files {
		for _, kept := range [][]string{decision.Trusted, decision.Missing} {
			for _, path := range kept {
				if path == f.Path {
					t.base[f.Path] = f
				}
			}
		}
	}
}

// seal builds the record. A file is recorded as present only if the run left it (and synced it), it was
// not changed by someone else, and none of its pieces is still waiting to be verified.
func (t *tracker) seal(root string, session *Session, unresolved map[int]bool, sync func(string) error) (files.Record, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var errs []error
	fail := func(err error) {
		errs = append(errs, err)
		session.Fail(err)
	}
	var rec files.Record
	if id, err := files.IdentityOf(root); err != nil {
		fail(fmt.Errorf("the save folder: %w", err))
	} else {
		rec.Dev = id.Dev
	}
	var err error
	if rec.FileSystem, err = files.FileSystem(root); err != nil {
		fail(fmt.Errorf("the file system of the save folder: %w", err))
	}
	for i, r := range t.ranges {
		state := files.FileState{Path: r.Path}
		waiting := false
		for p := r.First; p < r.End; p++ {
			waiting = waiting || unresolved[p]
		}
		if !t.tainted[i] && !waiting {
			if left, ok := t.left[i]; ok {
				if err := sync(r.Path); err != nil {
					fail(err)
				} else {
					state = left
				}
			} else if base, ok := t.base[r.Path]; ok {
				state = base
			}
		}
		rec.Files = append(rec.Files, state)
	}
	rec.Clean = session.Clean()
	return rec, errors.Join(errs...)
}

// boundHash is the infohash the tracker serves, as the marks know it.
func (t *tracker) boundHash() [20]byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.infoHash
}
