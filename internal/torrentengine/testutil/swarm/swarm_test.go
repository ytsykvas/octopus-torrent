package swarm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/testutil/fixture"
)

// Time limits of the scenarios; every wait in this package has one.
const (
	transferLimit = 20 * time.Second
	noPeersLimit  = 2 * time.Second
	closeLimit    = 5 * time.Second
	rateLimit     = 256 << 10 // bytes per second: v1-large then takes about 4 s
)

func TestTransfersArriveByteForByteFromLocalPeersOnly(t *testing.T) {
	for _, name := range []string{"v1-single", "hybrid-single", "v1-tree-no-empty", "hybrid-tree"} {
		t.Run(name, func(t *testing.T) {
			fx := named(t, name)
			seeder, leecher, st, lt := pair(t, fx, Options{})
			if err := WaitComplete(deadline(t, transferLimit), lt); err != nil {
				t.Fatal(err)
			}
			var total int64
			for _, f := range fx.Files {
				total += f.Length
				got, err := os.ReadFile(fx.FilePath(leecher.Dir, f))
				sum := sha256.Sum256(got)
				if err != nil || !bytes.Equal(got, fx.Content(f)) || hex.EncodeToString(sum[:]) != f.SHA256 {
					t.Errorf("%q: %v; equal %v, sha256 %x want %s", f.Path, err, bytes.Equal(got, fx.Content(f)), sum, f.SHA256)
				}
			}
			if got := Count(lt).PayloadRead; got < total {
				t.Errorf("leecher read %d payload bytes, want at least %d", got, total)
			}
			if got := Count(st).PayloadWritten; got < total {
				t.Errorf("seeder wrote %d payload bytes, want at least %d", got, total)
			}
			for _, p := range []*Peer{seeder, leecher} {
				assertOnlyPayload(t, p.Dir, fx)
				if s := p.Settings(); s.DHT || s.Trackers || s.PEX || s.PortForwarding || s.WebSeeds || s.WebTorrent || s.UTP || s.IPv6 {
					t.Errorf("a discovery or transport path is on: %+v", s)
				}
				if bad := NonLoopback(p.Logs()); len(bad) > 0 {
					t.Errorf("the engine logged non-loopback addresses %q", bad)
				}
				if addrs := p.ListenAddrs(); len(addrs) != 1 || addrs[0].Network() != "tcp" || !addrs[0].(*net.TCPAddr).IP.IsLoopback() {
					t.Errorf("listening on %v, want one loopback TCP address", addrs)
				}
			}
			for _, p := range []*Peer{seeder, leecher} {
				addrs := p.SeenAddrs()
				if bad := NonLoopback(strings.Join(addrs, " ")); len(bad) > 0 || len(addrs) == 0 {
					t.Errorf("peers %q, non-loopback %q", addrs, bad)
				}
			}
		})
	}
}

func TestWithoutPeersTheWaitEndsAtItsLimit(t *testing.T) {
	leecher := peer(t, Options{})
	lt, err := leecher.Leech(metaInfo(t, named(t, "v1-single")))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	err = WaitComplete(deadline(t, noPeersLimit), lt)
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > noPeersLimit+time.Second {
		t.Fatalf("WaitComplete = %v after %v, want an IncompleteError at the deadline", err, time.Since(start))
	}
	if incomplete.Completed != 0 || incomplete.Length != lt.Length() || !slices.Equal(incomplete.MissingPieces, []int{0, 1, 2, 3}) {
		t.Errorf("IncompleteError = %+v", incomplete)
	}
	if want := "0 of 57344 bytes complete, missing pieces [0 1 2 3]: context deadline exceeded"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err, want)
	}

	hash := metaInfo(t, named(t, "v1-tree")).HashInfoBytes()
	start = time.Now()
	err = WaitComplete(deadline(t, noPeersLimit), leecher.AddInfohash(hash))
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "no metainfo") || time.Since(start) > noPeersLimit+time.Second {
		t.Fatalf("WaitComplete for a magnet without peers = %v after %v", err, time.Since(start))
	}
}

