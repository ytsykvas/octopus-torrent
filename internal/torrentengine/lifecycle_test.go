package torrentengine_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/chansync/events"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	pp "github.com/anacrolix/torrent/peer_protocol"
	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/torrentengine/filestore"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

func TestMain(m *testing.M) { os.Exit(swarm.Main(m)) }

const (
	transferLimit = 20 * time.Second
	closeLimit    = 5 * time.Second
	promptLimit   = time.Second
	deadlockGrace = 2 * time.Second
)

// The signatures every later task relies on. A pinned version that changes one fails to compile
// here, before any adapter code is written against it.
var (
	_ func(*torrent.ClientConfig) (*torrent.Client, error)                        = torrent.NewClient
	_ func(*metainfo.MetaInfo) (*torrent.TorrentSpec, error)                      = torrent.TorrentSpecFromMetaInfoErr
	_ func(*torrent.Client, *torrent.TorrentSpec) (*torrent.Torrent, bool, error) = (*torrent.Client).AddTorrentSpec
	_ func(*torrent.Client, torrent.AddTorrentOpts) (*torrent.Torrent, bool)      = (*torrent.Client).AddTorrentOpt
	_ func(*torrent.Client, metainfo.Hash) (*torrent.Torrent, bool)               = (*torrent.Client).AddTorrentInfoHash
	_ func(*torrent.Client, metainfo.Hash) (*torrent.Torrent, bool)               = (*torrent.Client).Torrent
	_ func(*torrent.Client) []*torrent.Torrent                                    = (*torrent.Client).Torrents
	_ func(*torrent.Client) []error                                               = (*torrent.Client).Close
	_ func(*torrent.Client) events.Done                                           = (*torrent.Client).Closed
	_ func(*torrent.Torrent) events.Done                                          = (*torrent.Torrent).GotInfo
	_ func(*torrent.Torrent) *metainfo.Info                                       = (*torrent.Torrent).Info
	_ func(*torrent.Torrent) torrent.TorrentStats                                 = (*torrent.Torrent).Stats
	_ func(*torrent.Torrent) int64                                                = (*torrent.Torrent).BytesCompleted
	_ func(*torrent.Torrent) int64                                                = (*torrent.Torrent).Length
	_ func(*torrent.Torrent)                                                      = (*torrent.Torrent).DownloadAll
	_ func(*torrent.Torrent)                                                      = (*torrent.Torrent).Drop
	_ func(*torrent.Torrent) events.Done                                          = (*torrent.Torrent).Closed
)

func TestATorrentLivesFromAddToDropAndTheClientFromNewToClose(t *testing.T) {
	fx := named(t, "v1-single")
	mi := metaInfo(t, fx)
	seeder := seed(t, fx, woken(0))
	leecher := peer(t, woken(0))
	client := leecher.Client()

	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		t.Fatal(err)
	}
	lt, isNew, err := client.AddTorrentSpec(spec)
	if err != nil || !isNew {
		t.Fatalf("AddTorrentSpec = new %v, %v; want a new torrent", isNew, err)
	}
	if !isClosed(lt.GotInfo()) {
		t.Error("GotInfo is still open for a torrent added with its metainfo")
	}
	if c := swarm.Count(lt); c != (swarm.Counters{}) {
		t.Errorf("a torrent with no peer already counts %+v", c)
	}
	lt.DownloadAll()
	swarm.Connect(lt, seeder)
	if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	length := lt.Length()
	if got := lt.BytesCompleted(); got != length {
		t.Errorf("BytesCompleted = %d, want the length %d", got, length)
	}
	if got := swarm.Count(lt).PayloadRead; got < length {
		t.Errorf("the leecher read %d payload bytes, want at least %d", got, length)
	}
	st, _ := seeder.Client().Torrent(mi.HashInfoBytes())
	if got := swarm.Count(st).PayloadWritten; got < length {
		t.Errorf("the seeder wrote %d payload bytes, want at least %d", got, length)
	}

	lt.Drop()
	if !waitClosed(lt.Closed(), promptLimit) {
		t.Fatal("Closed is still open after Drop")
	}
	if _, ok := client.Torrent(mi.HashInfoBytes()); ok {
		t.Error("the client still holds the dropped torrent")
	}
	if data, err := os.ReadFile(fx.FilePath(leecher.Dir, fx.Files[0])); err != nil || int64(len(data)) != length {
		t.Errorf("after Drop the payload is %d bytes, %v; Drop must leave the files", len(data), err)
	}

	if errs := client.Close(); len(errs) != 0 {
		t.Errorf("Client.Close = %v", errs)
	}
	if !waitClosed(client.Closed(), promptLimit) {
		t.Error("Client.Closed is still open after Close")
	}
	closeAll(t, leecher, seeder)
}

