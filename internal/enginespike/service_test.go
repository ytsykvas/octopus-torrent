package enginespike

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

func TestMain(m *testing.M) { os.Exit(swarm.Main(m)) }

const (
	transferLimit = 20 * time.Second
	closeLimit    = 5 * time.Second
	oneMiB        = 1 << 20
)

// events records every Transfer the service emits, in order.
type events struct {
	mu   sync.Mutex
	list []Transfer
}

func (e *events) emit(ctx context.Context, name string, data ...any) {
	if ctx == nil || name != Event || len(data) != 1 {
		panic("emit called with a nil context, another event or no Transfer")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, data[0].(Transfer))
}

func (e *events) all() []Transfer {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Transfer(nil), e.list...)
}

func (e *events) last() Transfer {
	all := e.all()
	if len(all) == 0 {
		return Transfer{}
	}
	return all[len(all)-1]
}

// await polls the emitted events until one satisfies ok, or fails the test at the limit.
func (e *events) await(t *testing.T, limit time.Duration, what string, ok func(Transfer) bool) Transfer {
	t.Helper()
	end := time.Now().Add(limit)
	for time.Now().Before(end) {
		for _, tr := range e.all() {
			if ok(tr) {
				return tr
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no event with %s within %v; last: %+v", what, limit, e.last())
	return Transfer{}
}

func replace[T any](t *testing.T, seam *T, value T) {
	saved := *seam
	*seam = value
	t.Cleanup(func() { *seam = saved })
}

// started returns a service after Startup, shut down when the test ends, transferring at rate.
func started(t *testing.T, rate int, newPeer NewPeer) (*Service, *events) {
	t.Helper()
	replace(t, &rateLimit, rate)
	ev := &events{}
	s := New(ev.emit, newPeer)
	s.Startup(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), closeLimit)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return s, ev
}

func TestStartTransfersRealBytesAndPublishesEveryChange(t *testing.T) {
	s, ev := started(t, oneMiB, swarm.NewPeer)
	api := s.API()
	first, err := api.Start()
	if err != nil || first.State != Running || first.ClientsStarted != 2 || first.Length != oneMiB {
		t.Fatalf("Start = %+v, %v; want running with two clients and 1 MiB", first, err)
	}
	done := ev.await(t, transferLimit, "state complete", func(tr Transfer) bool { return tr.State == Complete })
	if done.BytesCompleted != oneMiB || done.PayloadRead < oneMiB || done.Error != "" {
		t.Errorf("complete event %+v; want all 1048576 bytes read from the seeder", done)
	}
	var previous Transfer
	for i, tr := range ev.all() {
		if i > 0 && (tr.Revision <= previous.Revision || tr.BytesCompleted < previous.BytesCompleted) {
			t.Fatalf("event %d %+v after %+v: revisions must rise and bytes never fall", i, tr, previous)
		}
		previous = tr
	}
	time.Sleep(3 * tickInterval)
	if after := ev.last(); after.Revision != done.Revision {
		t.Errorf("the ticker kept publishing after completion: %+v", after)
	}
	if got := api.Snapshot(); got != done {
		t.Errorf("Snapshot = %+v, want the last published %+v", got, done)
	}
}

func TestStartingTwiceKeepsOneRun(t *testing.T) {
	s, _ := started(t, 64<<10, swarm.NewPeer)
	if _, err := s.API().Start(); err != nil {
		t.Fatal(err)
	}
	again, err := s.API().Start()
	if err != nil || again.ClientsStarted != 2 || again.State != Running {
		t.Errorf("second Start = %+v, %v; want the same run with two clients", again, err)
	}
}

func TestPauseHoldsTheBytesAndResumeCompletesTheTransfer(t *testing.T) {
	s, ev := started(t, 256<<10, swarm.NewPeer)
	api := s.API()
	if _, err := api.Start(); err != nil {
		t.Fatal(err)
	}
	ev.await(t, transferLimit, "payload", func(tr Transfer) bool { return tr.BytesCompleted > 0 })
	paused, err := api.Pause()
	if err != nil || paused.State != Paused {
		t.Fatalf("Pause = %+v, %v", paused, err)
	}
	time.Sleep(500 * time.Millisecond) // chunks already on the wire may still land
	held := ev.last().BytesCompleted
	time.Sleep(time.Second)
	if now := ev.last(); now.BytesCompleted != held || now.State != Paused {
		t.Errorf("while paused the bytes moved from %d to %+v", held, now)
	}
	if resumed, err := api.Resume(); err != nil || resumed.State != Running {
		t.Fatalf("Resume = %+v, %v", resumed, err)
	}
	ev.await(t, transferLimit, "state complete", func(tr Transfer) bool { return tr.State == Complete })
}

// A repeated Pause must not reach the engine twice (engine finding 12): the transfer still completes
// after Resume.
func TestARepeatedPauseStillResumes(t *testing.T) {
	s, ev := started(t, 256<<10, swarm.NewPeer)
	api := s.API()
	if _, err := api.Start(); err != nil {
		t.Fatal(err)
	}
	ev.await(t, transferLimit, "payload", func(tr Transfer) bool { return tr.BytesCompleted > 0 })
	for range 2 {
		if paused, err := api.Pause(); err != nil || paused.State != Paused {
			t.Fatalf("Pause = %+v, %v", paused, err)
		}
	}
	if _, err := api.Resume(); err != nil {
		t.Fatal(err)
	}
	ev.await(t, transferLimit, "state complete", func(tr Transfer) bool { return tr.State == Complete })
}

func TestCommandsAreAppliedInTheOrderOfTheirRevisions(t *testing.T) {
	s, ev := started(t, 256<<10, swarm.NewPeer)
	api := s.API()
	if _, err := api.Start(); err != nil {
		t.Fatal(err)
	}
	ev.await(t, transferLimit, "payload", func(tr Transfer) bool { return tr.BytesCompleted > 0 })
	var mu sync.Mutex
	var replies []Transfer
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			command := api.Pause
			if i%2 == 1 {
				command = api.Resume
			}
			tr, err := command()
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			replies = append(replies, tr)
			mu.Unlock()
		}()
	}
	wg.Wait()
	seen, latest := map[contracts.Revision]bool{}, Transfer{}
	for _, tr := range replies {
		if seen[tr.Revision] {
			t.Fatalf("two replies share revision %d", tr.Revision)
		}
		seen[tr.Revision] = true
		if tr.Revision > latest.Revision {
			latest = tr
		}
	}
	// Ticks may follow the last command, but they never change its state.
	if got := api.Snapshot(); got.Revision < latest.Revision || got.State != latest.State {
		t.Errorf("Snapshot %+v is older than the last command's reply %+v", got, latest)
	}
	if resumed, err := api.Resume(); err != nil || resumed.Revision <= latest.Revision {
		t.Fatalf("Resume = %+v, %v", resumed, err)
	}
	ev.await(t, transferLimit, "state complete", func(tr Transfer) bool { return tr.State == Complete })
}

