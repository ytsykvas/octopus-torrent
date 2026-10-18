// Package swarm runs anacrolix/torrent clients as a controlled local swarm for engine tests: every
// discovery path is off, peers listen on loopback TCP only and are connected explicitly, storage
// stays in the given directory, and every wait has a deadline.
package swarm

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/iplist"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

// Options configure one peer.
type Options struct {
	// RateLimit caps payload upload and download at this many bytes per second; 0 is unlimited.
	RateLimit int
	// DefaultStorage replaces the peer's file storage for torrents added without storage of their
	// own. The caller owns it and closes it after the peer.
	DefaultStorage storage.ClientImpl
	// DataDir is the engine's own data folder; it defaults to the peer's Dir.
	DataDir string
	// KeepAliveTimeout replaces the engine's keep-alive interval, which also bounds how long a
	// connection's writer can sleep through a lost wake-up (task 010); 0 keeps the engine default.
	KeepAliveTimeout time.Duration
}

// Settings is what a peer's engine configuration allows, for reports and for the tests that prove
// the swarm is closed. Every field but RateLimit and Storage must be false.
type Settings struct {
	DHT, Trackers, PEX, PortForwarding, WebSeeds, WebTorrent, UTP, IPv6 bool
	RateLimit                                                           int
	Storage                                                             string
}

// Peer is one engine client of the swarm.
type Peer struct {
	// Dir holds this peer's payload; nothing else is written there.
	Dir string

	client  *torrent.Client
	config  *torrent.ClientConfig
	storage storage.ClientImplCloser // nil when the caller supplied the default storage
	logs    *syncBuffer
	options Options
	seen    sync.Map // remote address → true, for every peer the engine created
	tripped syncBuffer
}

// newClient starts the engine; tests replace it to reach the error branch.
var newClient = torrent.NewClient

// NewPeer starts a client that keeps its files in dir and listens on 127.0.0.1 only.
func NewPeer(dir string, opts Options) (*Peer, error) {
	p := &Peer{Dir: dir, logs: &syncBuffer{}, options: opts}
	cfg := torrent.NewDefaultClientConfig()
	cfg.SetListenAddr("127.0.0.1:0")
	cfg.DataDir = cmp.Or(opts.DataDir, dir)
	cfg.KeepAliveTimeout = cmp.Or(opts.KeepAliveTimeout, cfg.KeepAliveTimeout)
	if opts.DefaultStorage != nil {
		cfg.DefaultStorage = opts.DefaultStorage
	} else {
		p.storage = storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: storage.NewMapPieceCompletion()})
		cfg.DefaultStorage = p.storage
	}
	cfg.NoDHT, cfg.DisableTrackers, cfg.DisablePEX = true, true, true
	cfg.NoDefaultPortForwarding, cfg.DisableWebseeds, cfg.DisableWebtorrent = true, true, true
	cfg.DisableUTP, cfg.DisableIPv6 = true, true
	// Tripwires behind the switches above: whatever a configuration or a metainfo lets through, no
	// peer outside loopback is added or accepted, and every HTTP, tracker or DNS path is refused and
	// recorded in Tripped.
	cfg.IPBlocklist = loopbackOnly{}
	cfg.WebTransport = refusingTransport{p}
	cfg.HTTPDialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
		return nil, p.refuse("HTTP dial to " + addr)
	}
	cfg.TrackerDialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
		return nil, p.refuse("tracker dial to " + addr)
	}
	cfg.TrackerListenPacket = func(_, addr string) (net.PacketConn, error) { return nil, p.refuse("tracker socket on " + addr) }
	cfg.LookupTrackerIp = func(u *url.URL) ([]net.IP, error) { return nil, p.refuse("tracker lookup of " + u.Host) }
	cfg.ICEServerList = nil
	cfg.Seed = true
	cfg.Callbacks.NewPeer = append(cfg.Callbacks.NewPeer, func(peer *torrent.Peer) { p.seen.Store(peer.RemoteAddr.String(), true) })
	cfg.Slogger = slog.New(slog.NewTextHandler(p.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if opts.RateLimit > 0 {
		// Burst 0: the engine sets each limiter's burst itself (to at least one chunk) when it is 0.
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(opts.RateLimit), 0)
		cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(opts.RateLimit), 0)
	}
	client, err := newClient(cfg)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("start a swarm peer: %w", err), p.closeStorage())
	}
	p.client, p.config = client, cfg
	return p, nil
}

