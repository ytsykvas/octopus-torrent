package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/testutil/volume"
)

// fakePiece and fakeInner are test doubles for the engine's file storage: they record the calls
// that reach them and return the errors they are told to.
type fakePiece struct {
	calls   *[]string
	mu      *sync.Mutex
	err     error // returned by every operation
	entered chan struct{}
	release chan struct{}
	data    []byte
}

func (p *fakePiece) record(call string) {
	p.mu.Lock()
	*p.calls = append(*p.calls, call)
	p.mu.Unlock()
}

func (p *fakePiece) ReadAt(b []byte, off int64) (int, error) {
	p.record("ReadAt")
	n := copy(b, p.data[off:])
	if p.err != nil {
		return n, p.err
	}
	return n, nil
}

func (p *fakePiece) WriteAt(b []byte, off int64) (int, error) {
	p.record("WriteAt")
	if p.entered != nil {
		p.entered <- struct{}{}
		<-p.release
	}
	return len(b), p.err
}
func (p *fakePiece) MarkComplete() error    { p.record("MarkComplete"); return p.err }
func (p *fakePiece) MarkNotComplete() error { p.record("MarkNotComplete"); return p.err }
func (p *fakePiece) Completion() storage.Completion {
	p.record("Completion")
	return storage.Completion{Ok: true, Complete: true, Err: p.err}
}

// withWriteTo is a fakePiece that offers io.WriterTo, as the engine's file piece does.
type withWriteTo struct{ *fakePiece }

func (p withWriteTo) WriteTo(w io.Writer) (int64, error) {
	p.record("WriteTo")
	n, err := w.Write(p.data)
	if err == nil {
		err = p.err
	}
	return int64(n), err
}

type fakeInner struct {
	calls     []string
	mu        sync.Mutex
	piece     *fakePiece
	writeTo   bool
	openErr   error
	impl      storage.TorrentImpl // extra fields, to test the tripwire
	closeFunc func() error
}

func newFake() *fakeInner {
	f := &fakeInner{}
	f.piece = &fakePiece{calls: &f.calls, mu: &f.mu, data: bytes.Repeat([]byte("p"), 16)}
	return f
}

func (f *fakeInner) OpenTorrent(context.Context, *metainfo.Info, metainfo.Hash) (storage.TorrentImpl, error) {
	f.mu.Lock()
	f.calls = append(f.calls, "OpenTorrent")
	f.mu.Unlock()
	if f.openErr != nil {
		return storage.TorrentImpl{}, f.openErr
	}
	impl := f.impl
	impl.Piece = func(metainfo.Piece) storage.PieceImpl {
		if f.writeTo {
			return withWriteTo{f.piece}
		}
		return f.piece
	}
	impl.Close = f.closeFunc
	return impl, nil
}

func (f *fakeInner) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

var testInfo = &metainfo.Info{PieceLength: 16, Length: 32, Pieces: make([]byte, 40), Name: "t"}

// guarded builds a TorrentStorage over a fake in a fresh save folder and opens a piece in it.
func guarded(t *testing.T, f *fakeInner) (*TorrentStorage, storage.PieceImpl, *Session, string) {
	t.Helper()
	root := t.TempDir()
	var session Session
	s, err := newGuard(f, root, &session, nil)
	if err != nil {
		t.Fatal(err)
	}
	impl, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	return s, impl.Piece(testInfo.Piece(0)), &session, root
}

func TestASaveFolderThatIsNotAnExistingCleanAbsoluteFolderIsRefused(t *testing.T) {
	file := writeFile(t)
	for name, root := range map[string]string{
		"relative":    "downloads",
		"not clean":   t.TempDir() + "/../x",
		"missing":     filepath.Join(t.TempDir(), "missing"),
		"a file":      file,
		"empty":       "",
		"a trailing/": t.TempDir() + "/",
	} {
		if _, err := newGuard(newFake(), root, &Session{}, nil); err == nil {
			t.Errorf("%s: %q was accepted", name, root)
		}
	}
}