func TestAddingAKnownTorrentReturnsTheSameTorrentWithNewFalse(t *testing.T) {
	fx := named(t, "v1-single")
	mi := metaInfo(t, fx)
	seeder := seed(t, fx, woken(0))
	leecher := peer(t, woken(0))
	lt, err := leecher.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}

	otherDir := t.TempDir()
	other := storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: otherDir, PieceCompletion: storage.NewMapPieceCompletion()})
	t.Cleanup(func() {
		if err := other.Close(); err != nil {
			t.Errorf("closing the second storage: %v", err)
		}
	})
	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		t.Fatal(err)
	}
	spec.Storage = other
	again, isNew, err := leecher.Client().AddTorrentSpec(spec)
	if again != lt || isNew || err != nil {
		t.Fatalf("AddTorrentSpec of a known torrent = same %v, new %v, %v; want the same torrent, new=false", again == lt, isNew, err)
	}
	if byHash, isNew := leecher.Client().AddTorrentInfoHash(mi.HashInfoBytes()); byHash != lt || isNew {
		t.Errorf("AddTorrentInfoHash of a known torrent = same %v, new %v", byHash == lt, isNew)
	}

	swarm.Connect(lt, seeder)
	if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(otherDir); err != nil || len(entries) != 0 {
		t.Errorf("the second spec's storage got %d entries, %v; the engine must ignore it", len(entries), err)
	}

	// The second caller holds the same torrent, so its Drop ends the first caller's torrent too:
	// an import that got new=false must never drop it.
	again.Drop()
	if !waitClosed(lt.Closed(), promptLimit) {
		t.Error("Drop through the second handle left the first one open")
	}
}

func TestAFailedAddIsAnErrorAndLeavesAnExistingTorrentWorking(t *testing.T) {
	t.Run("an info dictionary the engine cannot read", func(t *testing.T) {
		client := peer(t, woken(0)).Client()
		for _, fx := range fixture.All() {
			if fx.Engine != fixture.StageInfo {
				continue
			}
			if _, err := torrent.TorrentSpecFromMetaInfoErr(metaInfo(t, fx)); err == nil {
				t.Errorf("%s: TorrentSpecFromMetaInfoErr accepted it", fx.Name)
			}
			if _, err := client.AddTorrent(metaInfo(t, fx)); err == nil {
				t.Errorf("%s: AddTorrent accepted it", fx.Name)
			}
		}
		if n := len(client.Torrents()); n != 0 {
			t.Errorf("after the failed adds the client holds %d torrents", n)
		}
	})

	fx := named(t, "v1-single")
	mi := metaInfo(t, fx)
	broken := func(t *testing.T) *torrent.TorrentSpec {
		spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			t.Fatal(err)
		}
		spec.InfoBytes = []byte("not bencode")
		return spec
	}

	t.Run("info bytes that are not bencode, as a new torrent", func(t *testing.T) {
		client := peer(t, woken(0)).Client()
		lt, isNew, err := client.AddTorrentSpec(broken(t))
		if err == nil || !isNew {
			t.Fatalf("AddTorrentSpec = new %v, %v; want an error for a new torrent", isNew, err)
		}
		if !waitClosed(lt.Closed(), promptLimit) || len(client.Torrents()) != 0 {
			t.Error("the failed new torrent was not dropped")
		}
	})

	t.Run("info bytes that are not bencode, for an existing torrent", func(t *testing.T) {
		seeder := seed(t, fx, woken(0))
		leecher := peer(t, woken(0))
		lt, err := leecher.Leech(mi)
		if err != nil {
			t.Fatal(err)
		}
		again, isNew, err := leecher.Client().AddTorrentSpec(broken(t))
		if err == nil || isNew || again != lt {
			t.Fatalf("AddTorrentSpec = same %v, new %v, %v; want an error and the existing torrent", again == lt, isNew, err)
		}
		if isClosed(lt.Closed()) {
			t.Fatal("the failed merge closed the existing torrent")
		}
		swarm.Connect(lt, seeder)
		if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
			t.Errorf("the existing torrent stopped working: %v", err)
		}
	})
}

