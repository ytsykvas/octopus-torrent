package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/files"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

const (
	transferLimit = 30 * time.Second
	closeLimit    = 10 * time.Second
)

// The scenarios run the real engine, through a closed loopback swarm, with our storage. Peers are
// connected only after the torrent is added, so what a restart reads from disk can be told from what
// arrives over the wire.

func deadline(t *testing.T, limit time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	t.Cleanup(cancel)
	return ctx
}

func named(t *testing.T, name string) (fixture.Fixture, *metainfo.MetaInfo) {
	t.Helper()
	fx, err := fixture.Named(name)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := fx.MetaInfo()
	if err != nil {
		t.Fatal(err)
	}
	return fx, mi
}

// seeder starts a peer that seeds the fixture's complete payload.
func seeder(t *testing.T, fx fixture.Fixture, mi *metainfo.MetaInfo, opts swarm.Options) *swarm.Peer {
	t.Helper()
	p, err := swarm.NewPeer(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(deadline(t, closeLimit)); err != nil {
			t.Error(err)
		}
	})
	if err := fx.WritePayload(p.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Seed(deadline(t, transferLimit), mi); err != nil {
		t.Fatal(err)
	}
	return p
}

// run is one run of the application on the leecher's side: an engine client whose default storage
// refuses, our marks store in dataDir, and our storage for the torrent in saveRoot.
type run struct {
	t          *testing.T
	peer       *swarm.Peer
	session    *Session
	completion *Completion
	storage    *TorrentStorage
	torrent    *torrent.Torrent
	addErr     error
	stopMoving func()         // ends the wake-ups of connect
	decision   files.Decision // what the rehash rule decided at the start
	record     files.Record   // what the run left at its quit
}

type runOptions struct {
	rateLimit  int
	prev       *files.Record // what the earlier run left; nil is "no clean quit"
	trustMarks bool          // skip the rule: believe every stored mark (to show why the rule exists)
	completion Options
}

// decide applies the rehash rule to what the earlier run left.
func decide(t *testing.T, mi *metainfo.MetaInfo, saveRoot string, prev *files.Record) (files.Decision, int) {
	t.Helper()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	return files.Decide(prev, saveRoot, info.NumPieces(), files.System{}), info.NumPieces()
}

// start begins a run: it adds the torrent with its info and its own storage, and asks for all of it,
// but connects no peer.
func start(t *testing.T, mi *metainfo.MetaInfo, saveRoot, dataDir string, o runOptions) *run {
	t.Helper()
	r := &run{t: t, session: &Session{}}
	var err error
	if r.completion, err = OpenCompletion(deadline(t, closeLimit), dataDir, r.session, o.completion); err != nil {
		t.Fatal(err)
	}
	if r.storage, err = NewTorrentStorage(saveRoot, r.completion, r.session, nil); err != nil {
		t.Fatal(err)
	}
	if !o.trustMarks {
		var pieces int
		r.decision, pieces = decide(t, mi, saveRoot, o.prev)
		r.completion.Distrust(mi.HashInfoBytes(), r.decision.Distrusted(pieces))
		r.storage.Baseline(o.prev, r.decision)
	}
	if r.peer, err = swarm.NewPeer(t.TempDir(), swarm.Options{RateLimit: o.rateLimit, DefaultStorage: Refusing(), DataDir: dataDir}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("engine log of the run:\n%s", r.peer.Logs())
		}
	})
	r.torrent, _ = r.peer.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: r.storage})
	if r.addErr = r.torrent.SetInfoBytes(mi.InfoBytes); r.addErr == nil {
		r.torrent.DownloadAll()
	}
	return r
}

// stallWindow is how long a seeder may upload nothing before keepMoving wakes the transfer.
const stallWindow = 1500 * time.Millisecond

// keepMoving wakes a transfer that stands still. The pinned engine stalls now and then (finding 22):
// one request sits unserved in the seeder's queue, and both sides wait for ever. It was seen only when
// the process runs slowly (-race: 4% of fresh transfers with classic file IO, 2% with mmap; none
// without it), and a second upload switch off and on, 50 ms apart, serves the request (finding 14).
// When the seeder has uploaded nothing for stallWindow, the helper does that and counts it; the count
// is logged at the end of the test. It stops with the test.
func keepMoving(t *testing.T, seeder *swarm.Peer, infoHash metainfo.Hash) (stopMoving func()) {
	t.Helper()
	st, ok := seeder.Client().Torrent(infoHash)
	if !ok {
		t.Fatal("the seeder does not hold the torrent")
	}
	var wakes atomic.Int64
	var once sync.Once
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		last, since := swarm.Count(st).PayloadWritten, time.Now()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			if now := swarm.Count(st).PayloadWritten; now != last {
				last, since = now, time.Now()
			} else if time.Since(since) >= stallWindow {
				st.DisallowDataUpload()
				time.Sleep(50 * time.Millisecond)
				st.AllowDataUpload()
				wakes.Add(1)
				since = time.Now()
			}
		}
	}()
	stopMoving = func() {
		once.Do(func() {
			close(stop)
			<-done
			if n := wakes.Load(); n > 0 {
				t.Logf("the stalled transfer was woken %d time(s) (engine finding 22)", n)
			}
		})
	}
	t.Cleanup(stopMoving)
	return stopMoving
}

