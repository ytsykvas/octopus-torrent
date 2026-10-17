package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/files"
)

func TestSealRefusesWhileOperationsAreStillRunningAndAcceptsOnceTheyHaveFinished(t *testing.T) {
	s, pieces, session, root, f := sealed(t)
	put(t, filepath.Join(root, "m", "a"), "aaaaaaaaaaaaaaaaaaaa")
	f.piece.entered, f.piece.release = make(chan struct{}, 1), make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := pieces[0].WriteAt([]byte("x"), 0); done <- err }()
	<-f.piece.entered

	short, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Stop(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v", err)
	}
	if session.Clean() {
		t.Fatal("a Stop that gave up left the session clean")
	}
	if _, err := s.Seal(); !errors.Is(err, ErrNotStopped) {
		t.Fatalf("Seal while a write runs = %v, want ErrNotStopped", err)
	}
	close(f.piece.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after the write = %v", err)
	}
	if rec, err := s.Seal(); err != nil || rec.Clean {
		t.Fatalf("Seal = %+v, %v; want a record that is not clean, because Stop had given up", rec, err)
	}
}

func TestSealWaitsForTheMarksToBeClosedAndKeepsFilesWithUnverifiedPiecesOutOfTheRecord(t *testing.T) {
	f := newFake()
	root, session := t.TempDir(), &Session{}
	c := NewCompletion(newDouble(), session, slowFlush)
	s, err := newGuard(f, root, session, c)
	if err != nil {
		t.Fatal(err)
	}
	impl, err := s.OpenTorrent(context.Background(), multiInfo, metainfo.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	piece0 := impl.Piece(multiInfo.Piece(0))
	a, b := filepath.Join(root, "m", "a"), filepath.Join(root, "m", "b")
	mustMkdir(t, filepath.Join(root, "m"))
	mustWrite(t, a, "aaaaaaaaaaaaaaaaaaaa")
	mustWrite(t, b, "bbbbbbbbbbbbbbbbbbbb")

	// The engine is to verify all three pieces; it has verified piece 0 and is stopped before the rest.
	c.Distrust([20]byte{}, []int{0, 1, 2})
	if err := piece0.MarkComplete(); err != nil {
		t.Fatal(err)
	}
	if err := c.Set(metainfo.PieceKey{InfoHash: metainfo.Hash{}, Index: 0}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seal(); !errors.Is(err, ErrNotClosed) {
		t.Fatalf("Seal with the marks open = %v, want ErrNotClosed", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{a, b} {
		if fileState(t, rec, path).Present {
			t.Errorf("%s is recorded as present while pieces of it are still unverified: the next start would trust their stale marks", path)
		}
	}
	// the next start verifies both files
	ranges, _ := Ranges(root, multiInfo)
	d := files.Decide(&rec, root, ranges, 3, files.System{})
	if !slices.Equal(d.Distrusted(3), []int{0, 1, 2}) || len(d.Trusted) != 0 {
		t.Fatalf("Decide = %+v, want every piece verified", d)
	}
}

func TestOnceEveryDistrustedPieceIsVerifiedTheFilesAreRecorded(t *testing.T) {
	f := newFake()
	root, session := t.TempDir(), &Session{}
	c := NewCompletion(newDouble(), session, slowFlush)
	s, _ := newGuard(f, root, session, c)
	impl, _ := s.OpenTorrent(context.Background(), multiInfo, metainfo.Hash{})
	mustMkdir(t, filepath.Join(root, "m"))
	mustWrite(t, filepath.Join(root, "m", "a"), "aaaaaaaaaaaaaaaaaaaa")
	c.Distrust([20]byte{}, []int{0, 1})
	for i := range 2 {
		if err := impl.Piece(multiInfo.Piece(i)).MarkComplete(); err != nil {
			t.Fatal(err)
		}
		if err := c.Set(metainfo.PieceKey{Index: i}, true); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.Stop(context.Background())
	_ = c.Close()
	rec, err := s.Seal()
	if err != nil || !fileState(t, rec, filepath.Join(root, "m", "a")).Present {
		t.Fatalf("Seal = %+v, %v; want a recorded as present", rec, err)
	}
}

func TestAFileChangedByAnotherProgramBetweenTheRunsOwnWritesIsNotRecorded(t *testing.T) {
	s, pieces, _, root, _ := sealed(t)
	a := filepath.Join(root, "m", "a")
	put(t, a, "aaaaaaaaaaaaaaaaaaaa")
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	put(t, a, "aaaaaaaaaaaaaaaaaaa!") // someone else, while the run is going on
	if _, err := pieces[0].WriteAt([]byte("y"), 1); err != nil {
		t.Fatal(err)
	}
	_ = s.Stop(context.Background())
	rec, err := s.Seal()
	if err != nil || fileState(t, rec, a).Present {
		t.Fatalf("Seal = %+v, %v; want a not recorded: it changed under the run, and its pieces cannot be told from the run's", rec, err)
	}
}

func TestOverlappingWritesDoNotMakeTheRunsOwnChangesLookForeign(t *testing.T) {
	s, pieces, _, root, f := sealed(t)
	a := filepath.Join(root, "m", "a")
	put(t, a, "aaaaaaaaaaaaaaaaaaaa")
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil { // the run has now left a
		t.Fatal(err)
	}
	f.piece.entered, f.piece.release = make(chan struct{}, 2), make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = pieces[0].WriteAt([]byte("z"), 2) }()
		<-f.piece.entered
		put(t, a, "aaaaaaaaaaaaaaaaaaa?") // this change belongs to the write in flight, as far as anyone can tell
	}
	close(f.piece.release)
	wg.Wait()
	_ = s.Stop(context.Background())
	rec, err := s.Seal()
	if err != nil || !fileState(t, rec, a).Present {
		t.Fatalf("Seal = %+v, %v; want a recorded: the second write could not know whose change it saw", rec, err)
	}
}

func TestAFileThatIsStillMissingStaysMissingInTheNextRecord(t *testing.T) {
	s, _, _, root, _ := sealed(t)
	b := filepath.Join(root, "m", "b")
	earlier := &files.Record{Clean: true, Files: []files.FileState{{Path: b, Present: true, Fingerprint: files.Fingerprint{Size: 20}}}}
	s.Baseline(earlier, files.Decision{Missing: []string{b}})
	_ = s.Stop(context.Background())
	rec, err := s.Seal()
	if err != nil || !fileState(t, rec, b).Present {
		t.Fatalf("Seal = %+v, %v; want b still recorded as present, so the next start reports it missing again", rec, err)
	}
}

func TestAStorageServesOneTorrentOnly(t *testing.T) {
	s, _, _, _, _ := sealed(t)
	other := metainfo.Hash{1}
	if _, err := s.OpenTorrent(context.Background(), multiInfo, other); err == nil {
		t.Fatal("a second torrent was opened in a storage made for the first")
	}
	if _, err := s.OpenTorrent(context.Background(), multiInfo, metainfo.Hash{}); err != nil {
		t.Fatalf("the same torrent again (the engine does that) = %v", err)
	}
}

func TestTwoVolumesWithTheSameDeviceAndInodeAreToldApartByTheirCreationTime(t *testing.T) {
	s, pieces, session, _, f := sealed(t)
	before := len(f.callList())
	s.rootID.Birth++ // what a different volume mounted at the same place looks like
	if _, err := pieces[0].WriteAt([]byte("x"), 0); !errors.Is(err, ErrRootChanged) {
		t.Fatalf("WriteAt = %v, want ErrRootChanged", err)
	}
	if len(f.callList()) != before || session.Clean() {
		t.Fatal("the engine was reached, or the swap was not recorded")
	}
}

func TestNoOperationReachesTheEngineOnceStopHasReturned(t *testing.T) {
	s, pieces, _, _, f := sealed(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = pieces[0].WriteAt([]byte("x"), 0)
					_ = pieces[1].MarkComplete()
				}
			}
		}()
	}
	time.Sleep(20 * time.Millisecond)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	reached := len(f.callList())
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	if after := len(f.callList()); after != reached {
		t.Fatalf("%d calls reached the engine after Stop had returned", after-reached)
	}
}