func TestEveryOperationReachesTheEngineStorageAndFailuresAreRecorded(t *testing.T) {
	f := newFake()
	_, piece, session, _ := guarded(t, f)
	buf := make([]byte, 4)
	if _, err := piece.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := piece.WriteAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if err := piece.MarkComplete(); err != nil {
		t.Fatal(err)
	}
	if err := piece.MarkNotComplete(); err != nil {
		t.Fatal(err)
	}
	if c := piece.Completion(); !c.Ok || !c.Complete || c.Err != nil {
		t.Fatalf("Completion = %+v", c)
	}
	want := "OpenTorrent ReadAt WriteAt MarkComplete MarkNotComplete Completion"
	if got := strings.Join(f.callList(), " "); got != want {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if !session.Clean() {
		t.Fatalf("a quiet run left problems %v", session.Problems())
	}

	boom := errors.New("input/output error")
	f.piece.err = boom
	_, rerr := piece.ReadAt(buf, 0)
	_, werr := piece.WriteAt(buf, 0)
	cerr, nerr := piece.MarkComplete(), piece.MarkNotComplete()
	c := piece.Completion()
	for name, err := range map[string]error{"ReadAt": rerr, "WriteAt": werr, "MarkComplete": cerr, "MarkNotComplete": nerr, "Completion": c.Err} {
		if !errors.Is(err, boom) {
			t.Errorf("%s = %v, want the engine's error", name, err)
		}
	}
	if n := len(session.Problems()); n != 5 {
		t.Fatalf("%d problems recorded, want 5: %v", n, session.Problems())
	}
}

func TestAShortReadIsNotAStorageProblem(t *testing.T) {
	f := newFake()
	_, piece, session, _ := guarded(t, f)
	f.piece.err = io.EOF
	if _, err := piece.ReadAt(make([]byte, 4), 0); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt = %v", err)
	}
	if !session.Clean() {
		t.Fatalf("an EOF was recorded: %v", session.Problems())
	}
}

func TestAnOpenFailureOfTheEngineStorageIsReturnedAndRecorded(t *testing.T) {
	f := newFake()
	f.openErr = errors.New("cannot open")
	root := t.TempDir()
	var session Session
	s, err := newGuard(f, root, &session, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{}); !errors.Is(err, f.openErr) {
		t.Fatalf("OpenTorrent = %v", err)
	}
	if session.Clean() {
		t.Fatal("the failed open was not recorded")
	}
}

func TestAnEngineStorageWithMoreThanPieceAndCloseIsRefusedBecauseTheGuardCouldNotCoverIt(t *testing.T) {
	for name, impl := range map[string]storage.TorrentImpl{
		"PieceWithHash":  withFunc("PieceWithHash"),
		"Capacity":       {Capacity: capacity()},
		"NewReader":      {NewReader: func() storage.TorrentReader { return nil }},
		"NewPieceReader": {NewPieceReader: func(storage.Piece) storage.PieceReader { return nil }},
	} {
		f := newFake()
		f.impl = impl
		closed := false
		f.closeFunc = func() error { closed = true; return nil }
		s, err := newGuard(f, t.TempDir(), &Session{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{}); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: OpenTorrent = %v, want a refusal naming the field", name, err)
		}
		if !closed {
			t.Errorf("%s: the refused torrent storage was not closed", name)
		}
	}
}

func TestACloseOfTheEngineTorrentStorageIsForwardedAndAMissingOneIsHarmless(t *testing.T) {
	f := newFake()
	root := t.TempDir()
	s, _ := newGuard(f, root, &Session{}, nil)
	impl, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{})
	if err != nil || impl.Close == nil {
		t.Fatalf("OpenTorrent = %v, Close missing: %v", err, impl.Close == nil)
	}
	if err := impl.Close(); err != nil {
		t.Fatalf("Close without an engine Close = %v", err)
	}
	f.closeFunc = func() error { return errors.New("close failed") }
	impl, _ = s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{})
	if err := impl.Close(); err == nil || err.Error() != "close failed" {
		t.Fatalf("Close = %v, want the engine's", err)
	}
}

func TestPieceReadsAreCountedWhetherTheyGoThroughReadAtOrWriteTo(t *testing.T) {
	for _, offersWriteTo := range []bool{true, false} {
		f := newFake()
		f.writeTo = offersWriteTo
		s, piece, _, _ := guarded(t, f)
		if _, err := piece.ReadAt(make([]byte, 4), 0); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		n, err := piece.(io.WriterTo).WriteTo(&out)
		if err != nil || n != 16 || out.String() != strings.Repeat("p", 16) {
			t.Fatalf("WriteTo (engine offers it: %v) = %d, %v, %q", offersWriteTo, n, err, out.String())
		}
		if got := s.Reads(); got != 2 {
			t.Fatalf("Reads = %d, want 2", got)
		}
		if has := strings.Contains(strings.Join(f.callList(), " "), "WriteTo"); has != offersWriteTo {
			t.Fatalf("engine WriteTo used = %v, offered = %v", has, offersWriteTo)
		}
	}
}