// connect gives the torrent the seeder as its only peer, and keeps the transfer moving until the run
// ends.
func (r *run) connect(seeder *swarm.Peer) {
	r.t.Helper()
	swarm.Connect(r.torrent, seeder)
	r.stopMoving = keepMoving(r.t, seeder, r.torrent.InfoHash())
}

// connectPlain is connect for a torrent that is not part of a run.
func connectPlain(t *testing.T, tor *torrent.Torrent, seeder *swarm.Peer) {
	t.Helper()
	swarm.Connect(tor, seeder)
	keepMoving(t, seeder, tor.InfoHash())
}

// download connects the seeder and waits until the torrent is complete. A transfer that stands still
// for good is reported with the engine's own status of both sides.
func (r *run) download(seeder *swarm.Peer) {
	r.t.Helper()
	r.connect(seeder)
	if err := swarm.WaitComplete(deadline(r.t, transferLimit), r.torrent); err != nil {
		var leech, seed bytes.Buffer
		r.peer.Client().WriteStatus(&leech)
		seeder.Client().WriteStatus(&seed)
		r.t.Fatalf("%v\nleecher status:\n%s\nseeder status:\n%s", err, leech.String(), seed.String())
	}
}

// end ends the run the way the application does: the client first, then the storage (waiting for
// operations in flight), then the marks, then the seal of the record. It returns the problems of the
// session and the error of Seal.
func (r *run) end() (problems []error, sealErr error) {
	r.t.Helper()
	if r.stopMoving != nil {
		r.stopMoving()
	}
	ctx := deadline(r.t, closeLimit)
	if err := r.peer.Close(ctx); err != nil {
		r.t.Error(err)
	}
	if err := r.storage.Stop(ctx); err != nil {
		r.t.Error(err)
	}
	if err := r.completion.Close(); err != nil {
		r.t.Error(err)
	}
	r.record, sealErr = r.storage.Seal()
	return r.session.Problems(), sealErr
}

// quit is end for a run that is expected to leave cleanly: any problem fails the test.
func (r *run) quit() []error {
	r.t.Helper()
	problems, sealErr := r.end()
	if sealErr != nil {
		r.t.Error(sealErr)
	}
	return problems
}

// quitCleanly stops the download first and lets the writes in flight finish, as the application does
// before it closes the engine, so that nothing reaches the storage after Stop.
func (r *run) quitCleanly() {
	r.t.Helper()
	r.torrent.DisallowDataDownload()
	time.Sleep(300 * time.Millisecond)
	if problems := r.quit(); len(problems) != 0 {
		r.t.Fatalf("session problems: %v", problems)
	}
	if !r.record.Clean {
		r.t.Fatal("the record is not clean")
	}
}

// listFiles lists every regular file and symlink below dir, as paths relative to it.
func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestEveryFormatLandsExactlyWhereLayoutSaysAndNothingElseIsWritten(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, wd)
	for _, name := range []string{"v1-single", "v1-tree", "hybrid-single", "hybrid-tree"} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, name)
			seed := seeder(t, fx, mi, swarm.Options{})
			saveRoot, dataDir := t.TempDir(), t.TempDir()
			r := start(t, mi, saveRoot, dataDir, runOptions{})
			if r.addErr != nil {
				t.Fatal(r.addErr)
			}
			r.download(seed)
			info := r.torrent.Info()
			ownDir := r.peer.Dir
			if problems := r.quit(); len(problems) != 0 {
				t.Fatalf("session problems: %v", problems)
			}

			contentRoot, layout, err := Layout(saveRoot, info)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string][]byte{}
			for _, f := range fx.Files {
				want[fx.FilePath(saveRoot, f)] = fx.Content(f)
			}
			if len(layout) != len(want) {
				t.Fatalf("Layout lists %d files, the fixture has %d", len(layout), len(want))
			}
			for _, path := range layout {
				content, ok := want[path]
				if !ok {
					t.Errorf("Layout lists %s, which the fixture does not have", path)
					continue
				}
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, content) {
					t.Errorf("%s is %d bytes (%v), want the fixture's %d", path, len(got), err, len(content))
				}
			}
			var onDisk []string
			for _, rel := range listFiles(t, saveRoot) {
				onDisk = append(onDisk, filepath.Join(saveRoot, rel))
			}
			wantPaths := slices.Clone(layout)
			slices.Sort(wantPaths)
			if !slices.Equal(onDisk, wantPaths) {
				t.Errorf("below the save folder:\n%s\nwant exactly:\n%s", strings.Join(onDisk, "\n"), strings.Join(wantPaths, "\n"))
			}
			if _, err := os.Stat(contentRoot); err != nil {
				t.Errorf("the content root: %v", err)
			}
			if got := listFiles(t, dataDir); len(got) == 0 || !strings.HasPrefix(got[0], CompletionFile) {
				t.Errorf("the data folder holds %v, want only the marks database", got)
			}
			for _, name := range listFiles(t, dataDir) {
				if !strings.HasPrefix(name, CompletionFile) {
					t.Errorf("the data folder holds %s", name)
				}
			}
			if got := listFiles(t, ownDir); len(got) != 0 {
				t.Errorf("the peer's own folder holds %v: something was stored by the default", got)
			}
		})
	}
	if after := listFiles(t, wd); !slices.Equal(before, after) {
		t.Errorf("the working directory changed from %v to %v", before, after)
	}
}

