package filestore

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/library"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// slowRate keeps a 1 MiB transfer running for about 4 seconds, long enough to act in the middle of it.
const slowRate = 256 << 10

// completePieces lists the pieces the engine has verified.
func completePieces(tor *torrent.Torrent) []int {
	var out []int
	for i := range tor.NumPieces() {
		if tor.PieceState(i).Complete {
			out = append(out, i)
		}
	}
	return out
}

// waitPieces waits until at least n pieces are complete.
func waitPieces(t *testing.T, tor *torrent.Torrent, n int) {
	t.Helper()
	ctx := deadline(t, transferLimit)
	for len(completePieces(tor)) < n {
		select {
		case <-ctx.Done():
			t.Fatalf("only %d pieces complete, want %d", len(completePieces(tor)), n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// payloadMoved reports whether the leecher received payload during the window.
func payloadMoved(tor *torrent.Torrent, window time.Duration) bool {
	before := swarm.Count(tor).PayloadRead
	time.Sleep(window)
	return swarm.Count(tor).PayloadRead != before
}

// storedMarks reads the marks of the torrent from the data folder, as a later run would.
func storedMarks(t *testing.T, dataDir string, mi *metainfo.MetaInfo) map[int]bool {
	t.Helper()
	store, err := library.OpenCompletion(deadline(t, closeLimit), filepath.Join(dataDir, CompletionFile))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	marks, err := store.LoadMarks(deadline(t, closeLimit), mi.HashInfoBytes())
	if err != nil {
		t.Fatal(err)
	}
	return marks
}

func TestASaveFolderThatVanishesMidTransferIsNotRecreatedAndTheMarksSurvive(t *testing.T) {
	for name, vanish := range map[string]func(t *testing.T, root string){
		"deleted": func(t *testing.T, root string) {
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
		},
		"replaced by another folder": func(t *testing.T, root string) {
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			mustMkdir(t, root)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, "v1-large")
			seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
			saveRoot := filepath.Join(t.TempDir(), "Downloads")
			mustMkdir(t, saveRoot)
			dataDir := t.TempDir()
			r := start(t, mi, saveRoot, dataDir, runOptions{})
			if r.addErr != nil {
				t.Fatal(r.addErr)
			}
			r.connect(seed)
			waitPieces(t, r.torrent, 8)

			vanish(t, saveRoot)

			eventually(t, 5*time.Second, "the loss of the save folder was not noticed", func() bool { return !r.session.Clean() })
			time.Sleep(drainWait)
			if payloadMoved(r.torrent, time.Second) {
				t.Error("the transfer goes on after the save folder is gone")
			}
			entries, err := os.ReadDir(saveRoot)
			if name == "deleted" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Errorf("the deleted save folder exists again: %v %v", entries, err)
				}
			} else if err != nil || len(entries) != 0 {
				t.Errorf("the replacement folder holds %v (%v), want it empty", entries, err)
			}
			kept := completePieces(r.torrent)
			assertMarksKept(t, r, mi, kept)
			assertRootLost(t, r)
			marks := storedMarks(t, dataDir, mi)
			for _, piece := range kept {
				if !marks[piece] {
					t.Errorf("piece %d is not marked complete in the store", piece)
				}
			}
		})
	}
}

func TestClosingTheRunMidTransferNeverLeavesAMarkWithoutItsBytes(t *testing.T) {
	fx, mi := named(t, "v1-large")
	info := infoOf(t, mi)
	for attempt := range 3 {
		saveRoot, dataDir := t.TempDir(), t.TempDir()
		seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
		r := start(t, mi, saveRoot, dataDir, runOptions{})
		if r.addErr != nil {
			t.Fatal(r.addErr)
		}
		r.connect(seed)
		waitPieces(t, r.torrent, 3+attempt*4)
		// close while chunks keep arriving
		problems := r.quit()
		for _, p := range problems {
			if !errors.Is(p, ErrStopped) && !errors.Is(p, ErrClosed) {
				t.Errorf("attempt %d: unexpected problem %v", attempt, p)
			}
		}
		bad := badPieces(t, info, saveRoot)
		marks := storedMarks(t, dataDir, mi)
		kept := 0
		for piece, complete := range marks {
			if !complete {
				continue
			}
			kept++
			if slices.Contains(bad, piece) {
				t.Errorf("attempt %d: piece %d is marked complete but its bytes are not on disk", attempt, piece)
			}
		}
		if kept < 3 {
			t.Errorf("attempt %d: only %d marks survived, want at least the 3 pieces seen complete", attempt, kept)
		}
	}
}