// Client is the peer's engine client, for tests of the engine's own API. The peer still owns it:
// close it only through Close.
func (p *Peer) Client() *torrent.Client {
	return p.client
}

// Settings reports what the peer's configuration allows.
func (p *Peer) Settings() Settings {
	c := p.config
	return Settings{DHT: !c.NoDHT, Trackers: !c.DisableTrackers, PEX: !c.DisablePEX, PortForwarding: !c.NoDefaultPortForwarding,
		WebSeeds: !c.DisableWebseeds, WebTorrent: !c.DisableWebtorrent, UTP: !c.DisableUTP, IPv6: !c.DisableIPv6,
		RateLimit: p.options.RateLimit, Storage: p.storageDescription()}
}

func (p *Peer) storageDescription() string {
	if p.options.DefaultStorage != nil {
		return "default storage given by the caller"
	}
	return "files in Dir, piece completion in memory"
}

// ListenAddrs are the addresses the peer accepts connections on.
func (p *Peer) ListenAddrs() []net.Addr {
	return p.client.ListenAddrs()
}

// SeenAddrs lists the address of every peer the engine created, incoming or outgoing, including
// connections that have closed since.
func (p *Peer) SeenAddrs() []string {
	var addrs []string
	p.seen.Range(func(addr, _ any) bool {
		addrs = append(addrs, addr.(string))
		return true
	})
	slices.Sort(addrs)
	return addrs
}

// Tripped lists every network path outside loopback the engine tried; empty in a closed swarm.
func (p *Peer) Tripped() string {
	return p.tripped.String()
}

func (p *Peer) refuse(what string) error {
	fmt.Fprintln(&p.tripped, what)
	return fmt.Errorf("swarm peer: %s is not allowed", what)
}

// refusingTransport is the engine's HTTP transport (web seeds, HTTP trackers): it refuses every request.
type refusingTransport struct{ p *Peer }

func (r refusingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, r.p.refuse("HTTP request to " + req.URL.Host)
}

// loopbackOnly is the engine's IP block list: every address but loopback is blocked.
type loopbackOnly struct{}

func (loopbackOnly) Lookup(ip net.IP) (iplist.Range, bool) {
	if ip.IsLoopback() {
		return iplist.Range{}, false
	}
	return iplist.Range{First: ip, Last: ip, Description: "outside the closed swarm"}, true
}

func (loopbackOnly) NumRanges() int {
	return 1
}

// Logs returns everything the engine logged so far.
func (p *Peer) Logs() string {
	return p.logs.String()
}

// Add adds a torrent that names no tracker, web seed or DHT node. The pinned engine panics on some metainfo (a v2-only torrent has no v1
// infohash); the panic is returned as an error.
func (p *Peer) Add(mi *metainfo.MetaInfo) (t *torrent.Torrent, err error) {
	if mi.Announce != "" || len(mi.AnnounceList) > 0 || len(mi.UrlList) > 0 || len(mi.Nodes) > 0 {
		return nil, errors.New("a swarm torrent may name no tracker, web seed or DHT node")
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the engine panicked adding the torrent: %v", r)
		}
	}()
	return p.client.AddTorrent(mi)
}