func TestShutdownDuringATransferReleasesOnlyTheSpikesResources(t *testing.T) {
	var dir string
	replace(t, &mkdirTemp, func(parent, pattern string) (string, error) {
		d, err := os.MkdirTemp(parent, pattern)
		dir = d
		return d, err
	})
	s, ev := started(t, 64<<10, swarm.NewPeer)
	if _, err := s.API().Start(); err != nil {
		t.Fatal(err)
	}
	ev.await(t, transferLimit, "payload", func(tr Transfer) bool { return tr.BytesCompleted > 0 })
	addrs := append(s.seeder.ListenAddrs(), s.leecher.ListenAddrs()...)

	ctx, cancel := context.WithTimeout(context.Background(), closeLimit)
	defer cancel()
	begin := time.Now()
	if err := s.Shutdown(ctx); err != nil || time.Since(begin) > closeLimit {
		t.Fatalf("Shutdown = %v after %v", err, time.Since(begin))
	}
	count := len(ev.all())
	time.Sleep(3 * tickInterval)
	if len(ev.all()) != count {
		t.Error("an event was emitted after Shutdown returned")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the spike's directory %s is still there: %v", dir, err)
	}
	for _, a := range addrs {
		if c, err := net.Dial("tcp", a.String()); err == nil {
			c.Close()
			t.Errorf("%s still accepts connections", a)
		}
	}
	if err := swarm.WaitEngineStopped(ctx); err != nil {
		t.Error(err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("a second Shutdown = %v, want a no-op", err)
	}
	if _, err := s.API().Start(); !errors.Is(err, errStopped) {
		t.Errorf("Start after Shutdown = %v, want %v", err, errStopped)
	}
}

func TestCommandsOutsideARunAreRefused(t *testing.T) {
	s := New((&events{}).emit, swarm.NewPeer)
	if _, err := s.API().Start(); !errors.Is(err, errNotStarted) {
		t.Errorf("Start before Startup = %v, want %v", err, errNotStarted)
	}
	s.Startup(context.Background())
	for name, command := range map[string]func() (Transfer, error){"Pause": s.API().Pause, "Resume": s.API().Resume} {
		if _, err := command(); !errors.Is(err, errNotRunning) {
			t.Errorf("%s before Start = %v, want %v", name, err, errNotRunning)
		}
	}
	if got := s.API().Snapshot(); got.State != Idle || got.Revision != 0 {
		t.Errorf("Snapshot before Start = %+v, want idle at revision 0", got)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown of an idle service = %v", err)
	}
}

func TestAFailedStartReleasesWhatItCreatedAndCanBeRetried(t *testing.T) {
	failing := func(after int) NewPeer {
		calls := 0
		return func(dir string, opts swarm.Options) (*swarm.Peer, error) {
			if calls++; calls > after {
				return nil, errors.New("no more peers")
			}
			return swarm.NewPeer(dir, opts)
		}
	}
	cases := map[string]struct {
		newPeer NewPeer
		setup   func(t *testing.T)
		want    string
		clients int
	}{
		"seeder fails":  {failing(0), func(*testing.T) {}, "no more peers", 0},
		"leecher fails": {failing(1), func(*testing.T) {}, "no more peers", 1},
		"no temporary folder": {swarm.NewPeer, func(t *testing.T) {
			replace(t, &mkdirTemp, func(string, string) (string, error) { return "", errors.New("disk full") })
		}, "disk full", 0},
		"payload not written": {swarm.NewPeer, func(t *testing.T) {
			replace(t, &writePayload, func(fixture.Fixture, string) error { return errors.New("read-only") })
		}, "read-only", 0},
		"unknown fixture":            {swarm.NewPeer, func(t *testing.T) { replace(t, &fixtureName, "missing") }, "missing", 0},
		"unreadable metainfo":        {swarm.NewPeer, func(t *testing.T) { replace(t, &fixtureName, "invalid-truncated") }, "read the metainfo of invalid-truncated", 0},
		"engine refuses the torrent": {swarm.NewPeer, func(t *testing.T) { replace(t, &fixtureName, "v2-single") }, "add the torrent to the leecher", 2},
		"seeder has no payload":      {swarm.NewPeer, func(t *testing.T) { replace(t, &writePayload, func(fixture.Fixture, string) error { return nil }) }, "incomplete", 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var dirs []string
			replace(t, &mkdirTemp, func(parent, pattern string) (string, error) {
				d, err := os.MkdirTemp(parent, pattern)
				dirs = append(dirs, d)
				return d, err
			})
			c.setup(t)
			s, ev := started(t, oneMiB, c.newPeer)
			got, err := s.API().Start()
			if err == nil || !strings.Contains(err.Error(), c.want) || got.State != Failed || !strings.Contains(got.Error, c.want) || got.ClientsStarted != c.clients {
				t.Fatalf("Start = %+v, %v; want failed with %q and %d clients", got, err, c.want, c.clients)
			}
			if ev.last() != got {
				t.Errorf("the failure was not published: last event %+v", ev.last())
			}
			for _, d := range dirs {
				if _, err := os.Stat(d); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s was left behind: %v", d, err)
				}
			}
			if err := swarm.WaitEngineStopped(context.Background()); err != nil {
				t.Errorf("a client of the failed start is still running: %v", err)
			}
		})
	}

	t.Run("retry", func(t *testing.T) {
		s, ev := started(t, oneMiB, failing(1))
		if _, err := s.API().Start(); err == nil {
			t.Fatal("the first Start did not fail")
		}
		replace(t, &s.newPeer, swarm.NewPeer)
		if got, err := s.API().Start(); err != nil || got.State != Running || got.Error != "" || got.ClientsStarted != 3 {
			t.Fatalf("retry = %+v, %v; want running, the error cleared, three clients in all", got, err)
		}
		ev.await(t, transferLimit, "state complete", func(tr Transfer) bool { return tr.State == Complete })
	})
}

func TestTheSharedExampleIsATransferAsGoWritesIt(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "transfer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Event    string          `json:"event"`
		Transfer json.RawMessage `json:"transfer"`
	}
	if err := json.Unmarshal(data, &example); err != nil || example.Event != Event {
		t.Fatalf("testdata/transfer.json: event %q, %v; want %q", example.Event, err, Event)
	}
	tr := Transfer{Revision: 7, State: Paused, BytesCompleted: 524288, Length: oneMiB, PayloadRead: 540672,
		PayloadWritten: 0, Peers: 1, ClientsStarted: 2, Error: ""}
	want, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	var got, expected any
	_ = json.Unmarshal(example.Transfer, &got)
	_ = json.Unmarshal(want, &expected)
	if gotText, _ := json.Marshal(got); string(gotText) != string(mustJSON(t, expected)) {
		t.Errorf("testdata/transfer.json transfer = %s, Go writes %s", example.Transfer, want)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
