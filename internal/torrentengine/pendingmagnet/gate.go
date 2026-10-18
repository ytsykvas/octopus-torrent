package pendingmagnet

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	pp "github.com/anacrolix/torrent/peer_protocol"

	"octopustorrent/internal/torrentengine/infocheck"
)

// rejectMsg is what every metadata data message, and every request for a piece no metadata can
// have, becomes before the engine reads it: the engine reads the dictionary, ignores the bytes
// after it and ignores a reject.
const rejectMsg = "d8:msg_typei2ee"

const (
	// metadataPiece is the BEP 9 piece size.
	metadataPiece = 16 << 10
	// maxMetadataBytes is the engine's own metadata cap; maxMetadataPieces is its piece count.
	maxMetadataBytes  = 16 << 20
	maxMetadataPieces = maxMetadataBytes / metadataPiece
)

// PeerError is metadata refused after it was assembled, with the peers that sent its pieces, so a
// caller can retry without them. It wraps the refusal.
type PeerError struct {
	Peers []string
	Err   error
}

func (e *PeerError) Error() string {
	return fmt.Sprintf("%v (metadata from %s)", e.Err, strings.Join(e.Peers, ", "))
}

func (e *PeerError) Unwrap() error { return e.Err }

// extHolepunch is the BEP 55 extension name; its payload is binary, not bencode.
const extHolepunch pp.ExtensionName = "ut_holepunch"

// keptExtensions are the peer extensions the gate lets the engine learn. ut_metadata carries the
// info; ut_pex finds peers. The rest are dropped, which both keeps the engine's per-name counter
// from growing on hostile handshakes and stops it using an extension the gate neutralizes.
var keptExtensions = []pp.ExtensionName{pp.ExtensionNameMetadata, pp.ExtensionNamePex}

// maxRequestLength caps a peer's request length before the engine records it; a real request asks
// for one 16 KiB chunk.
const maxRequestLength = 1 << 17

// Gate keeps peers' hostile input away from the engine. The engine parses and applies a magnet's
// info as soon as its pieces arrive, before anything can check it, and several shapes crash or stall
// it, some before any storage hook (findings 33–40); a few other peer messages crash it outright
// (findings 41–43). The gate sees each message first, through the engine's own callbacks, which run
// under the client lock:
//   - a ut_holepunch message (binary, not bencode) becomes bytes the engine refuses: the engine
//     panics answering one from a peer that did not advertise the extension (finding 43). The
//     product moves no payload while a magnet is pending, so it needs no hole punching;
//   - every other extended message must pass infocheck.CheckMessage, or it becomes bytes the engine
//     cannot decode, and the engine drops the peer;
//   - each metadata data piece is copied into the assembly of the torrent Fetch waits for, and the
//     message becomes a reject, so the engine never assembles an info itself; Fetch vets the bytes
//     and hands them to the engine;
//   - a metadata request for a piece no metadata can have becomes a reject;
//   - the extended handshake's metadata size is cleared when the engine cannot hold it, and every
//     extension but ut_metadata and ut_pex is dropped, so a handshake of many names cannot grow the
//     engine's per-name counter without bound;
//   - v2 hash messages become keep-alives (the product verifies pieces with v1 hashes), and a
//     request for an outsized chunk length too (the engine records every length it is sent).
//
// One gate serves one client, which must be started with the configuration NewGate changed. NewGate
// must be the last thing to set these callbacks: it wraps the ones already there, so a later
// assignment would drop the gate's screen.
type Gate struct {
	mu         sync.Mutex
	assemblies map[*torrent.Torrent]*assembly
}

// NewGate returns a gate installed in cfg, wrapping any callbacks cfg already has.
func NewGate(cfg *torrent.ClientConfig) *Gate {
	g := &Gate{assemblies: map[*torrent.Torrent]*assembly{}}
	cb := &cfg.Callbacks
	cb.PeerConnReadExtensionMessage = append(cb.PeerConnReadExtensionMessage, func(e torrent.PeerConnReadExtensionMessageEvent) {
		name, _, _ := e.PeerConn.LocalLtepProtocolMap.LookupId(e.ExtensionNumber)
		g.onMessage(e.PeerConn.Torrent(), peerIP(e.PeerConn.RemoteAddr), name, e.Payload)
	})
	readHandshake := cb.ReadExtendedHandshake
	cb.ReadExtendedHandshake = func(pc *torrent.PeerConn, hs *pp.ExtendedHandshakeMessage) {
		if readHandshake != nil {
			readHandshake(pc, hs)
		}
		if hs.MetadataSize < 0 || hs.MetadataSize > maxMetadataBytes {
			hs.MetadataSize = 0
		}
		for name := range hs.M {
			if !slices.Contains(keptExtensions, name) {
				delete(hs.M, name)
			}
		}
	}
	readMessage := cb.ReadMessage
	cb.ReadMessage = func(pc *torrent.PeerConn, m *pp.Message) {
		if readMessage != nil {
			readMessage(pc, m)
		}
		switch {
		case m.Type == pp.Hashes || m.Type == pp.HashRequest || m.Type == pp.HashReject:
			*m = pp.Message{Keepalive: true}
		case m.Type == pp.Request && m.Length > maxRequestLength:
			*m = pp.Message{Keepalive: true}
		}
	}
	return g
}