// Seed adds a torrent whose payload is already in Dir and verifies it. Missing or corrupted pieces
// are returned as an *IncompleteError together with the torrent.
func (p *Peer) Seed(ctx context.Context, mi *metainfo.MetaInfo) (*torrent.Torrent, error) {
	t, err := p.Add(mi)
	if err != nil {
		return nil, err
	}
	if err := t.VerifyDataContext(ctx); err != nil {
		return t, err
	}
	if err := Settle(ctx, t); err != nil {
		return t, err
	}
	if missing := missingPieces(t); len(missing) > 0 {
		return t, &IncompleteError{Completed: t.BytesCompleted(), Length: t.Length(), MissingPieces: missing, Err: errors.New("the seeder's data is incomplete")}
	}
	return t, nil
}

// Leech adds a torrent and asks for all of it.
func (p *Peer) Leech(mi *metainfo.MetaInfo) (*torrent.Torrent, error) {
	t, err := p.Add(mi)
	if err != nil {
		return nil, err
	}
	t.DownloadAll()
	return t, nil
}

// AddInfohash adds a torrent known only by its v1 infohash, as a magnet link does: it waits for
// the metainfo from peers.
func (p *Peer) AddInfohash(h metainfo.Hash) *torrent.Torrent {
	t, _ := p.client.AddTorrentInfoHash(h)
	return t
}

// Lie adds a torrent under infohash h with another torrent's info bytes, as a lying peer does: the
// pinned engine accepts info bytes that do not match a v1 infohash (finding 11) and serves them to a
// magnet leecher as the metadata of h. The lie wants its pieces, because a torrent that wants
// nothing accepts no connection, and Lie returns only once the engine has checked them: while a piece
// is being checked it is not wanted yet, so a connection that came then would be refused.
func (p *Peer) Lie(ctx context.Context, h metainfo.Hash, infoBytes []byte) (*torrent.Torrent, error) {
	t, isNew := p.client.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: h})
	if !isNew {
		return nil, fmt.Errorf("the peer already has %s: a lie needs a torrent of its own", h.HexString())
	}
	err := t.SetInfoBytes(infoBytes)
	if err == nil {
		t.DownloadAll()
		err = Settle(ctx, t)
	}
	if err != nil {
		t.Drop()
		return nil, err
	}
	return t, nil
}

// Connect gives the torrent the listen addresses of the peers; no other peer source exists. The peers
// are trusted: they all share 127.0.0.1 and the engine bans a peer that sent bad data by its IP, so
// one ban would cut off every local peer. A trusted peer is never banned; its bad piece is requested
// again (and fails again) until the wait ends.
func Connect(t *torrent.Torrent, peers ...*Peer) {
	t.AddPeers(PeerInfos(peers...))
}

// PeerInfos returns the listen addresses of the peers as trusted peer entries, as Connect gives
// them; for code that takes its peers as a list.
func PeerInfos(peers ...*Peer) []torrent.PeerInfo {
	var infos []torrent.PeerInfo
	for _, p := range peers {
		for _, a := range p.ListenAddrs() {
			infos = append(infos, torrent.PeerInfo{Addr: a, Trusted: true})
		}
	}
	return infos
}

// Close stops the client and its storage, then checks that its listen address refuses connections.
func (p *Peer) Close(ctx context.Context) error {
	addrs := p.ListenAddrs()
	errs := append(p.client.Close(), p.closeStorage())
	for _, a := range addrs {
		errs = append(errs, Refuses(ctx, a))
	}
	return errors.Join(errs...)
}

// closeStorage closes the storage the peer created; one the caller supplied stays the caller's.
func (p *Peer) closeStorage() error {
	if p.storage == nil {
		return nil
	}
	return p.storage.Close()
}

// Refuses waits until addr refuses TCP connections, and returns an error when it still accepts
// them as ctx ends. The engine closes its listeners in a goroutine, after Client.Close returns.
func Refuses(ctx context.Context, addr net.Addr) error {
	for {
		conn, err := net.DialTimeout("tcp", addr.String(), time.Second)
		if err != nil {
			return nil
		}
		conn.Close()
		if err := pause(ctx, 10*time.Millisecond); err != nil {
			return fmt.Errorf("%s still accepts connections: %w", addr, err)
		}
	}
}

// syncBuffer is a bytes.Buffer safe for the engine's concurrent log writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