func TestWriteToReportsTheEnginesFailure(t *testing.T) {
	f := newFake()
	f.writeTo = true
	_, piece, session, _ := guarded(t, f)
	f.piece.err = errors.New("read failed")
	if _, err := piece.(io.WriterTo).WriteTo(io.Discard); !errors.Is(err, f.piece.err) {
		t.Fatalf("WriteTo = %v", err)
	}
	if session.Clean() {
		t.Fatal("the failure was not recorded")
	}
}

func TestOnceTheSaveFolderIsGoneNoOperationReachesTheEngineAndTheLossIsRecordedOnce(t *testing.T) {
	for name, vanish := range map[string]func(root string){
		"deleted":  func(root string) { _ = os.RemoveAll(root) },
		"replaced": func(root string) { _ = os.RemoveAll(root); _ = os.Mkdir(root, 0o755) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			f.writeTo = true
			s, piece, session, root := guarded(t, f)
			before := len(f.callList())
			vanish(root)
			buf := make([]byte, 4)
			_, rerr := piece.ReadAt(buf, 0)
			_, werr := piece.WriteAt(buf, 0)
			_, terr := piece.(io.WriterTo).WriteTo(io.Discard)
			errs := []error{rerr, werr, terr, piece.MarkComplete(), piece.MarkNotComplete(), piece.Completion().Err}
			for i, err := range errs {
				if !errors.Is(err, ErrRootChanged) {
					t.Errorf("operation %d = %v, want ErrRootChanged", i, err)
				}
			}
			if _, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{}); !errors.Is(err, ErrRootChanged) {
				t.Errorf("OpenTorrent = %v, want ErrRootChanged", err)
			}
			if got := len(f.callList()); got != before {
				t.Fatalf("%d calls reached the engine after the folder was gone: %v", got-before, f.callList()[before:])
			}
			if p := session.Problems(); len(p) != 1 || !errors.Is(p[0], ErrRootChanged) {
				t.Fatalf("problems %v, want exactly one ErrRootChanged", p)
			}
		})
	}
}

func TestAfterStopReadsAreRefusedQuietlyButWritesAndMarksMakeTheSessionUnclean(t *testing.T) {
	f := newFake()
	f.writeTo = true
	s, piece, session, _ := guarded(t, f)
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(f.callList())
	buf := make([]byte, 4)
	_, rerr := piece.ReadAt(buf, 0)
	_, terr := piece.(io.WriterTo).WriteTo(io.Discard)
	cerr := piece.Completion().Err
	for name, err := range map[string]error{"ReadAt": rerr, "WriteTo": terr, "Completion": cerr} {
		if !errors.Is(err, ErrStopped) {
			t.Errorf("%s after Stop = %v, want ErrStopped", name, err)
		}
	}
	if !session.Clean() {
		t.Fatalf("refused reads made the session unclean: %v", session.Problems())
	}
	for name, err := range map[string]error{"MarkComplete": piece.MarkComplete(), "MarkNotComplete": piece.MarkNotComplete()} {
		if !errors.Is(err, ErrStopped) {
			t.Errorf("%s after Stop = %v, want ErrStopped", name, err)
		}
	}
	if n := len(session.Problems()); n != 2 {
		t.Fatalf("%d problems, want the 2 refused mutations", n)
	}
	if _, err := s.OpenTorrent(context.Background(), testInfo, metainfo.Hash{}); !errors.Is(err, ErrStopped) {
		t.Fatalf("OpenTorrent after Stop = %v", err)
	}
	if got := len(f.callList()); got != before {
		t.Fatalf("%d calls reached the engine after Stop", got-before)
	}
}