// peerIP is a peer's IP without the port, which is stable for a dialed peer and the only stable part
// of an incoming one. It is how a refusal names who to retry without. remote is the engine's
// torrent.PeerRemoteAddr (a Stringer); a *net.TCPAddr also offers AddrPort.
func peerIP(remote interface{ String() string }) string {
	if ap, ok := remote.(interface{ AddrPort() netip.AddrPort }); ok {
		return ap.AddrPort().Addr().Unmap().String()
	}
	if host, _, err := net.SplitHostPort(remote.String()); err == nil {
		return host
	}
	return remote.String()
}

// onMessage handles one extended message from peer before the engine does; name is the extension's,
// or empty for the handshake and unknown extensions.
func (g *Gate) onMessage(t *torrent.Torrent, peer string, name pp.ExtensionName, payload []byte) {
	if name == extHolepunch {
		clear(payload)
		return
	}
	if infocheck.CheckMessage(payload) != nil {
		clear(payload)
		return
	}
	if name != pp.ExtensionNameMetadata {
		return
	}
	var msg pp.ExtendedMetadataRequestMsg
	if err := bencode.Unmarshal(payload, &msg); err != nil {
		if _, trailing := err.(bencode.ErrUnusedTrailingBytes); !trailing {
			return // the engine fails the same way and drops the peer
		}
	}
	switch {
	case msg.Type == pp.RequestMetadataExtensionMsgType && (msg.Piece < 0 || msg.Piece >= maxMetadataPieces):
		reject(payload)
	case msg.Type == pp.DataMetadataExtensionMsgType:
		g.mu.Lock()
		if a := g.assemblies[t]; a != nil {
			a.add(peer, msg, payload)
		}
		g.mu.Unlock()
		reject(payload)
	}
}

// reject turns a metadata message into a reject in place, or into bytes the engine cannot decode
// when it is too short for one.
func reject(payload []byte) {
	if len(payload) < len(rejectMsg) {
		clear(payload)
		return
	}
	copy(payload, rejectMsg)
}

// register starts collecting t's metadata, at most limit bytes of it.
func (g *Gate) register(t *torrent.Torrent, limit int) *assembly {
	a := &assembly{limit: limit, done: make(chan struct{})}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.assemblies[t] = a
	return a
}

func (g *Gate) unregister(t *torrent.Torrent) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.assemblies, t)
}

// assembly collects one torrent's metadata pieces. It is used under the gate's lock until done is
// closed; result is read after that.
type assembly struct {
	limit   int
	buf     []byte
	have    []bool
	missing int
	peers   []string // who sent pieces, in order
	err     error
	done    chan struct{}
}

// add takes the piece of a data message the way the engine would (the last PieceSize bytes of the
// payload). The first copy of a piece is kept; the info's SHA-1 decides whether it was right. A
// piece that cannot belong to the metadata ends the assembly, as a liar's info ends a fetch.
func (a *assembly) add(peer string, msg pp.ExtendedMetadataRequestMsg, payload []byte) {
	if isClosed(a.done) {
		return
	}
	if !slices.Contains(a.peers, peer) {
		a.peers = append(a.peers, peer)
	}
	if a.buf == nil {
		if msg.TotalSize > a.limit {
			a.finish(fmt.Errorf("%w: metadata of %d bytes, the limit is %d", infocheck.ErrInfoTooLarge, msg.TotalSize, a.limit))
			return
		}
		if msg.TotalSize > 0 {
			a.buf, a.have = make([]byte, msg.TotalSize), make([]bool, (msg.TotalSize+metadataPiece-1)/metadataPiece)
			a.missing = len(a.have)
		}
	}
	size := msg.PieceSize()
	if msg.TotalSize != len(a.buf) || msg.Piece < 0 || msg.Piece >= len(a.have) || size >= len(payload) {
		a.finish(fmt.Errorf("%w: metadata piece %d of %d bytes does not fit", ErrInfoMismatch, msg.Piece, msg.TotalSize))
		return
	}
	if a.have[msg.Piece] {
		return
	}
	copy(a.buf[msg.Piece*metadataPiece:], payload[len(payload)-size:])
	a.have[msg.Piece] = true
	if a.missing--; a.missing == 0 {
		a.finish(nil)
	}
}

func (a *assembly) finish(err error) {
	a.err = err
	close(a.done)
}

// result is the assembled metadata or why there is none, naming the peers that sent it; it is read
// after done is closed.
func (a *assembly) result() ([]byte, error) {
	if a.err != nil {
		return nil, a.refused(a.err)
	}
	return a.buf, nil
}

// refused names the assembly's peers on a refusal of its metadata.
func (a *assembly) refused(err error) error {
	return &PeerError{Peers: a.peers, Err: err}
}
