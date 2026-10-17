package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"octopustorrent/internal/library"
	"octopustorrent/internal/testutil/volume"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// refusingMarks is a test double for the marks store, and it is named as one: it reads nothing and
// refuses every write, as a store on a full disk does.
type refusingMarks struct{}

var errDiskFull = errors.New("the disk holding the marks is full")

func (refusingMarks) LoadMarks(context.Context, [20]byte) (map[int]bool, error) {
	return map[int]bool{}, nil
}
func (refusingMarks) SaveMarks(context.Context, []library.Mark) error { return errDiskFull }
func (refusingMarks) Close() error                                    { return nil }

func TestRefusedMarkWritesAreReportedWhileTheEngineGoesOnAndTheNextStartVerifies(t *testing.T) {
	fx, mi := named(t, "v1-large")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	lib := libraryIn(t, dataDir)

	first := start(t, mi, saveRoot, dataDir, runOptions{marks: refusingMarks{}, completion: Options{FlushRows: 4}})
	first.download(seed) // the engine only logs a failed mark and completes the torrent
	eventually(t, transferLimit, "the refused write was never reported", func() bool { return first.completion.Err() != nil })
	if !errors.Is(first.completion.Err(), errDiskFull) {
		t.Fatalf("Err = %v", first.completion.Err())
	}
	if !strings.Contains(first.peer.Logs(), "error marking piece complete") {
		t.Error("the engine did not log the refused mark: the contrast with its silence is gone")
	}
	problems, err := first.end()
	if !errors.Is(err, errDiskFull) || len(problems) == 0 || first.record.Clean {
		t.Fatalf("end = %v, %v, clean %v; want the refused write reported and an unclean record", problems, err, first.record.Clean)
	}
	assertPayload(t, fx, saveRoot)

	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
	if !second.decision.VerifyAll {
		t.Fatalf("decision %+v, want everything verified", second.decision)
	}
	settle(t, second.torrent)
	n := second.torrent.NumPieces()
	if got := completePieces(second.torrent); !slices.Equal(got, all(n)) || second.storage.Reads() != int64(n) {
		t.Fatalf("complete %d of %d pieces with %d reads, want all verified by reading", len(got), n, second.storage.Reads())
	}
	second.connect(seed)
	time.Sleep(idleWindow)
	if payload := swarm.Count(second.torrent).PayloadRead; payload != 0 {
		t.Fatalf("%d payload bytes downloaded again", payload)
	}
	second.quitCleanly()
	lib.intact()
}

func TestAMarksFolderOnAFullVolumeIsAnErrorBeforeAnythingIsWritten(t *testing.T) {
	vol := volume.New(t, volume.Options{Size: "8m", FS: "APFS", Name: "OctoFull"})
	lib := libraryIn(t, vol.Mount)
	filler := volume.Fill(t, vol.Mount)
	saveRoot := t.TempDir()
	var session Session
	c, err := OpenCompletion(deadline(t, closeLimit), vol.Mount, &session, Options{})
	if err == nil || c != nil || !errors.Is(err, library.ErrCannotOpen) && !errors.Is(err, library.ErrFull) {
		t.Fatalf("OpenCompletion on a full volume = %v, %v; want an explicit error and no store", c, err)
	}
	if got := listFiles(t, saveRoot); len(got) != 0 {
		t.Fatalf("the payload folder holds %v", got)
	}
	lib.unchanged() // the library on the full volume was not touched by the failed attempt
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	lib.intact()
}

// onVolume makes a run on a new volume: the payload goes into <volume>/Downloads and the marks into a
// data folder on the internal disk.
func onVolume(t *testing.T, o volume.Options) (vol *volume.Volume, saveRoot, dataDir string) {
	t.Helper()
	vol = volume.New(t, o)
	saveRoot = filepath.Join(vol.Mount, "Downloads")
	mustMkdir(t, saveRoot)
	return vol, saveRoot, t.TempDir()
}