// The pinned engine checks info bytes against a v1-only infohash only when they match it or its
// short v2 form; anything else falls through hashInfoBytes without an error. Info bytes of another
// torrent are therefore accepted under the wrong infohash, from a spec and from a peer's metadata
// alike: a magnet leecher takes whatever info dictionary its first peer sends, and creates that
// dictionary's zero-length files before any download.
func TestPinnedEngineAcceptsInfoBytesThatDoNotMatchTheInfohash(t *testing.T) {
	want := metaInfo(t, named(t, "v1-single"))
	other := named(t, "v1-tree")
	spec, err := torrent.TorrentSpecFromMetaInfoErr(want)
	if err != nil {
		t.Fatal(err)
	}
	spec.InfoBytes = metaInfo(t, other).InfoBytes
	otherInfo, err := metaInfo(t, other).UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("from a spec", func(t *testing.T) {
		client := peer(t, woken(0)).Client()
		lt, isNew, err := client.AddTorrentSpec(spec)
		if err != nil || !isNew || lt.Info().Name != otherInfo.Name {
			t.Fatalf("AddTorrentSpec = new %v, %v, name %q; the engine now rejects info bytes of another torrent: re-check 011, 019 and 022's notes", isNew, err, lt.Info().Name)
		}
	})

	t.Run("from a peer's metadata", func(t *testing.T) {
		liar := peer(t, woken(0))
		if _, err := liar.Lie(deadline(t, transferLimit), want.HashInfoBytes(), spec.InfoBytes); err != nil {
			t.Fatal(err)
		}
		leecher := peer(t, woken(0))
		lt := leecher.AddInfohash(want.HashInfoBytes())
		swarm.Connect(lt, liar)
		if err := swarm.WaitInfo(deadline(t, transferLimit), lt); err != nil {
			t.Fatal(err)
		}
		got := lt.Metainfo().InfoBytes
		if lt.Info().Name != otherInfo.Name || metainfo.HashBytes(got) == want.HashInfoBytes() {
			t.Fatalf("the magnet got %q (hash match %v); the engine now verifies peer metadata: re-check 011's notes", lt.Info().Name, metainfo.HashBytes(got) == want.HashInfoBytes())
		}
		// The defence 011 and 022 need: hash the received info bytes before anything else.
		empty := other.Files[1] // tree's zero-length file, created on metadata arrival
		if empty.Length != 0 {
			t.Fatalf("fixture %s changed: file %v is not the empty one", other.Name, empty.Path)
		}
		if _, err := os.Stat(other.FilePath(leecher.Dir, empty)); err != nil {
			t.Errorf("the peer's zero-length file was not created on metadata arrival: %v", err)
		}
	})
}

func TestATorrentWaitingForMetadataIsDroppedAndClosedPromptly(t *testing.T) {
	hash := metaInfo(t, named(t, "v1-single")).HashInfoBytes()

	t.Run("Drop", func(t *testing.T) {
		leecher := peer(t, woken(0))
		lt := leecher.AddInfohash(hash)
		if waitClosed(lt.GotInfo(), 500*time.Millisecond) {
			t.Fatal("GotInfo closed with no peer to send the metainfo")
		}
		if lt.Info() != nil || lt.Length() != 0 {
			t.Errorf("before the metainfo: Info %v, Length %d; want nil and 0", lt.Info(), lt.Length())
		}
		lt.Drop()
		if !waitClosed(lt.Closed(), promptLimit) {
			t.Error("Closed is still open after Drop")
		}
		closeAll(t, leecher)
	})

	t.Run("Client.Close", func(t *testing.T) {
		leecher := peer(t, woken(0))
		lt := leecher.AddInfohash(hash)
		closed := make(chan []error, 1)
		go func() { closed <- leecher.Client().Close() }()
		select {
		case errs := <-closed:
			if len(errs) != 0 {
				t.Errorf("Client.Close = %v, want no error", errs)
			}
		case <-time.After(promptLimit):
			t.Fatal("Client.Close did not return within 1 s while metadata was awaited")
		}
		if !waitClosed(lt.Closed(), promptLimit) {
			t.Fatal("Client.Close did not end the wait for metadata within 1 s")
		}
		closeAll(t, leecher)
	})
}

