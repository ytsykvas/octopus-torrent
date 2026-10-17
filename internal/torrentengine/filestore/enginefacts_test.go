package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/testutil/volume"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

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

	// with the info in the options the engine does not even set it, and says nothing
	withInfo, isNew := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: metainfo.Hash{7}, InfoBytes: mi.InfoBytes})
	select {
	case <-withInfo.GotInfo():
		t.Fatal("AddTorrentOpt set the info it was given: finding 15 no longer holds")
	default:
	}
	if !isNew || withInfo.Info() != nil {
		t.Fatalf("AddTorrentOpt with InfoBytes = new %v, info %v; want a new torrent without info and no error", isNew, withInfo.Info())
	}
}

// countingStorage counts how often the engine opens a torrent's storage.
type countingStorage struct {
	inner storage.ClientImpl
	opens atomic.Int64
}

func (c *countingStorage) OpenTorrent(ctx context.Context, info *metainfo.Info, h metainfo.Hash) (storage.TorrentImpl, error) {
	c.opens.Add(1)
	return c.inner.OpenTorrent(ctx, info, h)
}

// Engine fact (finding 15): a storage that opens is opened once, whether it is given with the torrent
// or is the client's default; a storage that refuses is asked a second time. So the number of opens
// is not something to count on.
func TestTheEngineOpensAWorkingStorageOnceAndARefusingOneTwice(t *testing.T) {
	_, mi := named(t, "v1-single")
	_, otherMI := named(t, "v1-large")
	explicit := &countingStorage{inner: plainStorage(t.TempDir(), storage.NewMapPieceCompletion())}
	working := &countingStorage{inner: plainStorage(t.TempDir(), storage.NewMapPieceCompletion())}
	refusing := &countingStorage{inner: Refusing()}

	p, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: working, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(deadline(t, closeLimit)) })
	tor, _ := p.Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: explicit})
	if err := tor.SetInfoBytes(mi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Add(otherMI); err != nil {
		t.Fatal(err)
	}
	q, err := swarm.NewPeer(t.TempDir(), swarm.Options{DefaultStorage: refusing, DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.Close(deadline(t, closeLimit)) })
	if _, err := q.Add(mi); err == nil {
		t.Fatal("a refusing storage accepted a torrent")
	}
	for name, c := range map[string]struct {
		got  int64
		want int64
	}{"explicit": {explicit.opens.Load(), 1}, "default that works": {working.opens.Load(), 1}, "default that refuses": {refusing.opens.Load(), 2}} {
		if c.got != c.want {
			t.Errorf("the %s storage was opened %d times, want %d", name, c.got, c.want)
		}
	}
}

// unreadableMarks is a completion whose reads fail.
type unreadableMarks struct{}

func (unreadableMarks) Get(metainfo.PieceKey) (storage.Completion, error) {
	return storage.Completion{}, errors.New("the marks cannot be read")
}

func (unreadableMarks) Set(metainfo.PieceKey, bool) error { return nil }

func (unreadableMarks) Close() error { return nil }

// Engine fact (finding 23): when the completion cannot be read, the engine disallows download: the
// failure is explicit and the transfer does not start, instead of the pieces being hashed or fetched.
func TestAnUnreadableCompletionStopsTheEnginesDownload(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	plain := plainStorage(t.TempDir(), unreadableMarks{})
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
	swarm.Connect(tor, seed)
	time.Sleep(1500 * time.Millisecond)
	if got := swarm.Count(tor).PayloadRead; got != 0 || len(completePieces(tor)) != 0 {
		t.Fatalf("the engine downloaded %d bytes with an unreadable completion: finding 23 no longer holds", got)
	}
	if !strings.Contains(p.Logs(), "error getting piece completion") {
		t.Errorf("the engine did not log the unreadable completion:\n%s", p.Logs())
	}
}