func TestAPathReplacedByAPipeIsNotSyncedAndMakesTheRunUnclean(t *testing.T) {
	s, pieces, session, root, _ := sealed(t)
	a := filepath.Join(root, "m", "a")
	put(t, a, "aaaaaaaaaaaaaaaaaaaa")
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(a, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = s.Stop(context.Background())
	rec, err := s.Seal()
	if err == nil || rec.Clean || session.Clean() || fileState(t, rec, a).Present {
		t.Fatalf("Seal = %+v, %v; want an error for the pipe and an unclean record", rec, err)
	}
}

func TestATorrentNamedAfterTheSaveFolderItselfIsRefused(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".", "a/..", ""} {
		info := &metainfo.Info{Name: name, Files: []metainfo.FileInfo{{Length: 1, Path: []string{"f"}}}, PieceLength: 16, Pieces: make([]byte, 20)}
		if _, _, err := Layout(root, info); err == nil {
			t.Errorf("a torrent named %q was accepted: its folder would be the save folder", name)
		}
	}
	nameless := &metainfo.Info{Name: metainfo.NoName, Files: []metainfo.FileInfo{{Length: 1, Path: []string{"f"}}}, PieceLength: 16, Pieces: make([]byte, 20)}
	if content, _, err := Layout(root, nameless); err != nil || content != root {
		t.Fatalf("a torrent without a name = %q, %v; want the save folder itself as its content root", content, err)
	}
}

func TestAFolderThatCannotBeSearchedMakesTheTorrentUnopenableNotUnchecked(t *testing.T) {
	f := newFake()
	root := t.TempDir()
	locked := filepath.Join(root, "m")
	mustMkdir(t, filepath.Join(locked, "x"))
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	s, err := newGuard(f, root, &Session{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenTorrent(context.Background(), multiInfo, metainfo.Hash{}); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("OpenTorrent = %v, want the permission error: a path that cannot be looked at is not safe", err)
	}
	if calls := f.callList(); len(calls) != 0 {
		t.Fatalf("the engine's storage was reached: %v", calls)
	}
}
