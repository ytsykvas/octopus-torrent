package torrentengine_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/chansync/events"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

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
		lie, _, err := liar.Client().AddTorrentSpec(spec)
		if err != nil {
			t.Fatal(err)
		}
		lie.DownloadAll() // a torrent that wants nothing accepts no connection
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

func TestPinnedEngineDeadlocksOnVerifyAfterDrop(t *testing.T) {
	if dir := os.Getenv(childDirEnv); dir != "" {
		// a folder outside the temporary folder (a variable left in the developer's shell) is refused
		// before the child writes anything into it
		if !strings.HasPrefix(dir, filepath.Clean(os.TempDir())+string(filepath.Separator)) {
			t.Fatalf("%s=%q is not under the temporary folder %s", childDirEnv, dir, os.TempDir())
		}
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