// The pinned engine's default file IO (mmap) fails to write a chunk that spans a zero-length file
// ("writing received chunk 0: invalid argument") and then disables the download. v1-tree-no-empty,
// the same tree without the empty file, transfers. This test turns red when an engine version fixes it.
func TestPinnedEngineCannotWriteAChunkAcrossAnEmptyFile(t *testing.T) {
	_, leecher, _, lt := pair(t, named(t, "v1-tree"), Options{})
	err := WaitComplete(deadline(t, 3*time.Second), lt)
	var incomplete *IncompleteError
	// Piece 0 always fails; pieces still in flight when the engine disables the download may too.
	if !errors.As(err, &incomplete) || !slices.Contains(incomplete.MissingPieces, 0) {
		t.Fatalf("WaitComplete = %v, want piece 0 (which spans empty.dat) missing", err)
	}
	if logs := leecher.Logs(); !strings.Contains(logs, "writing received chunk 0: invalid argument") {
		t.Errorf("the leecher did not log the chunk write failure:\n%s", logs)
	}
}

func TestACorruptedPieceIsTheOnlyOneMissing(t *testing.T) {
	fx := named(t, "v1-tree-no-empty")
	seeder, leecher := peer(t, Options{}), peer(t, Options{})
	if err := fx.WritePayload(seeder.Dir); err != nil {
		t.Fatal(err)
	}
	if err := fx.CorruptPiece(seeder.Dir, 1); err != nil {
		t.Fatal(err)
	}
	var incomplete *IncompleteError
	st, err := seeder.Seed(deadline(t, transferLimit), metaInfo(t, fx))
	if !errors.As(err, &incomplete) || !slices.Equal(incomplete.MissingPieces, []int{1}) {
		t.Fatalf("Seed = %v, want piece 1 missing", err)
	}
	lt, err := leecher.Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	Connect(lt, seeder)
	for ctx := deadline(t, transferLimit); lt.BytesCompleted() < lt.Length()-fx.PieceLength && ctx.Err() == nil; {
		time.Sleep(10 * time.Millisecond)
	}
	err = WaitComplete(deadline(t, 500*time.Millisecond), lt)
	if !errors.As(err, &incomplete) || !slices.Equal(incomplete.MissingPieces, []int{1}) || incomplete.Completed != lt.Length()-fx.PieceLength {
		t.Fatalf("WaitComplete = %v, want only piece 1 missing", err)
	}
	if st.PieceState(1).Complete {
		t.Error("the seeder claims the corrupted piece")
	}
}

func TestCancellingAWaitReturnsPromptly(t *testing.T) {
	_, _, _, lt := pair(t, named(t, "v1-large"), Options{RateLimit: rateLimit})
	ctx, cancel := context.WithCancel(deadline(t, transferLimit))
	cancelled := make(chan time.Time, 1)
	go func() {
		for Count(lt).PayloadRead == 0 && ctx.Err() == nil {
			time.Sleep(10 * time.Millisecond)
		}
		cancelled <- time.Now()
		cancel()
	}()
	err := WaitComplete(ctx, lt)
	if since := time.Since(<-cancelled); !errors.Is(err, context.Canceled) || since > time.Second {
		t.Fatalf("WaitComplete = %v, %v after the cancel", err, since)
	}
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || incomplete.Completed >= incomplete.Length {
		t.Errorf("IncompleteError = %+v, want a partial transfer", incomplete)
	}
}

func TestTheRecorderMeasuresStartStopAndResidualTraffic(t *testing.T) {
	fx := named(t, "v1-large")
	_, _, st, lt := pair(t, fx, Options{RateLimit: rateLimit})
	rec := Record(50*time.Millisecond, map[string]*torrent.Torrent{"seeder": st, "leecher": lt})
	for ctx := deadline(t, transferLimit); Count(lt).PayloadRead < lt.Length()/4 && ctx.Err() == nil; {
		time.Sleep(10 * time.Millisecond)
	}
	rec.Mark("stop")
	st.DisallowDataUpload()
	time.Sleep(time.Second)
	log := rec.Stop()
	first, ok := log.FirstPayload("leecher")
	stop := log.MarkAt("stop")
	if !ok || first > stop {
		t.Errorf("first payload at %v (%v), stop at %v", first, ok, stop)
	}
	last, _ := log.LastPayload("leecher")
	after, err := log.PayloadAfter("stop", "leecher")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("v1-large at %d B/s: first payload %v, upload disallowed at %v, last payload %v, %d payload bytes after the stop",
		rateLimit, first, stop, last, after)
}

