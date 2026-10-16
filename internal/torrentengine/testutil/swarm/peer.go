// Package swarm runs anacrolix/torrent clients as a controlled local swarm for engine tests: every
// discovery path is off, peers listen on loopback TCP only and are connected explicitly, storage
// stays in the given directory, and every wait has a deadline.
package swarm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"
)

// Options configure one peer.
type Options struct {
	// RateLimit caps payload upload and download at this many bytes per second; 0 is unlimited.
	RateLimit int
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
	storage storage.ClientImplCloser
	logs    *syncBuffer
	options Options
	seen    sync.Map // remote address → true, for every peer the engine created
}

// newClient starts the engine; tests replace it to reach the error branch.
var newClient = torrent.NewClient

// NewPeer starts a client that keeps its files in dir and listens on 127.0.0.1 only.
func NewPeer(dir string, opts Options) (*Peer, error) {
	p := &Peer{Dir: dir, logs: &syncBuffer{}, options: opts}
	p.storage = storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: storage.NewMapPieceCompletion()})
	cfg := torrent.NewDefaultClientConfig()
	cfg.SetListenAddr("127.0.0.1:0")
	cfg.DataDir = dir
	cfg.DefaultStorage = p.storage
	cfg.NoDHT, cfg.DisableTrackers, cfg.DisablePEX = true, true, true
	cfg.NoDefaultPortForwarding, cfg.DisableWebseeds, cfg.DisableWebtorrent = true, true, true
	cfg.DisableUTP, cfg.DisableIPv6 = true, true
	cfg.Seed = true
	cfg.Callbacks.NewPeer = append(cfg.Callbacks.NewPeer, func(peer *torrent.Peer) { p.seen.Store(peer.RemoteAddr.String(), true) })
	cfg.Slogger = slog.New(slog.NewTextHandler(p.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if opts.RateLimit > 0 {
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(opts.RateLimit), 0)
		cfg.DownloadRateLimiter = rate.NewLimiter(rate.Limit(opts.RateLimit), 0)
	}
	client, err := newClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("start a swarm peer: %w", err)
	}
	p.client, p.config = client, cfg
	return p, nil
}

// Settings reports what the peer's configuration allows.
func (p *Peer) Settings() Settings {
	c := p.config
	return Settings{DHT: !c.NoDHT, Trackers: !c.DisableTrackers, PEX: !c.DisablePEX, PortForwarding: !c.NoDefaultPortForwarding,
		WebSeeds: !c.DisableWebseeds, WebTorrent: !c.DisableWebtorrent, UTP: !c.DisableUTP, IPv6: !c.DisableIPv6,
		RateLimit: p.options.RateLimit, Storage: "files in Dir, piece completion in memory"}
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

// Logs returns everything the engine logged so far.
func (p *Peer) Logs() string {
	return p.logs.String()
}

// Add adds a torrent. The pinned engine panics on some metainfo (a v2-only torrent has no v1
// infohash); the panic is returned as an error.
func (p *Peer) Add(mi *metainfo.MetaInfo) (t *torrent.Torrent, err error) {
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
	if err := settle(ctx, t); err != nil {
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

// Connect gives the torrent the listen addresses of the peers; no other peer source exists.
func Connect(t *torrent.Torrent, peers ...*Peer) {
	var infos []torrent.PeerInfo
	for _, p := range peers {
		for _, a := range p.ListenAddrs() {
			infos = append(infos, torrent.PeerInfo{Addr: a, Trusted: true})
		}
	}
	t.AddPeers(infos)
}

// Close stops the client and its storage, then checks that its listen address refuses connections.
func (p *Peer) Close(ctx context.Context) error {
	addrs := p.ListenAddrs()
	errs := append(p.client.Close(), p.storage.Close())
	for _, a := range addrs {
		errs = append(errs, Refuses(ctx, a))
	}
	return errors.Join(errs...)
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
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s still accepts connections: %w", addr, ctx.Err())
		case <-time.After(10 * time.Millisecond):
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
