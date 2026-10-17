package filestore

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/library"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// eventually waits until cond holds and fails the test with what once limit has passed. The deadline
// is taken once, before the first poll.
func eventually(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	ctx := deadline(t, limit)
	for !cond() {
		select {
		case <-ctx.Done():
			t.Fatal(what)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// newPeer starts a swarm peer in a temporary folder and closes it when the test ends; a failed close
// fails the test.
func newPeer(t *testing.T, opts swarm.Options) *swarm.Peer {
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
	return p
}

// plainTorrent adds mi with the given engine storage, with none of our guard, and starts the download;
// the caller connects it. plain is closed after the peer: its cleanup is registered first.
func plainTorrent(t *testing.T, mi *metainfo.MetaInfo, plain storage.ClientImplCloser) (*swarm.Peer, *torrent.Torrent) {
	t.Helper()
	t.Cleanup(func() { _ = plain.Close() })
	p := newPeer(t, swarm.Options{DefaultStorage: Refusing(), DataDir: t.TempDir()})
	tor, _ := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: plain})
	if err := tor.SetInfoBytes(mi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	tor.DownloadAll()
	return p, tor
}

// assertDownloadedAtMost fails the test when tor fetched more payload than its missing pieces hold.
// Errorf, so the checks of the payload and the library that follow still run.
func assertDownloadedAtMost(t *testing.T, tor *torrent.Torrent, missing int) {
	t.Helper()
	if got, want := swarm.Count(tor).PayloadRead, int64(missing)*tor.Info().PieceLength; got > want {
		t.Errorf("downloaded %d payload bytes, want at most the %d of the %d missing pieces", got, want, missing)
	}
}

// assertMarksKept fails the test when a mark of pieces is no longer complete in r's completion.
func assertMarksKept(t *testing.T, r *run, mi *metainfo.MetaInfo, pieces []int) {
	t.Helper()
	for _, p := range pieces {
		if c, err := r.completion.Get(key(mi.HashInfoBytes(), p)); err != nil || !c.Ok || !c.Complete {
			t.Errorf("the mark of piece %d was lost: %+v %v", p, c, err)
		}
	}
}

// assertRootLost ends r, which lost its save folder, and requires the loss to be the first problem, the
// record to be unclean and Seal to report it.
func assertRootLost(t *testing.T, r *run) {
	t.Helper()
	problems, err := r.end()
	if len(problems) == 0 || !errors.Is(problems[0], ErrRootChanged) {
		t.Fatalf("problems %v, want ErrRootChanged first", problems)
	}
	if r.record.Clean {
		t.Error("the record of a run that lost its save folder is clean")
	}
	if err == nil {
		t.Error("Seal did not report the lost save folder")
	}
}

// writeFile returns the path of a regular file, for tests that need a path that is not a folder.
func writeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// plainStorage is the engine's own file storage over dir, with part files off and the given
// completion, with none of our guard.
func plainStorage(dir string, completion storage.PieceCompletion) storage.ClientImplCloser {
	opts := storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: completion}
	opts.UsePartFiles.Set(false)
	return storage.NewFileOpts(opts)
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// guardedLibrary is a stand-in for the owner's library database: a real library file with one row, in
// the data folder. The scenarios in which something goes wrong (a crash, a refused write, a pulled
// volume) prove that the library is still there.
type guardedLibrary struct {
	t      *testing.T
	path   string
	before [sha256.Size]byte
}

// libraryIn creates the library database in dataDir and closes it.
func libraryIn(t *testing.T, dataDir string) *guardedLibrary {
	t.Helper()
	path := filepath.Join(dataDir, "library.sqlite")
	ctx := deadline(t, closeLimit)
	s, err := library.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Write(ctx, func(tx library.Tx) error {
		if _, err := tx.Exec("CREATE TABLE torrents(id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO torrents(name) VALUES ('kept')")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	g := &guardedLibrary{t: t, path: path}
	g.before = g.hash()
	return g
}

func (g *guardedLibrary) hash() [sha256.Size]byte {
	g.t.Helper()
	data, err := os.ReadFile(g.path)
	if err != nil {
		g.t.Fatalf("the library: %v", err)
	}
	return sha256.Sum256(data)
}

// unchanged requires the library file to be byte for byte what it was.
func (g *guardedLibrary) unchanged() {
	g.t.Helper()
	if g.hash() != g.before {
		g.t.Fatal("the library database changed")
	}
}

// intact requires it to be unchanged, to open, and to hold its row.
func (g *guardedLibrary) intact() {
	g.t.Helper()
	g.unchanged()
	ctx := deadline(g.t, closeLimit)
	s, err := library.Open(ctx, g.path)
	if err != nil {
		g.t.Fatalf("the library does not open: %v", err)
	}
	defer s.Close()
	var name string
	err = s.Read(ctx, func(tx library.Tx) error { return tx.QueryRow("SELECT name FROM torrents").Scan(&name) })
	if err != nil || name != "kept" {
		g.t.Fatalf("the library row = %q, %v", name, err)
	}
}

// lockedBuffer collects a log written from the engine's goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