func TestClosingTheClientRepeatedlyDuringTransfersLeavesNothingRunning(t *testing.T) {
	fx := named(t, "v1-large")
	mi := metaInfo(t, fx)
	waiting := metaInfo(t, named(t, "v1-single")).HashInfoBytes()
	for cycle := range 3 {
		opts := woken(64 << 10)
		seeder := seed(t, fx, opts)
		leecher := peer(t, opts)
		lt, err := leecher.Leech(mi)
		if err != nil {
			t.Fatal(err)
		}
		pending := leecher.AddInfohash(waiting)
		swarm.Connect(lt, seeder)
		if err := firstPayload(deadline(t, transferLimit), lt); err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}

		for _, p := range []*swarm.Peer{leecher, seeder} {
			if errs := p.Client().Close(); len(errs) != 0 {
				t.Errorf("cycle %d: Client.Close = %v, want no error", cycle, errs)
			}
			if !waitClosed(p.Client().Closed(), closeLimit) {
				t.Fatalf("cycle %d: Client.Closed is still open", cycle)
			}
		}
		if !isClosed(lt.Closed()) || !isClosed(pending.Closed()) {
			t.Errorf("cycle %d: Client.Close left a torrent open", cycle)
		}
		// Released twice: a second Close (inside Peer.Close) and a Drop after Close are harmless.
		lt.Drop()
		closeAll(t, leecher, seeder)
	}
}

func TestPinnedEngineIgnoresTheUploadSwitchOfTheAddOptions(t *testing.T) {
	fx := named(t, "v1-single")
	mi := metaInfo(t, fx)
	seeder := peer(t, woken(0))
	if err := fx.WritePayload(seeder.Dir); err != nil {
		t.Fatal(err)
	}
	st, isNew := seeder.Client().AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash:           mi.HashInfoBytes(),
		InfoBytes:          mi.InfoBytes,
		DisallowDataUpload: true,
	})
	if !isNew {
		t.Fatal("AddTorrentOpt did not add a new torrent")
	}
	ctx := deadline(t, transferLimit)
	if err := st.VerifyDataContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-st.Complete().On():
	case <-ctx.Done():
		t.Fatal("the seeder never marked its data complete")
	}

	leecher := peer(t, woken(0))
	lt, err := leecher.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, seeder)
	if err := swarm.WaitComplete(ctx, lt); err != nil {
		t.Fatalf("the leecher did not get the data, so the option is honoured now: re-check 010's notes: %v", err)
	}
}

// Allowing download right after disallowing it, while the cancelled requests are still on the wire,
// leaves the pinned engine with no request for good: the transfer never moves again (20 of 20 runs
// while 007 was written; with 5 ms between the two calls, 0 of 20). Cause (010): the last Reject of
// a cancelled request triggers a request update that adds nothing, and nothing asks again; see
// TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected. The product resumes download with
// transferswitch.ResumeDownload.
func TestPinnedEngineStallsWhenDownloadIsAllowedRightAfterDisallowing(t *testing.T) {
	fx := named(t, "v1-large")
	opts := swarm.Options{RateLimit: 256 << 10}
	seeder := seed(t, fx, opts)
	lt, err := peer(t, opts).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, seeder)
	if err := firstPayload(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	lt.DisallowDataDownload()
	lt.AllowDataDownload()
	// 1 MiB at 256 KiB/s needs at most 4 s, so a transfer that resumed completes within the wait.
	err = swarm.WaitComplete(deadline(t, 8*time.Second), lt)
	var incomplete *swarm.IncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("WaitComplete = %v; the transfer resumed, so the engine no longer stalls: re-check 010, 018 and 025's notes", err)
	}
	before := incomplete.Completed
	time.Sleep(time.Second)
	if now := lt.BytesCompleted(); now != before {
		t.Fatalf("the transfer moved from %d to %d bytes: the engine no longer stalls: re-check 010, 018 and 025's notes", before, now)
	}
}