// Engine fact (finding 24): with part files off the engine still tries to flush "<name>.part" at every
// completed file and logs a warning that names the local path.
func TestWithPartFilesOffTheEngineStillWarnsAboutAMissingPartFile(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	var logs lockedBuffer
	r := start(t, mi, t.TempDir(), t.TempDir(), runOptions{logger: slog.New(slog.NewTextHandler(&logs, nil))})
	r.download(seed)
	r.quitCleanly()
	if !strings.Contains(logs.String(), "error flushing file before promotion") || !strings.Contains(logs.String(), ".part") {
		t.Fatalf("the storage's log does not show the part-file warning:\n%s", logs.String())
	}
}

// Engine fact (finding 16): the engine follows a symlink at <save folder>/<name>: the payload lands
// wherever it points. The guard refuses such a torrent (next test); this is what happens without it.
func TestAnEngineWithoutTheGuardWritesThroughASymlinkAtTheTorrentPath(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "elsewhere.bin")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, fx.FilePath(saveRoot, fx.Files[0])); err != nil {
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
	if err := swarm.WaitComplete(deadline(t, transferLimit), tor); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, fx.Content(fx.Files[0])) {
		t.Fatalf("the symlink target holds %d bytes (%v); the engine should have written through the link", len(got), err)
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

// Engine fact (finding 20): the engine believes a stored mark over a file of the right size and
// does not hash. It cannot see a same-size rewrite, and it re-downloads, without hashing, a file that
// is short or gone. This is why a mark is never trusted on its own.
func TestTrustedMarksAreBlindToASameSizeRewrite(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	seed := seeder(t, fx, mi, swarm.Options{})
	saveRoot, dataDir := t.TempDir(), t.TempDir()
	first := start(t, mi, saveRoot, dataDir, runOptions{})
	first.download(seed)
	first.quitCleanly()
	readWrite(t, fx.FilePath(saveRoot, fx.Files[0]), func(b []byte) { b[len(b)/2] ^= 0xff })

	believing := start(t, mi, saveRoot, dataDir, runOptions{trustMarks: true})
	settle(t, believing.torrent)
	n := believing.torrent.NumPieces()
	if bad := badPieces(t, believing.torrent.Info(), saveRoot); !slices.Equal(bad, []int{0}) {
		t.Fatalf("bad pieces = %v, want piece 0", bad)
	}
	if got := completePieces(believing.torrent); !slices.Equal(got, all(n)) {
		t.Fatalf("complete pieces = %v: the engine no longer believes the marks over a same-size rewrite", got)
	}
	if reads := believing.storage.Reads(); reads != 0 {
		t.Fatalf("%d pieces were read", reads)
	}
	believing.quit()
}

// Engine fact (finding 21): on HFS+ the engine's classic file IO cannot hash a piece, because it
// seeks with SEEK_DATA, which HFS+ lacks. Every piece fails its check and the download never
// completes. The guard refuses such a save folder; this test shows what happens without it.
func TestOnHFSPlusTheEngineCannotHashAnyPiece(t *testing.T) {
	fx, mi := named(t, "v1-single")
	seed := seeder(t, fx, mi, swarm.Options{})
	vol := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctoHFS"})
	plain := plainStorage(vol.Mount, storage.NewMapPieceCompletion())
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
	time.Sleep(2 * time.Second)
	if n := len(completePieces(tor)); n != 0 {
		t.Fatalf("%d pieces completed on HFS+: the engine can hash there now (finding 21)", n)
	}
	if !strings.Contains(p.Logs(), "inappropriate ioctl for device") {
		t.Fatalf("the engine's log does not show the SEEK_DATA failure:\n%s", p.Logs())
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
	// the folder and the file come back: the engine writes the payload on
	eventually(t, transferLimit, "the engine did not recreate the deleted save folder; the guard may no longer be needed (finding 18)", func() bool {
		_, err := os.Stat(fx.FilePath(saveRoot, fx.Files[0]))
		return err == nil
	})
}
