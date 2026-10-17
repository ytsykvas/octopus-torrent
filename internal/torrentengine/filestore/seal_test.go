package filestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
)

// multiInfo is a torrent of two files over three pieces of 16 bytes: a is [0,20), b is [20,40), so
// piece 1 spans both. Its paths are <save folder>/m/a and <save folder>/m/b.
var multiInfo = &metainfo.Info{
	Name: "m", PieceLength: 16, Pieces: make([]byte, 60),
	Files: []metainfo.FileInfo{{Length: 20, Path: []string{"a"}}, {Length: 20, Path: []string{"b"}}},
}

// sealed builds a guarded storage for multiInfo over a fake, opens the torrent, and gives the pieces.
func sealed(t *testing.T) (s *TorrentStorage, pieces [3]storage.PieceImpl, session *Session, root string, f *fakeInner) {
	t.Helper()
	f = newFake()
	root = t.TempDir()
	session = &Session{}
	var err error
	if s, err = newGuard(f, root, session, nil); err != nil {
		t.Fatal(err)
	}
	impl, err := s.OpenTorrent(context.Background(), multiInfo, metainfo.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range pieces {
		pieces[i] = impl.Piece(multiInfo.Piece(i))
	}
	mustMkdir(t, filepath.Join(root, "m"))
	return s, pieces, session, root, f
}

func fileState(t *testing.T, rec files.Record, path string) files.FileState {
	t.Helper()
	for _, f := range rec.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("the record has no %s: %+v", path, rec.Files)
	return files.FileState{}
}

func TestSealNeedsTheStorageToBeStoppedFirst(t *testing.T) {
	s, _, _, _, _ := sealed(t)
	if _, err := s.Seal(); !errors.Is(err, ErrNotStopped) {
		t.Fatalf("Seal before Stop = %v, want ErrNotStopped", err)
	}
}

func TestTheRecordDescribesEveryFileWithItsPiecesAndOnlyTheOnesTheRunLeftArePresent(t *testing.T) {
	s, pieces, _, root, _ := sealed(t)
	a, b := filepath.Join(root, "m", "a"), filepath.Join(root, "m", "b")
	mustWrite(t, a, "0123456789012345678?")
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil { // piece 0 is inside file a
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	fs, _ := files.FileSystem(root)
	rootFP, _ := files.Stat(root)
	if !rec.Clean || rec.FileSystem != fs || rec.Dev != rootFP.Dev {
		t.Fatalf("record %+v", rec)
	}
	if len(rec.Files) != 2 {
		t.Fatalf("%d files in the record", len(rec.Files))
	}
	fa, fb := fileState(t, rec, a), fileState(t, rec, b)
	wantFP, _ := files.Stat(a)
	if !fa.Present || fa.Fingerprint != wantFP {
		t.Fatalf("a = %+v, want present with the fingerprint %+v", fa, wantFP)
	}
	if fb.Present {
		t.Fatalf("b = %+v, want absent: the run never wrote it", fb)
	}
}

func TestAWriteOrAVerificationRecordsTheFilesItTouchedAsTheRunLeftThem(t *testing.T) {
	s, pieces, _, root, _ := sealed(t)
	a, b := filepath.Join(root, "m", "a"), filepath.Join(root, "m", "b")
	mustWrite(t, a, "aaaaaaaaaaaaaaaaaaaa")
	mustWrite(t, b, "bbbbbbbbbbbbbbbbbbbb")
	// piece 1 spans the end of a and the start of b: a write into it touches both
	if _, err := pieces[1].WriteAt([]byte("zz"), 3); err != nil {
		t.Fatal(err)
	}
	// piece 2 is inside b: marking it complete (the engine verified its bytes) touches b only
	later := "bbbbbbbbbbbbbbbbbbb!"
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, b, later)
	if err := pieces[2].MarkComplete(); err != nil {
		t.Fatal(err)
	}
	// an edit made after the run's last operation is not recorded: it must show at the next start
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, a, "aaaaaaaaaaaaaaaaaaa!")
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	fa, fb := fileState(t, rec, a), fileState(t, rec, b)
	nowA, _ := files.Stat(a)
	nowB, _ := files.Stat(b)
	if fa.Fingerprint == nowA {
		t.Fatalf("the edit of a after the run's last operation was recorded: %+v", fa)
	}
	if fb.Fingerprint != nowB {
		t.Fatalf("b = %+v, want the fingerprint %+v", fb.Fingerprint, nowB)
	}
}