// Finding 13 continued: the rename is not where it ends. The user's file, renamed to the part file,
// is where the engine then writes the torrent's bytes, and the completed part file takes the name
// back. A user's file at a torrent path is overwritten, not set aside.
func TestPinnedEngineOverwritesAUsersFileAtATorrentPathWhenItDownloads(t *testing.T) {
	// v1-single: v1-tree has a zero-length file, which the default IO cannot download across (006
	// limitation 2).
	fx := named(t, "v1-single")
	seeder := seed(t, fx, woken(0))
	leecher := peer(t, woken(0))
	file := fx.Files[0]
	path := fx.FilePath(leecher.Dir, file)
	users := []byte("the user's own file, which the torrent never named")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, users, 0o644); err != nil {
		t.Fatal(err)
	}
	lt, err := leecher.Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, seeder)
	if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, fx.Content(file)) {
		t.Errorf("%s holds %d bytes (%v), want the torrent's %d: the engine changed, re-check 020, 023, 029 and 030's notes", path, len(data), err, file.Length)
	}
	err = filepath.WalkDir(leecher.Dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if data, err := os.ReadFile(p); err != nil || bytes.Contains(data, users) {
			t.Errorf("%s still holds the user's bytes (%v): the engine changed, re-check 020, 023, 029 and 030's notes", p, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	closeAll(t, leecher, seeder)
}

// childDirEnv runs TestPinnedEngineDeadlocksOnVerifyAfterDrop as the child process that deadlocks.
const childDirEnv = "OCTOPUS_ENGINE_DEADLOCK_CHILD_DIR"

// childDir is the folder this process was given as a child in env, or "" in the parent. A folder
// outside the temporary folder (a variable left in the developer's shell) is refused before the child
// writes anything into it.
func childDir(t *testing.T, env string) string {
	t.Helper()
	dir := os.Getenv(env)
	if dir != "" && !strings.HasPrefix(dir, filepath.Clean(os.TempDir())+string(filepath.Separator)) {
		t.Fatalf("%s=%q is not under the temporary folder %s", env, dir, os.TempDir())
	}
	return dir
}

func TestPinnedEngineDeadlocksOnVerifyAfterDrop(t *testing.T) {
	if dir := childDir(t, childDirEnv); dir != "" {
		verifyAfterDrop(t, dir)
		return
	}
	// The deadlock keeps the client lock forever, so it runs in a child process: in this one it would
	// hang every later test and the goroutine check.
	var out lockedBuffer
	// The child's own timeout ends it even if this process dies before it can kill the child.
	cmd := exec.Command(os.Args[0], "-test.run=^TestPinnedEngineDeadlocksOnVerifyAfterDrop$", "-test.count=1", "-test.v", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), childDirEnv+"="+t.TempDir())
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	ended := false
	defer func() {
		if !ended {
			_ = cmd.Process.Kill()
			<-exited
		}
	}()
	// The child's setup (a client, a payload, a piece check) has a generous limit, so a loaded machine
	// cannot pass for a fixed engine; the deadlock itself then has a fixed grace period to end.
	limit := time.After(transferLimit)
	for !strings.Contains(out.String(), "VerifyDataContext returned") {
		select {
		case err := <-exited:
			ended = true
			t.Fatalf("the child exited (%v) before VerifyDataContext returned:\n%s", err, out.String())
		case <-limit:
			t.Fatalf("VerifyDataContext did not return within %v:\n%s", transferLimit, out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	select {
	case err := <-exited:
		ended = true
		t.Fatalf("the child exited (%v), so the deadlock is gone: re-check 018's notes\n%s", err, out.String())
	case <-time.After(deadlockGrace):
	}
	if text := out.String(); strings.Contains(text, "Torrents returned") {
		t.Errorf("Torrents returned after VerifyDataContext on a dropped torrent:\n%s", text)
	}
}

// verifyAfterDrop is the child: VerifyDataContext on a dropped torrent returns, but keeps the client
// lock, so the next call that takes it never returns.
func verifyAfterDrop(t *testing.T, dir string) {
	fx := named(t, "v1-single")
	p, err := swarm.NewPeer(dir, swarm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.WritePayload(dir); err != nil {
		t.Fatal(err)
	}
	st, err := p.Seed(deadline(t, transferLimit), metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	st.Drop()
	t.Logf("VerifyDataContext returned %v", st.VerifyDataContext(context.Background()))
	p.Client().Torrents()
	t.Log("Torrents returned")
}

// seed starts a peer that seeds the fixture's payload.
func seed(t *testing.T, fx fixture.Fixture, opts swarm.Options) *swarm.Peer {
	t.Helper()
	p := peer(t, opts)
	if err := fx.WritePayload(p.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Seed(deadline(t, transferLimit), metaInfo(t, fx)); err != nil {
		t.Fatal(err)
	}
	return p
}

// peer starts a peer in a fresh temporary directory and closes it when the test ends.
func peer(t *testing.T, opts swarm.Options) *swarm.Peer {
	t.Helper()
	p, err := swarm.NewPeer(t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeLimit)
		defer cancel()
		if err := p.Close(ctx); err != nil {
			t.Error(err)
		}
		if t.Failed() {
			t.Logf("engine log of the peer in %s:\n%s", p.Dir, p.Logs())
		}
	})
	return p
}

// closeAll closes the peers, waits until their addresses refuse connections, and requires that no
// engine goroutine is left in the process.
func closeAll(t *testing.T, peers ...*swarm.Peer) {
	t.Helper()
	ctx := deadline(t, closeLimit)
	for _, p := range peers {
		if err := p.Close(ctx); err != nil {
			t.Error(err)
		}
	}
	if err := swarm.WaitEngineStopped(ctx); err != nil {
		t.Error(err)
	}
}

func firstPayload(ctx context.Context, lt *torrent.Torrent) error {
	for swarm.Count(lt).PayloadRead == 0 {
		select {
		case <-ctx.Done():
			return errors.New("no payload arrived")
		case <-time.After(10 * time.Millisecond):
		}
	}
	return nil
}

func isClosed(ch events.Done) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func waitClosed[T any](ch <-chan T, limit time.Duration) bool {
	select {
	case <-ch:
		return true
	case <-time.After(limit):
		return false
	}
}

func deadline(t *testing.T, limit time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	t.Cleanup(cancel)
	return ctx
}

func named(t *testing.T, name string) fixture.Fixture {
	t.Helper()
	fx, err := fixture.Named(name)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func metaInfo(t *testing.T, fx fixture.Fixture) *metainfo.MetaInfo {
	t.Helper()
	mi, err := fx.MetaInfo()
	if err != nil {
		t.Fatal(err)
	}
	return mi
}

// lockedBuffer collects a child's output while the exec package writes it from its own goroutine.
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

// Finding 31: once a peer's metadata set a torrent's info, SetInfoBytes with other bytes returns nil
// and keeps the peer's info. An add that gives a torrent its peers before its own info can end up on
// a stranger's file list with no error: 011 never adds a second time, and 019/023 add a .torrent with
// no peer until its info is set.
func TestPinnedEngineKeepsThePeersInfoWhenSetInfoBytesComesLate(t *testing.T) {
	want := metaInfo(t, named(t, "v1-single"))
	other := metaInfo(t, named(t, "v1-tree"))
	liar := peer(t, woken(0))
	if _, err := liar.Lie(deadline(t, transferLimit), want.HashInfoBytes(), other.InfoBytes); err != nil {
		t.Fatal(err)
	}
	leecher := peer(t, woken(0))
	lt := leecher.AddInfohash(want.HashInfoBytes())
	lt.DisallowDataDownload()
	swarm.Connect(lt, liar)
	if err := swarm.WaitInfo(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	if err := lt.SetInfoBytes(want.InfoBytes); err != nil || lt.Info().Name != "tree" {
		t.Fatalf("SetInfoBytes after the peer's metadata = %v, name %q; the engine now replaces or refuses the info: re-check 011, 019 and 023's notes", err, lt.Info().Name)
	}
}

// hostileChildDirEnv and hostileShapeEnv run TestPinnedEnginePanicsOnHostileInfo as the child that
// applies one hostile info and panics.
const (
	hostileChildDirEnv = "OCTOPUS_ENGINE_HOSTILE_CHILD_DIR"
	hostileShapeEnv    = "OCTOPUS_ENGINE_HOSTILE_SHAPE"
)

// childPeer starts a swarm peer in a child process's folder, closed when the child's test ends.
func childPeer(t *testing.T, dir string) *swarm.Peer {
	t.Helper()
	p, err := swarm.NewPeer(dir, swarm.Options{})
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

// runChild runs test in a child process with env set to a fresh temporary folder plus extra, and
// returns its output and exit error. The child's own timeout ends it even if this process dies.
func runChild(t *testing.T, test, env string, extra ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+test+"$", "-test.count=1", "-test.v", "-test.timeout=60s")
	cmd.Env = append(append(os.Environ(), env+"="+t.TempDir()), extra...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Findings 33–37: info that the pinned engine parses or applies without checking it, and that
// crashes it. A peer can serve such info for a magnet whose btih is its SHA-1, so it passes every
// hash check; over the wire the crash is in an engine goroutine and ends the application.
//   - 33: v2-only info under a v1 infohash ("piece request order has {} and pending pieces has …").
//     The check is sampled; the first one in a process runs, so a fresh child panics every time.
//   - 34: "meta version 2" info whose file tree needs more pieces than "pieces" holds: makePieces
//     slices past the end of "pieces" ("slice bounds out of range"), every time.
//   - 35: a file tree node the engine reads unchecked: a pieces root that is not 32 bytes
//     ("PiecesRootAsByteArray"), or a folder with a length, which the engine counts as pieces while
//     no file holds them ("mustGetOnlyFile"), every time.
//   - 36: "meta version 2" with no piece length: validateInfo divides by it before the storage is
//     opened ("integer divide by zero").
//   - 37: an unknown key nested 4M levels: the recursive decoder overflows the stack before anything
//     else runs ("fatal error: stack overflow", which no recover can catch).
//
// Each shape runs in a child process, applied as the engine applies a peer's complete metadata.
// pendingmagnet's gate keeps peers' metadata from the engine, and infocheck.CheckBytes refuses every
// shape before Fetch gives the engine the bytes (TestFetchRefusesHostileMetadataBeforeTheEngineParsesIt);
// when a release fixes the engine, this test fails and names what to re-check.
func TestPinnedEnginePanicsOnHostileInfo(t *testing.T) {
	if dir := childDir(t, hostileChildDirEnv); dir != "" {
		for _, h := range fixture.HostileInfos() {
			if h.Name == os.Getenv(hostileShapeEnv) {
				lt, _ := childPeer(t, dir).Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: metainfo.HashBytes(h.InfoBytes), Storage: filestore.NewDeferred(func(*metainfo.Info) error { return nil }), DisableInitialPieceCheck: true})
				t.Logf("SetInfoBytes returned %v", lt.SetInfoBytes(h.InfoBytes))
				return
			}
		}
		t.Fatalf("no hostile info named %q", os.Getenv(hostileShapeEnv))
	}
	for _, h := range fixture.HostileInfos() {
		out, err := runChild(t, "TestPinnedEnginePanicsOnHostileInfo", hostileChildDirEnv, hostileShapeEnv+"="+h.Name)
		if err == nil || !strings.Contains(out, h.Panic) {
			t.Errorf("%s: the child ended with %v and no %q, so finding %d may be fixed: re-check the magnet-pending-storage ADR and infocheck\n%s", h.Name, err, h.Panic, h.Finding, out)
		}
	}
}

// Finding 38: the engine's file tree decoder parses every folder again for each folder above it,
// so a chain of folders costs the square of its depth, under the client lock (100 KB took 18–30 s).
// Twice the depth must cost more than three times the allocation; when a release makes it linear,
// re-check infocheck's maxTreeWork.
func TestPinnedEngineDecodesAFileTreeInQuadraticWork(t *testing.T) {
	alloc := func(depth int) uint64 {
		b := []byte("d9:file tree" + strings.Repeat("d1:a", depth) + "de" + strings.Repeat("e", depth) + "e")
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var info metainfo.Info
		if err := bencode.Unmarshal(b, &info); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	if small, large := alloc(1000), alloc(2000); large < 3*small {
		t.Errorf("a tree twice as deep allocated %d bytes against %d: the decoder may be linear now; re-check infocheck's maxTreeWork", large, small)
	}
}

// infoPastTheIndexEnv runs TestPinnedEnginePanicsPlanningPastItsRequestIndex as the child.
const infoPastTheIndexEnv = "OCTOPUS_ENGINE_INDEX_CHILD_DIR"

// Finding 39: an info with a piece length of 2^32 is applied without complaint, and the engine
// panics once a peer has its pieces: the request planner divides by a chunk count its uint32 index
// truncated to zero. infocheck refuses such a piece length; when a release fixes the engine, this
// test fails and names what to re-check.
func TestPinnedEnginePanicsPlanningPastItsRequestIndex(t *testing.T) {
	if dir := childDir(t, infoPastTheIndexEnv); dir != "" {
		var m map[string]any
		if err := bencode.Unmarshal(metaInfo(t, named(t, "v1-single")).InfoBytes, &m); err != nil {
			t.Fatal(err)
		}
		m["piece length"], m["pieces"] = int64(1)<<32, string(make([]byte, 20))
		b := bencode.MustMarshal(m)
		h := metainfo.HashBytes(b)
		seeder, err := swarm.ServeMetadata(h, b, swarm.MetadataOptions{HaveAll: true})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(seeder.Close)
		lt, _ := childPeer(t, dir).Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: h})
		if err := lt.SetInfoBytes(b); err != nil {
			t.Fatalf("SetInfoBytes = %v; the engine now refuses the piece length: re-check infocheck's maxPieceLength", err)
		}
		lt.DownloadAll()
		lt.AddPeers([]torrent.PeerInfo{seeder.PeerInfo()})
		waitClosed(lt.Closed(), 10*time.Second) // the panic ends the child long before this
		return
	}
	if out, err := runChild(t, "TestPinnedEnginePanicsPlanningPastItsRequestIndex", infoPastTheIndexEnv); err == nil || !strings.Contains(out, "integer divide by zero") {
		t.Errorf("the child ended with %v and no panic, so finding 39 may be fixed: re-check infocheck's piece length and chunk bounds\n%s", err, out)
	}
}

// peerMessageEnv runs TestPinnedEnginePanicsOnAPeersMessage as the child that connects to a hostile
// MetadataPeer and crashes on the message it sends.
const peerMessageEnv = "OCTOPUS_ENGINE_PEER_MESSAGE_CHILD_DIR"

// metadataAttacks are shapes a peer sends that crash the pinned engine when it handles them, apart
// from the metadata it may apply. The gate turns each into something harmless (findings 41–43);
// each child must still crash, so a release that fixes the engine fails this test.
var metadataAttacks = map[string]struct {
	panic string
	size  int
	send  func(send func(pp.Message), utMetadata pp.ExtensionNumber)
}{
	// 42: the extended handshake's metadata_size passes a 32-bit check, then the engine allocates it.
	"a metadata size the engine cannot hold": {"out of range", 1 << 62, nil},
	// 43: a v2 hash message dereferences the torrent's files, nil before the info.
	"a v2 hashes message": {"invalid memory address or nil pointer dereference", 0, func(send func(pp.Message), _ pp.ExtensionNumber) {
		send(pp.Message{Type: pp.Hashes, PiecesRoot: [32]byte{1}})
	}},
	// 41: once a size is set the engine allocates the metadata buffer, then a request for a negative
	// piece slices it from a negative offset.
	"a metadata request for a negative piece": {"index out of range", 1 << 20, func(send func(pp.Message), utMetadata pp.ExtensionNumber) {
		send(pp.Message{Type: pp.Extended, ExtendedID: utMetadata, ExtendedPayload: bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Type: pp.RequestMetadataExtensionMsgType, Piece: -1})})
	}},
}

// Findings 41–43: a peer of a pending magnet ends the application with one message, apart from the
// metadata. Each runs in a child process whose engine dials a hostile MetadataPeer; the gate (in
// Fetch, not here) turns each message into something harmless.
func TestPinnedEnginePanicsOnAPeersMessage(t *testing.T) {
	if dir := childDir(t, peerMessageEnv); dir != "" {
		a := metadataAttacks[os.Getenv(hostileShapeEnv)]
		mi := metaInfo(t, named(t, "v1-single"))
		m, err := swarm.ServeMetadata(mi.HashInfoBytes(), mi.InfoBytes, swarm.MetadataOptions{MetadataSize: a.size, Attack: a.send})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		lt, _ := childPeer(t, dir).Client().AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes()})
		lt.AddPeers([]torrent.PeerInfo{m.PeerInfo()})
		waitClosed(lt.GotInfo(), 10*time.Second) // the crash ends the child long before this
		return
	}
	for name, a := range metadataAttacks {
		out, err := runChild(t, "TestPinnedEnginePanicsOnAPeersMessage", peerMessageEnv, hostileShapeEnv+"="+name)
		if err == nil || !strings.Contains(out, a.panic) {
			t.Errorf("%s: the child ended with %v and no %q, so the engine may be fixed: re-check the gate's callbacks\n%s", name, err, a.panic, out)
		}
	}
}

// Finding 32: the engine adds a torrent to a closed client and never closes it. 011's Fetch drops
// such a torrent itself; 018 checks the client before every add.
func TestPinnedEngineAddsATorrentToAClosedClient(t *testing.T) {
	leecher := peer(t, woken(0))
	client := leecher.Client()
	client.Close()
	lt, isNew := client.AddTorrentInfoHash(metaInfo(t, named(t, "v1-single")).HashInfoBytes())
	if !isNew || waitClosed(lt.Closed(), promptLimit) {
		t.Fatalf("add to a closed client: new %v, closed %v; the engine now refuses or closes it: re-check 011 and 018's notes", isNew, isClosed(lt.Closed()))
	}
	lt.Drop()
}
