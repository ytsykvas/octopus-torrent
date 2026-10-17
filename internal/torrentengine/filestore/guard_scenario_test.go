package filestore

import (
	"crypto/sha1"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

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

// payloadAfter reports whether the leecher received payload during the window.
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

// hashesRight reports whether the bytes of a piece in a single-file torrent's file match the
// torrent's hash of it.
func hashesRight(t *testing.T, info *metainfo.Info, file string, piece int) bool {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	p := info.Piece(piece)
	if p.Offset()+p.Length() > int64(len(data)) {
		return false
	}
	sum := sha1.Sum(data[p.Offset() : p.Offset()+p.Length()])
	return metainfo.Hash(sum) == p.V1Hash().Value
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
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, "v1-large")
			seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
			saveRoot := filepath.Join(t.TempDir(), "Downloads")
			if err := os.Mkdir(saveRoot, 0o755); err != nil {
				t.Fatal(err)
			}
			dataDir := t.TempDir()
			r := start(t, mi, saveRoot, dataDir, runOptions{})
			if r.addErr != nil {
				t.Fatal(r.addErr)
			}
			r.connect(seed)
			waitPieces(t, r.torrent, 8)

			vanish(t, saveRoot)

			deadline := time.Now().Add(5 * time.Second)
			for r.session.Clean() {
				if time.Now().After(deadline) {
					t.Fatal("the loss of the save folder was not noticed")
				}
				time.Sleep(10 * time.Millisecond)
			}
			// the in-flight chunks drain, then the engine has stopped asking for data
			time.Sleep(300 * time.Millisecond)
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
			for _, piece := range kept {
				if c, err := r.completion.Get(metainfo.PieceKey{InfoHash: mi.HashInfoBytes(), Index: piece}); err != nil || !c.Ok || !c.Complete {
					t.Errorf("the mark of piece %d was lost: %+v %v", piece, c, err)
				}
			}
			problems, endErr := r.end()
			if len(problems) == 0 || !errors.Is(problems[0], ErrRootChanged) {
				t.Fatalf("problems %v, want ErrRootChanged first", problems)
			}
			if r.record.Clean {
				t.Error("the record of a run that lost its save folder is clean")
			}
			if endErr == nil {
				t.Error("Seal did not report the lost save folder")
			}
			marks := storedMarks(t, dataDir, mi)
			for _, piece := range kept {
				if !marks[piece] {
					t.Errorf("piece %d is not marked complete in the store", piece)
				}
			}
		})
	}
}

// Engine fact (finding 18): without the guard, classic IO recreates a deleted save folder and goes on
// writing the payload there. For a detached volume that folder is the mount point, on the disk of
// the application.
func TestWithoutTheGuardTheEngineRecreatesADeletedSaveFolder(t *testing.T) {
	fx, mi := named(t, "v1-large")
	seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
	saveRoot := filepath.Join(t.TempDir(), "Downloads")
	if err := os.Mkdir(saveRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	plain := plainStorage(saveRoot, storage.NewMapPieceCompletion())
	p, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: Refusing(), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(deadline(t, closeLimit)); _ = plain.Close() })
	tor, _ := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: plain})
	if err := tor.SetInfoBytes(mi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	tor.DownloadAll()
	connectPlain(t, tor, seed)
	waitPieces(t, tor, 8)
	if err := os.RemoveAll(saveRoot); err != nil {
		t.Fatal(err)
	}
	ctx := deadline(t, transferLimit)
	for {
		if _, err := os.Stat(fx.FilePath(saveRoot, fx.Files[0])); err == nil {
			return // the folder and the file are back: the engine wrote the payload on
		}
		select {
		case <-ctx.Done():
			t.Fatal("the engine did not recreate the deleted save folder; the guard may no longer be needed (finding 18)")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestClosingTheRunMidTransferNeverLeavesAMarkWithoutItsBytes(t *testing.T) {
	fx, mi := named(t, "v1-large")
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
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
		file := fx.FilePath(saveRoot, fx.Files[0])
		marks := storedMarks(t, dataDir, mi)
		kept := 0
		for piece, complete := range marks {
			if !complete {
				continue
			}
			kept++
			if !hashesRight(t, &info, file, piece) {
				t.Errorf("attempt %d: piece %d is marked complete but its bytes are not on disk", attempt, piece)
			}
		}
		if kept < 3 {
			t.Errorf("attempt %d: only %d marks survived, want at least the 3 pieces seen complete", attempt, kept)
		}
	}
}