func TestAFileTheRunNeverTouchedKeepsTheFingerprintTheEarlierRunLeftIfItWasTrusted(t *testing.T) {
	s, pieces, _, root, _ := sealed(t)
	a, b := filepath.Join(root, "m", "a"), filepath.Join(root, "m", "b")
	mustWrite(t, a, "aaaaaaaaaaaaaaaaaaaa")
	mustWrite(t, b, "bbbbbbbbbbbbbbbbbbbb")
	fa, _ := files.Stat(a)
	fb, _ := files.Stat(b)
	earlier := &files.Record{Clean: true, Files: []files.FileState{
		{Path: a, Present: true, Fingerprint: fa},
		{Path: b, Present: true, Fingerprint: fb},
	}}
	// b is trusted and kept; a is not trusted (it is to be verified), so it is not carried over
	s.Baseline(earlier, files.Decision{Trusted: []string{b}})
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, b, "bbbbbbbbbbbbbbbbbbb!") // an outside edit during the run: the carried fingerprint is the old one
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil {
		t.Fatal(err)
	}
	if got := fileState(t, rec, b); got.Fingerprint != fb || !got.Present {
		t.Fatalf("b = %+v, want the earlier run's state carried over", got)
	}
	nowA, _ := files.Stat(a)
	if got := fileState(t, rec, a); got.Fingerprint != nowA {
		t.Fatalf("a = %+v, want what this run left", got)
	}
}

func TestARunWithAStorageProblemIsNotACleanRecord(t *testing.T) {
	s, _, session, _, _ := sealed(t)
	session.Fail(errors.New("a write failed"))
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil || rec.Clean {
		t.Fatalf("Seal = %+v, %v; want an unclean record", rec, err)
	}
}

func TestAFileThatVanishedBeforeTheSyncMakesTheRunUncleanAndSealReportsIt(t *testing.T) {
	s, pieces, session, root, _ := sealed(t)
	a := filepath.Join(root, "m", "a")
	mustWrite(t, a, "aaaaaaaaaaaaaaaaaaaa")
	if _, err := pieces[0].WriteAt([]byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if !errors.Is(err, os.ErrNotExist) || rec.Clean || session.Clean() {
		t.Fatalf("Seal = %+v, %v (session clean %v); want ErrNotExist and an unclean record", rec, err, session.Clean())
	}
}

func TestRangesGiveEveryFileItsPathAndItsPieces(t *testing.T) {
	root := t.TempDir()
	got, err := Ranges(root, multiInfo)
	want := []files.FileRange{
		{Path: filepath.Join(root, "m", "a"), First: 0, End: 2},
		{Path: filepath.Join(root, "m", "b"), First: 1, End: 3},
	}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Ranges = %v, %v; want %v", got, err, want)
	}
	single := &metainfo.Info{Name: "m", PieceLength: 16, Pieces: make([]byte, 40), Length: 24}
	got, err = Ranges(root, single)
	if err != nil || len(got) != 1 || got[0] != (files.FileRange{Path: filepath.Join(root, "m"), First: 0, End: 2}) {
		t.Fatalf("Ranges of a single file = %v, %v", got, err)
	}
	if _, err := Ranges(root, &metainfo.Info{Name: "m", Files: []metainfo.FileInfo{{Length: 1, Path: []string{"..", "..", "x"}}}}); err == nil {
		t.Fatal("Ranges accepted a path outside the save folder")
	}
}

func TestClosingTheMarksStoreWithAnErrorMakesTheSessionUnclean(t *testing.T) {
	d := newDouble()
	d.closeErr = errors.New("checkpoint blocked")
	c, session := completion(t, d, slowFlush)
	if err := c.Close(); err == nil {
		t.Fatal("Close hid the store's error")
	}
	if session.Clean() {
		t.Fatal("a failed close of the marks store left the session clean")
	}
}

func TestATorrentWithAPathOutsideTheSaveFolderIsRefusedBeforeTheEngineSeesIt(t *testing.T) {
	f := newFake()
	s, err := newGuard(f, t.TempDir(), &Session{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	escape := &metainfo.Info{Name: "m", PieceLength: 16, Pieces: make([]byte, 20),
		Files: []metainfo.FileInfo{{Length: 5, Path: []string{"..", "..", "evil"}}}}
	if _, err := s.OpenTorrent(context.Background(), escape, metainfo.Hash{}); err == nil {
		t.Fatal("a torrent that writes outside the save folder was opened")
	}
	if calls := f.callList(); len(calls) != 0 {
		t.Fatalf("the engine's storage was reached: %v", calls)
	}
}

func TestABaselineOfNothingChangesNothing(t *testing.T) {
	s, _, _, _, _ := sealed(t)
	s.Baseline(nil, files.Decision{Trusted: []string{"x"}})
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if err != nil || !rec.Clean {
		t.Fatalf("Seal = %+v, %v", rec, err)
	}
	for _, f := range rec.Files {
		if f.Present {
			t.Fatalf("%+v is present", f)
		}
	}
}

func TestASaveFolderThatIsGoneAtSealIsAnErrorAndAnUncleanRecord(t *testing.T) {
	s, _, session, root, _ := sealed(t)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Seal()
	if !errors.Is(err, os.ErrNotExist) || rec.Clean || session.Clean() {
		t.Fatalf("Seal = %+v, %v (session clean %v); want ErrNotExist and an unclean record", rec, err, session.Clean())
	}
}