func TestAVolumePulledWhileLeechingLeavesNothingOnTheInternalDiskAndLosesNothingValid(t *testing.T) {
	fx, mi := named(t, "v1-large")
	seed := seeder(t, fx, mi, swarm.Options{RateLimit: slowRate})
	vol, saveRoot, dataDir := onVolume(t, volume.Options{Size: "32m", FS: "APFS", Name: "OctoLeech"})
	lib := libraryIn(t, dataDir)

	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.connect(seed)
	waitPieces(t, first.torrent, 8)
	have := completePieces(first.torrent)

	vol.Detach()
	eventually(t, 5*time.Second, "the pulled volume was not noticed", func() bool { return !first.session.Clean() })
	time.Sleep(drainWait)
	if entries, err := os.ReadDir(vol.Mount); err != nil || len(entries) != 0 {
		t.Fatalf("the mount point on the internal disk holds %v (%v): the engine wrote beside the pulled volume", entries, err)
	}
	assertMarksKept(t, first, mi, have)
	assertRootLost(t, first)
	if got := storedMarks(t, dataDir, mi); len(got) < len(have) {
		t.Fatalf("%d marks survived in the store, want at least the %d seen complete", len(got), len(have))
	}

	vol.Attach()
	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record})
	if !second.decision.VerifyAll {
		t.Fatalf("decision %+v, want everything verified after an unclean run", second.decision)
	}
	settle(t, second.torrent)
	got := completePieces(second.torrent)
	for _, p := range have {
		if !slices.Contains(got, p) {
			t.Errorf("piece %d was complete before the volume was pulled and is not after it came back", p)
		}
	}
	second.download(seed)
	assertDownloadedAtMost(t, second.torrent, second.torrent.NumPieces()-len(got))
	second.quitCleanly()
	assertPayload(t, fx, saveRoot)
	lib.intact()
}

func TestAVolumePulledWhileSeedingIsNotRecreatedAndTheMarksSurvive(t *testing.T) {
	fx, mi := named(t, "v1-large")
	source := seeder(t, fx, mi, swarm.Options{})
	vol, saveRoot, dataDir := onVolume(t, volume.Options{Size: "32m", FS: "APFS", Name: "OctoSeed"})
	lib := libraryIn(t, dataDir)
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(source)
	first.quitCleanly()

	// the second run seeds from the volume, slowly, to a plain leecher
	second := start(t, mi, saveRoot, dataDir, runOptions{prev: &first.record, rateLimit: slowRate})
	if second.decision.VerifyAll {
		t.Fatalf("decision %+v, want the complete payload trusted", second.decision)
	}
	leecher := newPeer(t, swarm.Options{})
	lt, err := leecher.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, second.peer)
	keepMoving(t, second.peer, mi.HashInfoBytes())
	eventually(t, transferLimit, "nothing was uploaded from the volume", func() bool { return swarm.Count(lt).PayloadRead > 0 })

	vol.Detach()
	eventually(t, transferLimit, "the pulled volume was not noticed while seeding", func() bool { return !second.session.Clean() })
	if entries, err := os.ReadDir(vol.Mount); err != nil || len(entries) != 0 {
		t.Fatalf("the mount point holds %v (%v)", entries, err)
	}
	assertMarksKept(t, second, mi, all(second.torrent.NumPieces())) // the failed reads wiped none
	assertRootLost(t, second)

	vol.Attach()
	third := start(t, mi, saveRoot, dataDir, runOptions{prev: &second.record})
	if !third.decision.VerifyAll {
		t.Fatalf("decision %+v, want everything verified", third.decision)
	}
	settle(t, third.torrent)
	if got := completePieces(third.torrent); !slices.Equal(got, all(third.torrent.NumPieces())) {
		t.Fatalf("%d pieces complete after the volume came back, want all", len(got))
	}
	third.quitCleanly()
	assertPayload(t, fx, saveRoot)
	lib.intact()
}
