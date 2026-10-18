package swarm

import (
	"bufio"
	"context"
	"net"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

// listen opens the MetadataPeer's socket; tests replace it to reach the error branch.
var listen = net.Listen

// metadataID is the number this peer gives ut_metadata in its extended handshake.
const metadataID pp.ExtensionNumber = 1

// metadataPiece is the BEP 9 piece size.
const metadataPiece = 16 << 10

// MetadataOptions change what a MetadataPeer says after the handshake.
type MetadataOptions struct {
	// HaveAll sends a bitfield with every piece of the info and an unchoke, so the engine plans
	// requests to this peer (finding 39). The info must be one the engine can parse.
	HaveAll bool
	// MetadataSize overrides the metadata_size the extended handshake advertises; 0 keeps len(info).
	// A peer lies here to make the engine allocate (findings 42).
	MetadataSize int
	// Attack, when set, runs once the engine's extended handshake has arrived, with a function that
	// sends a message to the engine and the engine's own ut_metadata number. A hostile peer sends
	// messages the engine mishandles (findings 41, 43); the peer serves no metadata itself then.
	Attack func(send func(pp.Message), utMetadata pp.ExtensionNumber)
}

// MetadataPeer is a loopback peer that speaks only the BitTorrent handshake, BEP 10 and BEP 9, and
// serves fixed info bytes for one infohash: what a hostile peer does when it serves an info that the
// engine must never apply. An engine peer cannot serve such info, because applying it panics its own
// client (findings 33–35). It speaks plain text only; the engine's dial falls back to it.
type MetadataPeer struct {
	ln       net.Listener
	infoHash metainfo.Hash
	info     []byte
	have     []bool // the bitfield to send; nil sends none
	opts     MetadataOptions
	ctx      context.Context
	stop     context.CancelFunc
	conns    sync.WaitGroup
	served   atomic.Int64
}

// ServeMetadata starts a MetadataPeer on 127.0.0.1 that serves info as the metadata of h.
func ServeMetadata(h metainfo.Hash, info []byte, opts MetadataOptions) (*MetadataPeer, error) {
	var have []bool
	if opts.HaveAll {
		var parsed metainfo.Info
		if err := bencode.Unmarshal(info, &parsed); err != nil {
			return nil, err
		}
		have = make([]bool, parsed.NumPieces())
		for i := range have {
			have[i] = true
		}
	}
	ln, err := listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	m := &MetadataPeer{ln: ln, infoHash: h, info: info, have: have, opts: opts, ctx: ctx, stop: stop}
	m.conns.Add(1)
	go m.accept()
	return m, nil
}

// Addr is the peer's listen address.
func (m *MetadataPeer) Addr() net.Addr {
	return m.ln.Addr()
}

// PeerInfo is the trusted peer entry for the listen address, as PeerInfos gives for engine peers.
func (m *MetadataPeer) PeerInfo() torrent.PeerInfo {
	return torrent.PeerInfo{Addr: m.Addr(), Trusted: true}
}

// Served is the number of metadata pieces sent.
func (m *MetadataPeer) Served() int64 {
	return m.served.Load()
}

// Close stops listening, closes every connection and waits for them to end. It may be called again.
func (m *MetadataPeer) Close() {
	m.stop()
	m.ln.Close()
	m.conns.Wait()
}

func (m *MetadataPeer) accept() {
	defer m.conns.Done()
	for {
		conn, err := m.ln.Accept()
		if err != nil {
			return // closed
		}
		m.conns.Add(1)
		go m.serve(conn)
	}
}

// serve answers one connection: the handshake for this peer's infohash, then the extended handshake,
// then metadata requests. Every other message is ignored. A connection ends when the other side
// closes it, when it fails to read, or when the peer is closed.
func (m *MetadataPeer) serve(conn net.Conn) {
	defer m.conns.Done()
	defer conn.Close()
	defer context.AfterFunc(m.ctx, func() { conn.Close() })()
	res, err := pp.Handshake(m.ctx, conn, &m.infoHash, [20]byte{'-', 'O', 'T', 'm', 'e', 't', 'a'}, pp.NewPeerExtensionBytes(pp.ExtensionBitLtep))
	if err != nil || res.Hash != m.infoHash {
		return
	}
	if m.have != nil {
		m.send(conn, pp.Message{Type: pp.Bitfield, Bitfield: m.have})
		m.send(conn, pp.Message{Type: pp.Unchoke})
	}
	size := len(m.info)
	if m.opts.MetadataSize != 0 {
		size = m.opts.MetadataSize
	}
	m.send(conn, pp.Message{Type: pp.Extended, ExtendedID: pp.HandshakeExtendedID, ExtendedPayload: bencode.MustMarshal(pp.ExtendedHandshakeMessage{
		M:            map[pp.ExtensionName]pp.ExtensionNumber{pp.ExtensionNameMetadata: metadataID},
		MetadataSize: size,
	})})
	dec := pp.Decoder{R: bufio.NewReader(conn), MaxLength: 1 << 20, Pool: &sync.Pool{New: func() any { b := make([]byte, metadataPiece); return &b }}}
	var theirs pp.ExtensionNumber // the other side's number for ut_metadata
	for {
		var msg pp.Message
		if dec.Decode(&msg) != nil {
			return
		}
		switch {
		case msg.Keepalive || msg.Type != pp.Extended:
		case msg.ExtendedID == pp.HandshakeExtendedID:
			var hs pp.ExtendedHandshakeMessage
			if bencode.Unmarshal(msg.ExtendedPayload, &hs) == nil {
				theirs = hs.M[pp.ExtensionNameMetadata]
				if m.opts.Attack != nil {
					m.opts.Attack(func(out pp.Message) { m.send(conn, out) }, theirs)
				}
			}
		case msg.ExtendedID == metadataID && m.opts.Attack == nil:
			var req pp.ExtendedMetadataRequestMsg
			if bencode.Unmarshal(msg.ExtendedPayload, &req) == nil && req.Type == pp.RequestMetadataExtensionMsgType {
				m.send(conn, m.reply(theirs, req.Piece))
			}
		}
	}
}

// reply is the data of a metadata piece, or a reject for a piece the info does not have.
func (m *MetadataPeer) reply(theirs pp.ExtensionNumber, piece int) pp.Message {
	msg := pp.Message{Type: pp.Extended, ExtendedID: theirs}
	begin := piece * metadataPiece
	if piece < 0 || begin >= len(m.info) {
		msg.ExtendedPayload = bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Piece: piece, Type: pp.RejectMetadataExtensionMsgType})
		return msg
	}
	m.served.Add(1)
	header := bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Piece: piece, TotalSize: len(m.info), Type: pp.DataMetadataExtensionMsgType})
	msg.ExtendedPayload = append(header, m.info[begin:min(begin+metadataPiece, len(m.info))]...)
	return msg
}

// send writes one message. A failed write is not handled here: the connection's next read fails
// and ends it. MarshalBinary cannot fail for the extended messages this peer builds.
func (m *MetadataPeer) send(conn net.Conn, msg pp.Message) {
	b, _ := msg.MarshalBinary()
	_, _ = conn.Write(b)
}