func TestTwoRunsInARowLeaveNoListenerOrGoroutine(t *testing.T) {
	for run := range 2 {
		fx := named(t, "v1-single")
		seeder, err := NewPeer(t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		leecher, err := NewPeer(t.TempDir(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := fx.WritePayload(seeder.Dir); err != nil {
			t.Fatal(err)
		}
		if _, err := seeder.Seed(deadline(t, transferLimit), metaInfo(t, fx)); err != nil {
			t.Fatal(err)
		}
		lt, err := leecher.Leech(metaInfo(t, fx))
		if err != nil {
			t.Fatal(err)
		}
		Connect(lt, seeder)
		if err := WaitComplete(deadline(t, transferLimit), lt); err != nil {
			t.Fatal(err)
		}
		addrs := []net.Addr{seeder.ListenAddrs()[0], leecher.ListenAddrs()[0]}
		for _, p := range []*Peer{seeder, leecher} {
			if err := p.Close(deadline(t, closeLimit)); err != nil {
				t.Fatalf("run %d: %v", run, err)
			}
		}
		for _, a := range addrs {
			if err := Refuses(deadline(t, closeLimit), a); err != nil {
				t.Errorf("run %d: %v", run, err)
			}
		}
		if err := WaitEngineStopped(deadline(t, closeLimit)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
}

func TestEveryFixtureIsAddedAsRecorded(t *testing.T) {
	p := peer(t, Options{})
	for _, fx := range fixture.All() {
		if stage, _ := fx.ParseStage(); stage != "" {
			continue
		}
		tor, err := p.Add(metaInfo(t, fx))
		switch {
		case fx.Engine == fixture.StageAdd && err == nil:
			t.Errorf("%s: added, recorded as rejected on add", fx.Name)
		case fx.Engine == fixture.StageAccepted && err != nil:
			t.Errorf("%s: %v, recorded as accepted", fx.Name, err)
		case err == nil:
			tor.Drop()
		}
		if fx.Format == fixture.V2 && (err == nil || !strings.Contains(err.Error(), "panicked")) {
			t.Errorf("%s: %v, want the pinned engine's panic on a v2-only torrent", fx.Name, err)
		}
	}
}

func TestErrorsAreReturnedNotHidden(t *testing.T) {
	savedClient := newClient
	newClient = func(*torrent.ClientConfig) (*torrent.Client, error) { return nil, errors.New("no socket") }
	if _, err := NewPeer(t.TempDir(), Options{}); err == nil || !strings.Contains(err.Error(), "no socket") {
		t.Errorf("NewPeer = %v", err)
	}
	newClient = savedClient

	p := peer(t, Options{RateLimit: rateLimit})
	if _, err := p.Seed(deadline(t, transferLimit), metaInfo(t, named(t, "v2-single"))); err == nil {
		t.Error("Seed of a torrent the engine refuses succeeded")
	}
	if _, err := p.Leech(metaInfo(t, named(t, "v2-tree"))); err == nil {
		t.Error("Leech of a torrent the engine refuses succeeded")
	}
	var incomplete *IncompleteError
	if _, err := p.Seed(deadline(t, transferLimit), metaInfo(t, named(t, "v1-tree-no-empty"))); !errors.As(err, &incomplete) || len(incomplete.MissingPieces) != 3 {
		t.Errorf("Seed without data = %v, want every piece missing", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Seed(cancelled, metaInfo(t, named(t, "hybrid-single"))); !errors.Is(err, context.Canceled) {
		t.Errorf("Seed with a cancelled context = %v", err)
	}
	if err := WaitEngineStopped(cancelled); err == nil || !strings.Contains(err.Error(), "anacrolix") {
		t.Errorf("WaitEngineStopped with a running client = %v, want its stacks", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := Refuses(deadline(t, 200*time.Millisecond), listener.Addr()); err == nil || !strings.Contains(err.Error(), "still accepts") {
		t.Errorf("Refuses of a live listener = %v", err)
	}
	saved := pieceBusy
	pieceBusy = func(*torrent.Torrent, int) bool { return true }
	_, err = p.Seed(deadline(t, 200*time.Millisecond), metaInfo(t, named(t, "v1-single")))
	pieceBusy = saved
	if err == nil || !strings.Contains(err.Error(), "still being checked") {
		t.Errorf("Seed while pieces stay busy = %v", err)
	}
	if got := NonLoopback("dial 10.0.0.1:6881 127.0.0.1:1 [::1]:2 [2001:db8::1]:3 [zz] v1.61.0"); !slices.Equal(got, []string{"10.0.0.1", "2001:db8::1"}) {
		t.Errorf("NonLoopback = %q", got)
	}
}

func TestLogFunctionsReadSamples(t *testing.T) {
	log := Log{Samples: []Sample{
		{At: 0, Counters: map[string]Counters{"l": {}}},
		{At: 10, Counters: map[string]Counters{"l": {PayloadRead: 5}}},
		{At: 20, Label: "stop", Counters: map[string]Counters{"l": {PayloadRead: 8}}},
		{At: 30, Counters: map[string]Counters{"l": {PayloadRead: 9}}},
		{At: 40, Counters: map[string]Counters{"l": {PayloadRead: 9}}},
	}}
	if at, ok := log.FirstPayload("l"); !ok || at != 10 {
		t.Errorf("FirstPayload = %v %v", at, ok)
	}
	if at, ok := log.LastPayload("l"); !ok || at != 30 {
		t.Errorf("LastPayload = %v %v", at, ok)
	}
	if n, err := log.PayloadAfter("stop", "l"); err != nil || n != 1 {
		t.Errorf("PayloadAfter = %d %v", n, err)
	}
	if _, err := log.PayloadAfter("none", "l"); err == nil {
		t.Error("PayloadAfter of an unknown mark succeeded")
	}
	if at := log.MarkAt("stop"); at != 20 {
		t.Errorf("MarkAt = %v", at)
	}
	if at := log.MarkAt("none"); at != 0 {
		t.Errorf("MarkAt of an unknown label = %v", at)
	}
	if _, ok := log.FirstPayload("x"); ok {
		t.Error("FirstPayload of a peer without payload reported one")
	}
	if _, ok := log.LastPayload("x"); ok {
		t.Error("LastPayload of a peer without payload reported one")
	}
}

// pair starts a seeder with the fixture's payload and a leecher connected to it.
func pair(t *testing.T, fx fixture.Fixture, opts Options) (seeder, leecher *Peer, st, lt *torrent.Torrent) {
	t.Helper()
	seeder, leecher = peer(t, opts), peer(t, opts)
	if err := fx.WritePayload(seeder.Dir); err != nil {
		t.Fatal(err)
	}
	st, err := seeder.Seed(deadline(t, transferLimit), metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	if lt, err = leecher.Leech(metaInfo(t, fx)); err != nil {
		t.Fatal(err)
	}
	Connect(lt, seeder)
	return seeder, leecher, st, lt
}

// peer starts a peer in a fresh temporary directory and closes it when the test ends.
func peer(t *testing.T, opts Options) *Peer {
	t.Helper()
	p, err := NewPeer(t.TempDir(), opts)
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

// assertOnlyPayload fails unless dir holds exactly the fixture's files: no part files, no piece
// completion database, nothing else.
func assertOnlyPayload(t *testing.T, dir string, fx fixture.Fixture) {
	t.Helper()
	var want, got []string
	for _, f := range fx.Files {
		want = append(want, fx.FilePath(dir, f))
	}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			got = append(got, path)
		}
		return err
	})
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %q, want exactly %q", dir, got, want)
	}
}
