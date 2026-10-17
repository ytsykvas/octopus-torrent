package swarm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

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
				if tripped := p.Tripped(); tripped != "" {
					t.Errorf("the engine tried paths outside the swarm:\n%s", tripped)
				}
				if bad := NonLoopback(p.Logs()); len(bad) > 0 {
					t.Errorf("the engine logged non-loopback addresses %q", bad)
				}
				if who := personal(t); strings.Contains(p.Logs(), who[0]) || strings.Contains(p.Logs(), "/"+who[1]+"/") {
					t.Errorf("the engine log names the home folder %q or the user %q", who[0], who[1])
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

// The seeder verified its data, then piece 1 changed on its disk: it serves bad bytes. The leecher
// must never accept them. Connect marks peers trusted (all share 127.0.0.1, and the engine bans by
// IP), so the leecher re-requests the piece instead of banning the seeder.
func TestACorruptedPieceReceivedOverTheWireIsNeverAccepted(t *testing.T) {
	fx := named(t, "v1-tree-no-empty")
	seeder, leecher := peer(t, Options{}), peer(t, Options{})
	if err := fx.WritePayload(seeder.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := seeder.Seed(deadline(t, transferLimit), metaInfo(t, fx)); err != nil {
		t.Fatal(err)
	}
	if err := fx.CorruptPiece(seeder.Dir, 1); err != nil {
		t.Fatal(err)
	}
	lt, err := leecher.Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	Connect(lt, seeder)
	for ctx := deadline(t, transferLimit); !slices.Equal(missingPieces(lt), []int{1}) && ctx.Err() == nil; {
		time.Sleep(10 * time.Millisecond)
	}
	err = WaitComplete(deadline(t, 500*time.Millisecond), lt)
	var incomplete *IncompleteError
	if !errors.As(err, &incomplete) || !slices.Equal(incomplete.MissingPieces, []int{1}) {
		t.Fatalf("WaitComplete = %v, want only piece 1 missing", err)
	}
	if !strings.Contains(leecher.Logs(), "piece 1 failed hash") {
		t.Error("the leecher did not log a hash failure of piece 1")
	}
}

// The pinned engine's default file IO maps payload files and never unmaps them: after Close, and
// after every engine goroutine ended, the files stay mapped in the process. This test turns red
// when an engine version releases them.
func TestPinnedEngineKeepsPayloadFilesMappedAfterClose(t *testing.T) {
	fx := named(t, "v1-single")
	seeder, leecher, _, lt := pair(t, fx, Options{})
	if err := WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	for _, p := range []*Peer{seeder, leecher} {
		if err := p.Close(deadline(t, closeLimit)); err != nil {
			t.Fatal(err)
		}
	}
	if err := WaitEngineStopped(deadline(t, closeLimit)); err != nil {
		t.Fatal(err)
	}
	if open := openUnder(t, seeder.Dir); len(open) == 0 {
		t.Error("no payload file of the closed seeder is open or mapped any more: the engine now releases them")
	}
}

// On add the pinned engine creates every zero-length file with O_TRUNC, and when a piece fails its
// check it renames each existing file of that piece to "<name>.part" (the default part-file mode).
// An existing file at a torrent path therefore loses its name, and at a zero-length path its data
// too, beside the torrent folder as well (one ".." level, an embedded separator). The waits for the
// piece checks make the outcome deterministic; without them the rename races the assertions.
func TestPinnedEngineTruncatesAndRenamesExistingFilesAtTorrentPathsOnAdd(t *testing.T) {
	p := peer(t, Options{})
	unrelated := filepath.Join(p.Dir, "tree", "docs", "readme.txt") // a file of v1-tree, not zero-length
	for name, victim := range map[string]string{
		"v1-tree":                    filepath.Join(p.Dir, "tree", "empty.dat"),
		"invalid-empty-traversal":    filepath.Join(p.Dir, "victim.txt"),
		"invalid-embedded-separator": filepath.Join(p.Dir, "x"),
	} {
		for _, path := range []string{victim, unrelated} {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("synthetic data that must survive"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		tor, err := p.Add(metaInfo(t, named(t, name)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := Settle(deadline(t, closeLimit), tor); err != nil {
			t.Fatal(err)
		}
		tor.Drop()
		_, gone := os.Stat(victim)
		if info, err := os.Stat(victim + ".part"); !errors.Is(gone, os.ErrNotExist) || err != nil || info.Size() != 0 {
			t.Errorf("%s: %s should be gone (%v) and %s.part empty (%v, %v): the engine changed, re-check 019, 020 and 023's notes", name, victim, gone, victim, info, err)
		}
	}
	_, gone := os.Stat(unrelated)
	if data, err := os.ReadFile(unrelated + ".part"); !errors.Is(gone, os.ErrNotExist) || err != nil || string(data) != "synthetic data that must survive" {
		t.Errorf("%s should be renamed to .part with its bytes (%v): %q, %v; the engine changed, re-check 020 and 023's notes", unrelated, gone, data, err)
	}
}

// Every path out of the closed swarm is refused and recorded, whatever reaches the engine.
func TestTripwiresRefuseEveryPathOutOfTheSwarm(t *testing.T) {
	p := peer(t, Options{})
	c := p.config
	_, dial := c.HTTPDialContext(context.Background(), "tcp", "example.com:80")
	_, tracker := c.TrackerDialContext(context.Background(), "tcp", "tracker.example:6969")
	_, packet := c.TrackerListenPacket("udp", ":0")
	_, lookup := c.LookupTrackerIp(&url.URL{Host: "tracker.example"})
	_, web := c.WebTransport.RoundTrip(httptest.NewRequest("GET", "http://seed.example/file", nil))
	for _, err := range []error{dial, tracker, packet, lookup, web} {
		if err == nil || !strings.Contains(err.Error(), "is not allowed") {
			t.Errorf("tripwire = %v, want a refusal", err)
		}
	}
	for _, want := range []string{"example.com:80", "tracker.example:6969", "tracker socket", "tracker lookup", "seed.example"} {
		if !strings.Contains(p.Tripped(), want) {
			t.Errorf("Tripped() lacks %q:\n%s", want, p.Tripped())
		}
	}
	if _, blocked := c.IPBlocklist.Lookup(net.ParseIP("203.0.113.7")); !blocked || c.IPBlocklist.NumRanges() != 1 {
		t.Error("a public address is not blocked")
	}
	if _, blocked := c.IPBlocklist.Lookup(net.ParseIP("127.0.0.1")); blocked {
		t.Error("loopback is blocked")
	}
	mi := metaInfo(t, named(t, "v1-single"))
	mi.Announce = "http://tracker.example/announce"
	if _, err := p.Add(mi); err == nil || !strings.Contains(err.Error(), "no tracker") {
		t.Errorf("Add with a tracker = %v", err)
	}
	// A magnet's peer in another network is blocked by the list, never dialled.
	tor := p.AddInfohash(metaInfo(t, named(t, "v1-tree")).HashInfoBytes())
	tor.AddPeers([]torrent.PeerInfo{{Addr: torrent.StringAddr("203.0.113.7:6881"), Trusted: true}})
	if addrs := p.SeenAddrs(); len(addrs) != 0 {
		t.Errorf("the engine created peers %q outside loopback", addrs)
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

func TestTheRecorderMeasuresARateLimitedTransferAndItsResidualTraffic(t *testing.T) {
	fx := named(t, "v1-large")
	_, _, st, lt := pair(t, fx, Options{RateLimit: rateLimit})
	rec := Record(50*time.Millisecond, map[string]*torrent.Torrent{"seeder": st, "leecher": lt})
	t.Cleanup(func() { rec.Stop() })
	for ctx := deadline(t, transferLimit); Count(lt).PayloadRead < lt.Length()/4 && ctx.Err() == nil; {
		time.Sleep(10 * time.Millisecond)
	}
	rec.Mark("stop")
	st.DisallowDataUpload()
	time.Sleep(time.Second)
	log := rec.Stop()
	if again := rec.Stop(); len(again.Samples) != len(log.Samples) {
		t.Errorf("a second Stop changed the log: %d samples, then %d", len(log.Samples), len(again.Samples))
	}
	first, ok := log.FirstPayload("leecher")
	stop := log.MarkAt("stop")
	// A quarter of 1 MiB at 256 KiB/s takes about 1 s; well under 0.5 s means the limit does not hold.
	if !ok || first > stop || stop < 500*time.Millisecond {
		t.Errorf("first payload at %v (%v), a quarter of the file at %v", first, ok, stop)
	}
	last, _ := log.LastPayload("leecher")
	after, err := log.PayloadAfter("stop", "leecher")
	if err != nil || after < 0 || last > log.Samples[len(log.Samples)-1].At {
		t.Fatalf("PayloadAfter = %d, %v; last payload %v", after, err, last)
	}
	t.Logf("v1-large at %d B/s: first payload %v, upload disallowed at %v, last payload %v, %d payload bytes after the stop",
		rateLimit, first, stop, last, after)
}

func TestTwoRunsInARowLeaveNoListenerOrGoroutine(t *testing.T) {
	for run := range 2 {
		fx := named(t, "v1-single")
		// Closed explicitly below; the cleanup only matters when the run fails before that.
		seeder, leecher := peer(t, Options{}), peer(t, Options{})
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
	t.Run("the engine does not start", func(t *testing.T) {
		replace(t, &newClient, func(*torrent.ClientConfig) (*torrent.Client, error) { return nil, errors.New("no socket") })
		if _, err := NewPeer(t.TempDir(), Options{}); err == nil || !strings.Contains(err.Error(), "no socket") {
			t.Errorf("NewPeer = %v", err)
		}
	})
	t.Run("pieces stay busy", func(t *testing.T) {
		p := peer(t, Options{})
		replace(t, &pieceBusy, func(*torrent.Torrent, int) bool { return true })
		if _, err := p.Seed(deadline(t, 200*time.Millisecond), metaInfo(t, named(t, "v1-single"))); err == nil || !strings.Contains(err.Error(), "still being checked") {
			t.Errorf("Seed while pieces stay busy = %v", err)
		}
	})
	t.Run("the engine refuses or cannot verify", func(t *testing.T) {
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
	})
	t.Run("an address still accepts", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		if err := Refuses(deadline(t, 200*time.Millisecond), listener.Addr()); err == nil || !strings.Contains(err.Error(), "still accepts") {
			t.Errorf("Refuses of a live listener = %v", err)
		}
	})
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

// personal returns the owner's home folder and user name, which no engine log may contain.
func personal(t *testing.T) [2]string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return [2]string{home, filepath.Base(home)}
}

// openUnder lists the files under dir that this process has open or mapped, from lsof.
func openUnder(t *testing.T, dir string) []string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("lsof", "-n", "-P", "-p", strconv.Itoa(os.Getpid()), "-Fn").Output()
	if err != nil {
		t.Fatalf("lsof: %v", err)
	}
	var open []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n"+real+"/") {
			open = append(open, line[1:])
		}
	}
	return open
}

// replace sets a test seam for the rest of the test and restores it when the test ends, even if it fails.
func replace[T any](t *testing.T, seam *T, value T) {
	saved := *seam
	*seam = value
	t.Cleanup(func() { *seam = saved })
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
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			got = append(got, path)
		}
		return err
	}); err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("%s holds %q, want exactly %q", dir, got, want)
	}
}

// recordingStorage is a storage that only records which torrents were opened in it, and refuses
// them: the engine's own error is the proof that the peer used it.
type recordingStorage struct{ opened []string }

func (r *recordingStorage) OpenTorrent(_ context.Context, info *metainfo.Info, _ metainfo.Hash) (storage.TorrentImpl, error) {
	r.opened = append(r.opened, info.BestName())
	return storage.TorrentImpl{}, errors.New("the recording storage stores nothing")
}

func TestAPeerCanTakeItsDefaultStorageAndItsDataDirFromTheCaller(t *testing.T) {
	data := t.TempDir()
	rec := &recordingStorage{}
	p := peer(t, Options{DefaultStorage: rec, DataDir: data})
	if p.config.DataDir != data {
		t.Fatalf("DataDir = %q, want %q", p.config.DataDir, data)
	}
	if got := p.Settings().Storage; !strings.Contains(got, "given by the caller") {
		t.Fatalf("Settings.Storage = %q", got)
	}
	if _, err := p.Add(metaInfo(t, named(t, "v1-single"))); err == nil {
		t.Fatal("a torrent added to a refusing default storage was accepted")
	}
	// The engine opens the storage once per call that sets the info; how often is its own business.
	if len(rec.opened) == 0 || rec.opened[0] == "" {
		t.Fatalf("the recording storage saw %v", rec.opened)
	}
}

func TestClientIsTheEngineClientOfThePeer(t *testing.T) {
	p := peer(t, Options{})
	if p.Client() == nil || p.Client() != p.client {
		t.Fatal("Client does not return the peer's engine client")
	}
}
