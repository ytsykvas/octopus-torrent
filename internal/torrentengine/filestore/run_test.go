package filestore

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/files"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
	"octopustorrent/internal/torrentengine/transferswitch"
)

const (
	transferLimit = 30 * time.Second
	closeLimit    = 10 * time.Second
)

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
	p := newPeer(t, opts)
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
	decision   files.Decision // what the rehash rule decided at the start
	record     files.Record   // what the run left at its quit
}

type runOptions struct {
	rateLimit  int
	prev       *files.Record // what the earlier run left; nil is "no clean quit"
	trustMarks bool          // skip the rule: believe every stored mark (to show why the rule exists)
	completion Options
	marks      Marks // a test double for the marks store; nil is the real one in dataDir
	logger     *slog.Logger
}

// decide applies the rehash rule to what the earlier run left.
func decide(t *testing.T, mi *metainfo.MetaInfo, saveRoot string, prev *files.Record) (files.Decision, int) {
	t.Helper()
	info := infoOf(t, mi)
	ranges, err := Ranges(saveRoot, info)
	if err != nil {
		t.Fatal(err)
	}
	return files.Decide(prev, saveRoot, ranges, info.NumPieces(), files.System{}), info.NumPieces()
}

// start begins a run: it adds the torrent with its info and its own storage, and asks for all of it,
// but connects no peer.
func start(t *testing.T, mi *metainfo.MetaInfo, saveRoot, dataDir string, o runOptions) *run {
	t.Helper()
	r := &run{t: t, session: &Session{}}
	var err error
	if o.marks != nil {
		r.completion = NewCompletion(o.marks, r.session, o.completion)
	} else if r.completion, err = OpenCompletion(deadline(t, closeLimit), dataDir, r.session, o.completion); err != nil {
		t.Fatal(err)
	}
	if r.storage, err = NewTorrentStorage(saveRoot, r.completion, r.session, o.logger); err != nil {
		t.Fatal(err)
	}
	if !o.trustMarks {
		var pieces int
		r.decision, pieces = decide(t, mi, saveRoot, o.prev)
		r.completion.Distrust(mi.HashInfoBytes(), r.decision.Distrusted(pieces))
		r.storage.Baseline(o.prev, r.decision)
	}
	if r.peer, err = swarm.NewPeer(t.TempDir(), swarm.Options{RateLimit: o.rateLimit, DefaultStorage: Refusing(), DataDir: dataDir, KeepAliveTimeout: transferswitch.WakeInterval}); err != nil {
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

// drainWait lets the chunks in flight land once the engine has stopped asking for data.
const drainWait = 300 * time.Millisecond

// idleWindow is how long a connected run must stay without traffic to show that it asks for nothing.
const idleWindow = 300 * time.Millisecond

// connect gives the torrent the seeder as its only peer.
func (r *run) connect(seeder *swarm.Peer) {
	r.t.Helper()
	swarm.Connect(r.torrent, seeder)
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
// session and the errors of closing the marks and of Seal.
func (r *run) end() (problems []error, err error) {
	r.t.Helper()
	ctx := deadline(r.t, closeLimit)
	if err := r.peer.Close(ctx); err != nil {
		r.t.Error(err)
	}
	if err := r.storage.Stop(ctx); err != nil {
		r.t.Error(err)
	}
	closeErr := r.completion.Close()
	var sealErr error
	r.record, sealErr = r.storage.Seal()
	return r.session.Problems(), errors.Join(closeErr, sealErr)
}

// quit is end for a run that is expected to leave cleanly: any problem fails the test.
func (r *run) quit() []error {
	r.t.Helper()
	problems, err := r.end()
	if err != nil {
		r.t.Error(err)
	}
	return problems
}

// quiet waits until the torrent has received no payload for 200 ms in a row: the writes that were in
// flight when the download was disallowed have landed. The engine gives no signal for this; it is the
// step the application takes before it closes the engine (an open primitive for 018 and 028).
func quiet(t *testing.T, tor *torrent.Torrent) {
	t.Helper()
	ctx := deadline(t, transferLimit)
	last, since := swarm.Count(tor).PayloadRead, time.Now()
	for time.Since(since) < 200*time.Millisecond {
		select {
		case <-ctx.Done():
			t.Fatal("the transfer never went quiet")
		case <-time.After(10 * time.Millisecond):
		}
		if now := swarm.Count(tor).PayloadRead; now != last {
			last, since = now, time.Now()
		}
	}
}

// quitCleanly stops the download first and lets the writes in flight finish, as the application does
// before it closes the engine, so that nothing reaches the storage after Stop.
func (r *run) quitCleanly() {
	r.t.Helper()
	r.torrent.DisallowDataDownload()
	quiet(r.t, r.torrent)
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
