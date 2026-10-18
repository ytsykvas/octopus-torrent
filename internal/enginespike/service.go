package enginespike

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
	"octopustorrent/internal/torrentengine/transferswitch"
)

// Seams for the tests.
var (
	// fixtureName is what the spike transfers: 1 MiB in 64 pieces.
	fixtureName = "v1-large"
	// rateLimit caps each client so the progress is visible: 1 MiB at 64 KiB/s takes about 16 s.
	rateLimit    = 64 << 10
	tickInterval = 250 * time.Millisecond
	seedLimit    = 20 * time.Second
	// releaseLimit bounds the clean-up of a failed Start, which no caller's context covers.
	releaseLimit = 5 * time.Second
	mkdirTemp    = os.MkdirTemp
	writePayload = fixture.Fixture.WritePayload
)

// The peers' folders inside the spike's own folder.
const (
	seederDir  = "seeder"
	leecherDir = "leecher"
)

var (
	errNotStarted = errors.New("the spike has not started yet")
	errStopped    = errors.New("the spike is stopped")
	errNotRunning = errors.New("no transfer is running")
)

// Emit publishes a Wails event: runtime.EventsEmit in the app.
type Emit func(ctx context.Context, name string, data ...any)

// NewPeer starts one engine client of the loopback swarm: swarm.NewPeer in the app.
type NewPeer func(dir string, opts swarm.Options) (*swarm.Peer, error)

// Service owns the spike's lifecycle: its two engine clients, its temporary folder and the ticker
// that publishes progress. Wails calls Startup and Shutdown; only API is bound to JavaScript, so the
// lifecycle never becomes callable from the page.
//
// One mutex orders everything: Start, Pause, Resume, every tick and Shutdown. A command changes the
// engine and takes the next revision in the same critical section, so the order of the revisions is
// the order in which the commands reached the engine. Start holds the mutex while it creates the run,
// which takes as long as the seeder's piece check; Shutdown ends the service's lifetime context
// before it waits for the mutex, so a Start in progress gives up instead of delaying the quit.
type Service struct {
	emit    Emit
	newPeer NewPeer
	life    context.Context // ends when Shutdown begins; bounds every engine wait of a Start
	end     context.CancelFunc

	mu      sync.Mutex
	ctx     context.Context // from Startup; nil before it
	stopped bool
	state   Transfer
	dir     string // created by this service; the only folder it ever removes
	seeder  *swarm.Peer
	leecher *swarm.Peer
	st      *torrent.Torrent // the seeder's torrent: Pause and Resume switch its upload
	lt      *torrent.Torrent // the leecher's torrent: the published progress
	stop    chan struct{}    // closes the ticker
	ticking chan struct{}    // closed when the ticker has returned

	shutdown sync.Once
	closed   error
}

// New returns an idle service.
func New(emit Emit, newPeer NewPeer) *Service {
	life, end := context.WithCancel(context.Background())
	return &Service{emit: emit, newPeer: newPeer, life: life, end: end, state: Transfer{State: Idle}}
}

// API is the only type bound to JavaScript.
func (s *Service) API() *API {
	return &API{s: s}
}

// Startup keeps the Wails context for events; Wails calls it before the page can call the API.
func (s *Service) Startup(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

// Shutdown stops the ticker and waits for it, closes both engine clients (each waits until its
// address refuses connections) and removes the spike's own folder. It runs once; later calls return
// the first result. Nothing is published after it returns, and Start is refused.
func (s *Service) Shutdown(ctx context.Context) error {
	s.shutdown.Do(func() {
		s.end()
		s.mu.Lock()
		s.stopped = true
		stop, ticking := s.stop, s.ticking
		s.mu.Unlock()
		if stop != nil {
			close(stop)
			<-ticking
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closed = s.release(ctx)
	})
	return s.closed
}

// release closes what a run created and removes its folder; the caller holds mu. Only the two peer
// folders are removed recursively; the folder itself must then be empty, so a wrong s.dir could
// never cost more than those two names.
func (s *Service) release(ctx context.Context) error {
	var errs []error
	for _, p := range []*swarm.Peer{s.leecher, s.seeder} {
		if p != nil {
			errs = append(errs, p.Close(ctx))
		}
	}
	if s.dir != "" {
		for _, sub := range []string{seederDir, leecherDir} {
			errs = append(errs, os.RemoveAll(filepath.Join(s.dir, sub)))
		}
		errs = append(errs, os.Remove(s.dir))
	}
	s.leecher, s.seeder, s.st, s.lt, s.dir = nil, nil, nil, nil, ""
	return errors.Join(errs...)
}

func (s *Service) start() (Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.stopped:
		return s.state, errStopped
	case s.ctx == nil:
		return s.state, errNotStarted
	case s.state.State != Idle && s.state.State != Failed:
		return s.state, nil // one run per service: a reloaded page gets the same run
	}
	if err := s.launch(); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), releaseLimit)
		defer cancel()
		err = errors.Join(err, s.release(ctx))
		s.state.State, s.state.Error = Failed, err.Error()
		s.publish()
		return s.state, err
	}
	s.state.State, s.state.Error = Running, ""
	s.publish()
	s.stop, s.ticking = make(chan struct{}), make(chan struct{})
	go s.tick(s.stop, s.ticking)
	return s.state, nil
}

