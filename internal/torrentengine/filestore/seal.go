package filestore

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"octopustorrent/internal/files"
)

// Baseline keeps the state an earlier run left for the files decision trusts or reports missing. Call
// it before the torrent is added.
func (s *TorrentStorage) Baseline(rec *files.Record, decision files.Decision) {
	s.tr.baseline(rec, decision)
}

// Seal syncs every payload file this run wrote and returns the record the next start decides by.
// Call it after Stop has drained the operations and after the marks are closed; otherwise it returns
// ErrNotStopped or ErrNotClosed. A file that cannot be synced makes the run unclean. A file is recorded
// as present only if no one else changed it during the run and none of its pieces is still waiting to
// be verified: the stale marks of such pieces must not be trusted at the next start. The record is Clean
// only if the whole session had no storage problem.
func (s *TorrentStorage) Seal() (files.Record, error) {
	select {
	case <-s.idle:
	default:
		return files.Record{}, ErrNotStopped
	}
	if !s.marks.Closed() {
		return files.Record{}, ErrNotClosed
	}
	unresolved := map[int]bool{}
	for _, p := range s.marks.Unresolved(s.tr.boundHash()) {
		unresolved[p] = true
	}
	return s.tr.seal(s.root, s.session, unresolved)
}

// syncFile flushes a regular file to stable storage (F_FULLFSYNC on macOS). It does not block on a
// path that was replaced by a pipe.
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is no regular file", path)
	}
	if err != nil {
		return errors.Join(err, f.Close())
	}
	return errors.Join(f.Sync(), f.Close())
}
