package pendingmagnet

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/filestore"
	"octopustorrent/internal/torrentengine/infocheck"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
	"octopustorrent/internal/torrentengine/transferswitch"
)

// limits admit every fixture; the refusals are infocheck's own tests and the hostile-peer test.
var limits = infocheck.Limits{Files: 100, Pieces: 1000, MetadataBytes: 16 << 20}

const (
	transferLimit = 30 * time.Second
	closeLimit    = 10 * time.Second
	promptLimit   = time.Second
	holdWindow    = 2 * time.Second
)

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

func deadline(t *testing.T, limit time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	t.Cleanup(cancel)
	return ctx
}

// newPeer starts a swarm peer that is closed when the test ends. Like every product client it uses
// the keep-alive interval of task 010.
func newPeer(t *testing.T, opts swarm.Options) *swarm.Peer {
	t.Helper()
	opts.KeepAliveTimeout = transferswitch.WakeInterval
	p, err := swarm.NewPeer(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(deadline(t, closeLimit)); err != nil {
			t.Error(err)
		}
	})
	return p
}

// seeder starts a peer that seeds the fixture's payload.
func seeder(t *testing.T, fx fixture.Fixture, mi *metainfo.MetaInfo, rateLimit int) *swarm.Peer {
	t.Helper()
	p := newPeer(t, swarm.Options{RateLimit: rateLimit})
	if err := fx.WritePayload(p.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Seed(deadline(t, transferLimit), mi); err != nil {
		t.Fatal(err)
	}
	return p
}

// leecher is the application's side: an engine client whose default storage refuses, so a torrent
// without storage of its own writes nothing, with its data folder apart from the peer's folder.
type leecher struct {
	peer    *swarm.Peer
	client  *torrent.Client
	gate    *Gate
	dataDir string
}

func newLeecher(t *testing.T) leecher {
	t.Helper()
	dataDir := t.TempDir()
	var gate *Gate
	p := newPeer(t, swarm.Options{DefaultStorage: filestore.Refusing(), DataDir: dataDir, Configure: func(cfg *torrent.ClientConfig) { gate = NewGate(cfg) }})
	return leecher{peer: p, client: p.Client(), gate: gate, dataDir: dataDir}
}

// dirs are the folders a pending import must leave as they are.
func (l leecher) dirs() []string { return []string{l.dataDir, l.peer.Dir} }

// payload is the client's payload read and written over its whole life, dropped torrents included.
func (l leecher) payload() (read, written int64) {
	s := l.client.Stats()
	return s.BytesReadData.Int64(), s.BytesWrittenData.Int64()
}

func (l leecher) protocolRead() int64 {
	s := l.client.Stats()
	return s.BytesRead.Int64()
}

func (l leecher) empty(t *testing.T) {
	t.Helper()
	if n := len(l.client.Torrents()); n != 0 {
		t.Errorf("the client still holds %d torrents", n)
	}
}

// final is the storage the user's folder gets on confirmation: the guarded engine storage with our
// marks. Its cleanup follows the quit order of 009: client, storage, marks.
func final(t *testing.T, l leecher, root string) *filestore.TorrentStorage {
	t.Helper()
	session := &filestore.Session{}
	marks, err := filestore.OpenCompletion(deadline(t, closeLimit), t.TempDir(), session, filestore.Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := filestore.NewTorrentStorage(root, marks, session, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		l.client.Close()
		if err := s.Stop(deadline(t, closeLimit)); err != nil {
			t.Error(err)
		}
		if err := marks.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

// tree lists every folder and file below the dirs with its size, so a test can show that nothing
// was created, removed or changed.
func tree(t *testing.T, dirs ...string) []string {
	t.Helper()
	var out []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			size := info.Size()
			if d.IsDir() {
				size = -1
			}
			out = append(out, fmt.Sprintf("%s %d", path, size))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(out)
	return out
}

// sameTree fails the test when the dirs changed since before.
func sameTree(t *testing.T, before []string, dirs ...string) {
	t.Helper()
	if after := tree(t, dirs...); !slices.Equal(before, after) {
		t.Errorf("the folders changed:\nbefore %q\nafter  %q", before, after)
	}
}

// filesAreTheFixture checks that the files below root are exactly the fixture's, at filestore.Layout's
// paths, with the fixture's bytes.
func filesAreTheFixture(t *testing.T, fx fixture.Fixture, mi *metainfo.MetaInfo, root string) {
	t.Helper()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	_, paths, err := filestore.Layout(root, &info)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			got = append(got, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := slices.Clone(paths)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("files below the chosen folder %q, want %q", got, want)
	}
	ref := t.TempDir()
	if err := fx.WritePayload(ref); err != nil {
		t.Fatal(err)
	}
	for _, f := range fx.Files {
		have, err := os.ReadFile(fx.FilePath(root, f))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(fx.FilePath(ref, f))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(have, expected) {
			t.Errorf("%v: the bytes differ from the fixture", f.Path)
		}
	}
}