// launch creates the folder, the payload and both clients, adds the torrent to both and connects
// them; the caller holds mu and releases whatever launch created when it fails.
func (s *Service) launch() error {
	fx, err := fixture.Named(fixtureName)
	if err != nil {
		return err
	}
	mi, err := fx.MetaInfo()
	if err != nil {
		return fmt.Errorf("read the metainfo of %s: %w", fixtureName, err)
	}
	dir, err := mkdirTemp("", "octopus-engine-spike-*")
	if err != nil {
		return fmt.Errorf("create the spike's folder: %w", err)
	}
	s.dir = dir
	seedDir, leechDir := filepath.Join(dir, seederDir), filepath.Join(dir, leecherDir)
	if err := writePayload(fx, seedDir); err != nil {
		return fmt.Errorf("write the payload: %w", err)
	}
	// The product's keep-alive interval bounds a lost writer wake-up (engine findings 14 and 22).
	opts := swarm.Options{RateLimit: rateLimit, KeepAliveTimeout: transferswitch.WakeInterval}
	if s.seeder, err = s.newPeer(seedDir, opts); err != nil {
		return fmt.Errorf("start the seeder: %w", err)
	}
	s.state.ClientsStarted++
	if s.leecher, err = s.newPeer(leechDir, opts); err != nil {
		return fmt.Errorf("start the leecher: %w", err)
	}
	s.state.ClientsStarted++
	if s.lt, err = s.leecher.Leech(mi); err != nil {
		return fmt.Errorf("add the torrent to the leecher: %w", err)
	}
	ctx, cancel := context.WithTimeout(s.life, seedLimit)
	defer cancel()
	if s.st, err = s.seeder.Seed(ctx, mi); err != nil {
		return fmt.Errorf("seed the payload: %w", err)
	}
	swarm.Connect(s.lt, s.seeder)
	return nil
}

// switchUpload is the seeder's upload switch of the pinned engine.
func switchUpload(t *torrent.Torrent, allowed bool) {
	if allowed {
		t.AllowDataUpload()
	} else {
		t.DisallowDataUpload()
	}
}

// command applies the seeder's upload switch and publishes the result. The engine is called only
// when the state changes. The leecher's download switch is not used: allowing download right after
// disallowing it stalls the pinned engine's transfer (engine finding 12; the product resumes with
// transferswitch.ResumeDownload). A stall after the upload switch (finding 14) ends within the
// clients' keep-alive interval.
func (s *Service) command(next State) (Transfer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped || (s.state.State != Running && s.state.State != Paused) {
		return s.state, errNotRunning
	}
	if s.state.State != next {
		switchUpload(s.st, next == Running)
	}
	s.state.State = next
	s.publish()
	return s.state, nil
}

// tick publishes the progress until the transfer completes or the service stops.
func (s *Service) tick(stop <-chan struct{}, ticking chan<- struct{}) {
	defer close(ticking)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.publish()
			done := s.state.State == Complete
			s.mu.Unlock()
			if done {
				return
			}
		}
	}
}

// publish reads the engine, takes the next revision and emits it; the caller holds mu.
func (s *Service) publish() {
	if s.lt != nil {
		c := swarm.Count(s.lt)
		s.state.BytesCompleted = contracts.ByteCount(s.lt.BytesCompleted())
		s.state.Length = contracts.ByteCount(s.lt.Length())
		s.state.PayloadRead, s.state.PayloadWritten = contracts.ByteCount(c.PayloadRead), contracts.ByteCount(c.PayloadWritten)
		s.state.Peers = s.lt.Stats().ActivePeers
		if s.state.Length > 0 && s.state.BytesCompleted == s.state.Length {
			s.state.State = Complete
		}
	}
	s.state.Revision++
	s.emit(s.ctx, Event, s.state)
}

// API is what the spike's page may call. No method takes an argument, so no path or value from the
// page reaches the engine or the file system.
type API struct{ s *Service }

// Start creates the two clients and starts the transfer; while a run exists it returns that run.
func (a *API) Start() (Transfer, error) { return a.s.start() }

// Pause stops the seeder's upload, so no payload reaches the leecher.
func (a *API) Pause() (Transfer, error) {
	return a.s.command(Paused)
}

// Resume lets the seeder upload again.
func (a *API) Resume() (Transfer, error) {
	return a.s.command(Running)
}

// Snapshot returns the last published state.
func (a *API) Snapshot() Transfer {
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	return a.s.state
}