func TestLayoutIsTheEnginesPathRuleAndRefusesPathsOutsideTheSaveFolder(t *testing.T) {
	root := "/save"
	single := &metainfo.Info{Name: "movie.mkv", Length: 10}
	content, files, err := Layout(root, single)
	if err != nil || content != "/save/movie.mkv" || len(files) != 1 || files[0] != "/save/movie.mkv" {
		t.Fatalf("single file = %q %v %v", content, files, err)
	}
	multi := &metainfo.Info{Name: "album", Files: []metainfo.FileInfo{
		{Length: 1, Path: []string{"disc 1", "a.flac"}}, {Length: 2, Path: []string{"b.flac"}}}}
	content, files, err = Layout(root, multi)
	if err != nil || content != "/save/album" || strings.Join(files, "|") != "/save/album/disc 1/a.flac|/save/album/b.flac" {
		t.Fatalf("multi file = %q %v %v", content, files, err)
	}
	escape := &metainfo.Info{Name: "x", Files: []metainfo.FileInfo{{Length: 1, Path: []string{"..", "..", "evil"}}}}
	if _, _, err := Layout(root, escape); err == nil {
		t.Fatal("a path outside the save folder was accepted")
	}
	noName := &metainfo.Info{Name: metainfo.NoName, Files: []metainfo.FileInfo{{Length: 1, Path: []string{"a"}}}}
	if _, files, err := Layout(root, noName); err != nil || files[0] != "/save/a" {
		t.Fatalf("a torrent without a name = %v %v", files, err)
	}
}

func TestNewTorrentStorageBuildsOnTheEnginesFileStorage(t *testing.T) {
	root := t.TempDir()
	s, err := NewTorrentStorage(root, storage.NewMapPieceCompletion(), &Session{}, slog.New(slog.DiscardHandler))
	if err != nil || s == nil {
		t.Fatalf("NewTorrentStorage = %v, %v", s, err)
	}
	if _, err := NewTorrentStorage("relative", storage.NewMapPieceCompletion(), &Session{}, nil); err == nil {
		t.Fatal("a relative save folder was accepted")
	}
	if _, err := NewTorrentStorage(root, storage.NewMapPieceCompletion(), &Session{}, nil); err != nil {
		t.Fatalf("a nil logger = %v", err)
	}
}

func TestTheRefusingStorageWritesNothingAndSaysWhy(t *testing.T) {
	_, err := Refusing().OpenTorrent(context.Background(), testInfo, metainfo.Hash{})
	if !errors.Is(err, ErrNoSaveFolder) {
		t.Fatalf("OpenTorrent = %v, want ErrNoSaveFolder", err)
	}
}

func capacity() storage.TorrentCapacity {
	f := func() (int64, bool) { return 0, false }
	return &f
}

func TestTheEnginesShortWriteSignalOfAPieceInsideAFileIsNotAStorageProblem(t *testing.T) {
	f := newFake()
	f.writeTo = true
	_, piece, session, _ := guarded(t, f)
	f.piece.err = io.ErrShortWrite
	if _, err := piece.(io.WriterTo).WriteTo(io.Discard); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteTo = %v, want the engine's signal passed through", err)
	}
	if !session.Clean() {
		t.Fatalf("the short-write signal was recorded: %v", session.Problems())
	}
}

func TestASaveFolderOnAFileSystemTheEngineCannotHashOnIsRefusedBeforeAnyWrite(t *testing.T) {
	vol := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctoHFS"})
	_, err := NewTorrentStorage(vol.Mount, storage.NewMapPieceCompletion(), &Session{}, nil)
	if !errors.Is(err, ErrFileSystem) {
		t.Fatalf("NewTorrentStorage on HFS+ = %v, want ErrFileSystem", err)
	}
	if got := listFiles(t, vol.Mount); len(got) != 0 {
		t.Fatalf("the refused volume holds %v", got)
	}
}

// withFunc is a TorrentImpl whose func field of the given name is set to a function that does nothing,
// whatever the field's signature: the test only needs the field to be non-nil.
func withFunc(name string) storage.TorrentImpl {
	var impl storage.TorrentImpl
	field := reflect.ValueOf(&impl).Elem().FieldByName(name)
	field.Set(reflect.MakeFunc(field.Type(), func([]reflect.Value) []reflect.Value {
		out := make([]reflect.Value, field.Type().NumOut())
		for i := range out {
			out[i] = reflect.Zero(field.Type().Out(i))
		}
		return out
	}))
	return impl
}