func TestATorrentAddedWithoutStorageOfItsOwnIsRefusedAndWritesNothing(t *testing.T) {
	_, mi := named(t, "v1-single")
	dataDir := t.TempDir()
	p, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: Refusing(), DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(deadline(t, closeLimit)) })
	if _, err := p.Add(mi); err == nil {
		t.Fatal("a torrent without storage of its own was added")
	} else if !strings.Contains(err.Error(), ErrNoSaveFolder.Error()) {
		t.Fatalf("Add = %v, want %q", err, ErrNoSaveFolder)
	}
	if got := listFiles(t, dataDir); len(got) != 0 {
		t.Fatalf("the data folder holds %v", got)
	}
	if got := listFiles(t, p.Dir); len(got) != 0 {
		t.Fatalf("the peer folder holds %v", got)
	}
}

// Engine fact: AddTorrentOpt drops the storage error of the info it is given; SetInfoBytes returns it.
// The adapter must use SetInfoBytes (finding 15).
func TestTheEngineReportsAStorageErrorOnlyThroughSetInfoBytes(t *testing.T) {
	_, mi := named(t, "v1-single")
	p, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: Refusing(), DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(deadline(t, closeLimit)) })
	tor, isNew := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes()})
	if !isNew || tor == nil {
		t.Fatal("AddTorrentOpt did not add the torrent")
	}
	if err := tor.SetInfoBytes(mi.InfoBytes); err == nil || !strings.Contains(err.Error(), ErrNoSaveFolder.Error()) {
		t.Fatalf("SetInfoBytes = %v, want the storage error %q", err, ErrNoSaveFolder)
	}
}

// Engine fact: the engine follows a symlink at <save folder>/<name>: the payload lands wherever it
// points (finding 16). 020 and 023 must refuse such a path before the add.
func TestAnEngineWritesThroughASymlinkAtTheTorrentPath(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "elsewhere.bin")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := fx.FilePath(saveRoot, fx.Files[0])
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	r := start(t, mi, saveRoot, t.TempDir(), runOptions{})
	if r.addErr != nil {
		t.Fatal(r.addErr)
	}
	r.download(seed)
	r.quit()
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, fx.Content(fx.Files[0])) {
		t.Fatalf("the symlink target holds %d bytes (%v); the payload should have been written through the link", len(got), err)
	}
}

// Engine fact (finding 17): with classic file IO, WriteTo returns io.ErrShortWrite for a piece that
// ends inside its file, because the engine's limit writer reports the end of the range that way; the
// engine only logs it and the hash is right. A piece that ends where its file ends returns nil.
func TestClassicIOReportsAShortWriteForAPieceThatEndsInsideItsFile(t *testing.T) {
	fx, mi := named(t, "v1-large")
	dir := t.TempDir()
	if err := fx.WritePayload(dir); err != nil {
		t.Fatal(err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	var session Session
	s, err := NewTorrentStorage(dir, storage.NewMapPieceCompletion(), &session, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	impl, err := s.OpenTorrent(deadline(t, closeLimit), &info, mi.HashInfoBytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := impl.Piece(info.Piece(0)).(io.WriterTo).WriteTo(io.Discard); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("a piece inside its file: WriteTo = %v, want io.ErrShortWrite", err)
	}
	if _, err := impl.Piece(info.Piece(info.NumPieces() - 1)).(io.WriterTo).WriteTo(io.Discard); err != nil {
		t.Errorf("the last piece of the file: WriteTo = %v, want nil", err)
	}
	if !session.Clean() {
		t.Errorf("session problems: %v", session.Problems())
	}
}
