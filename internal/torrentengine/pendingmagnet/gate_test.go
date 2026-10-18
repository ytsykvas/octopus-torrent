package pendingmagnet

import (
	"bytes"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	pp "github.com/anacrolix/torrent/peer_protocol"

	"octopustorrent/internal/torrentengine/infocheck"
)

// metadataMsg is a BEP 9 message as a peer sends it: the dictionary, then a data piece's bytes.
func metadataMsg(kind pp.ExtendedMetadataRequestMsgType, piece, total int, data []byte) []byte {
	return append(bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Type: kind, Piece: piece, TotalSize: total}), data...)
}

// rejected reports whether the engine will read payload as a reject, which it ignores.
func rejected(t *testing.T, payload []byte) bool {
	t.Helper()
	var msg pp.ExtendedMetadataRequestMsg
	err := bencode.Unmarshal(payload, &msg)
	var trailing bencode.ErrUnusedTrailingBytes
	return (err == nil || errors.As(err, &trailing)) && msg.Type == pp.RejectMetadataExtensionMsgType
}

func newTestGate(t *testing.T) *Gate {
	t.Helper()
	cfg := torrent.NewDefaultClientConfig()
	before := len(cfg.Callbacks.PeerConnReadExtensionMessage)
	g := NewGate(cfg)
	if len(cfg.Callbacks.PeerConnReadExtensionMessage) != before+1 {
		t.Fatal("NewGate did not install its callback")
	}
	return g
}

// The gate assembles a torrent's metadata from data pieces in any order, keeps the first copy of a
// piece, and turns every data message into a reject before the engine reads it.
func TestTheGateAssemblesMetadataAndTheEngineSeesOnlyRejects(t *testing.T) {
	g := newTestGate(t)
	key := new(torrent.Torrent)
	info := bytes.Repeat([]byte("0123456789"), 4000) // 3 pieces: 16 KiB, 16 KiB, 7232 bytes
	a := g.register(key, len(info))
	piece := func(i int) []byte { return info[i<<14 : min((i+1)<<14, len(info))] }
	for _, p := range [][]byte{
		metadataMsg(pp.DataMetadataExtensionMsgType, 2, len(info), piece(2)),
		metadataMsg(pp.DataMetadataExtensionMsgType, 0, len(info), piece(0)),
		metadataMsg(pp.DataMetadataExtensionMsgType, 0, len(info), bytes.Repeat([]byte("x"), 1<<14)), // a later copy
	} {
		g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, p)
		if !rejected(t, p) {
			t.Fatalf("a data message reached the engine as %q", p[:20])
		}
	}
	select {
	case <-a.done:
		t.Fatal("done before every piece arrived")
	default:
	}
	g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, metadataMsg(pp.DataMetadataExtensionMsgType, 1, len(info), piece(1)))
	<-a.done
	if got, err := a.result(); err != nil || !bytes.Equal(got, info) {
		t.Errorf("result = %d bytes, %v; want the info", len(got), err)
	}
	g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, metadataMsg(pp.DataMetadataExtensionMsgType, 0, 7, []byte("ignored"))) // after the end
	if got, err := a.result(); err != nil || !bytes.Equal(got, info) {
		t.Errorf("a piece after the end changed the result: %v", err)
	}
	g.unregister(key)
	if len(g.assemblies) != 0 {
		t.Error("unregister left the assembly")
	}
}

// Data for a torrent nobody fetches is rejected too: the engine never builds an info from peers.
// Valid requests, rejects, handshakes, other extensions and a dictionary the engine fails on the
// same way are left to the engine.
func TestTheGateRejectsEveryDataMessageAndLeavesTheRest(t *testing.T) {
	g := newTestGate(t)
	data := metadataMsg(pp.DataMetadataExtensionMsgType, 0, 3, []byte("abc"))
	g.onMessage(new(torrent.Torrent), "10.0.0.1", pp.ExtensionNameMetadata, data)
	if !rejected(t, data) {
		t.Error("data for an unregistered torrent reached the engine")
	}
	// A request for a piece no metadata can have would crash the engine once it has the info
	// (finding 41): it becomes a reject, or nothing the engine can decode if too short for one.
	for _, piece := range []int{-1, maxMetadataPieces, 1<<63 - 1} {
		req := metadataMsg(pp.RequestMetadataExtensionMsgType, piece, 0, nil)
		g.onMessage(new(torrent.Torrent), "10.0.0.1", pp.ExtensionNameMetadata, req)
		if !rejected(t, req) {
			t.Errorf("a request for piece %d reached the engine", piece)
		}
	}
	short := []byte("d5:piecei-1ee")
	g.onMessage(new(torrent.Torrent), "10.0.0.1", pp.ExtensionNameMetadata, short)
	if !bytes.Equal(short, make([]byte, len(short))) {
		t.Errorf("a short request for piece -1 became %q, want zeros", short)
	}
	// Every extended message, the handshake included, must be one the engine can decode within
	// infocheck's bounds; a deep one becomes bytes the engine refuses, so it drops the peer.
	deep := []byte(strings.Repeat("l", 100) + strings.Repeat("e", 100))
	g.onMessage(new(torrent.Torrent), "10.0.0.1", "", deep)
	if !bytes.Equal(deep, make([]byte, len(deep))) {
		t.Error("a message nested 100 deep reached the engine")
	}
	for _, c := range []struct {
		name string
		ext  pp.ExtensionName
		msg  []byte
	}{
		{"a request", pp.ExtensionNameMetadata, metadataMsg(pp.RequestMetadataExtensionMsgType, 0, 0, nil)},
		{"a handshake", "", []byte("d1:md11:ut_metadatai1eee")},
		{"a dictionary the engine cannot read as a metadata message", pp.ExtensionNameMetadata, []byte("d8:msg_type1:xe")},
		{"a reject", pp.ExtensionNameMetadata, metadataMsg(pp.RejectMetadataExtensionMsgType, 0, 0, nil)},
		{"another extension", pp.ExtensionNamePex, metadataMsg(pp.DataMetadataExtensionMsgType, 0, 3, []byte("abc"))},
	} {
		before := bytes.Clone(c.msg)
		g.onMessage(new(torrent.Torrent), "10.0.0.1", c.ext, c.msg)
		if !bytes.Equal(c.msg, before) {
			t.Errorf("%s was changed", c.name)
		}
	}
}

// The extended handshake's metadata_size reaches the engine's allocation through a 32-bit check
// (finding 42): a size the engine cannot hold becomes "unknown", so the engine allocates nothing.
func TestTheGateClampsTheHandshakesMetadataSize(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	var before int
	cfg.Callbacks.ReadExtendedHandshake = func(_ *torrent.PeerConn, hs *pp.ExtendedHandshakeMessage) { before = hs.MetadataSize }
	g := NewGate(cfg)
	for size, want := range map[int]int{1 << 20: 1 << 20, 16 << 20: 16 << 20, 16<<20 + 1: 0, 1<<32 + 5: 0, -1: 0} {
		hs := pp.ExtendedHandshakeMessage{MetadataSize: size}
		cfg.Callbacks.ReadExtendedHandshake(nil, &hs)
		if hs.MetadataSize != want || before != size {
			t.Errorf("metadata_size %d became %d (the earlier callback saw %d), want %d", size, hs.MetadataSize, before, want)
		}
	}
	_ = g
}

// A ut_holepunch message is binary, not bencode; the gate turns it into bytes the engine refuses,
// so the engine drops the peer instead of panicking when it answers (finding 43). Scanning it as
// bencode would wrongly clear every legitimate one.
// peerIP gives a dialed peer's stable IP, the only stable part of an incoming one, or the address
// itself when it has no port.
func TestPeerIPIsTheAddressWithoutThePort(t *testing.T) {
	for _, c := range []struct {
		addr interface{ String() string }
		want string
	}{
		{&net.TCPAddr{IP: net.IPv4(10, 0, 0, 3), Port: 51413}, "10.0.0.3"},
		{fakeAddr("host.example:6881"), "host.example"},
		{fakeAddr("no-port"), "no-port"},
	} {
		if got := peerIP(c.addr); got != c.want {
			t.Errorf("peerIP(%v) = %q, want %q", c.addr, got, c.want)
		}
	}
}

// fakeAddr is a net.Addr whose String has no AddrPort method, to reach peerIP's text paths.
type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestTheGateNeutralizesHolepunch(t *testing.T) {
	g := newTestGate(t)
	hp := []byte{0, 0, 1, 1, 1, 1, 0, 80, 0, 0, 0, 0} // a rendezvous, valid binary, not bencode
	g.onMessage(new(torrent.Torrent), "10.0.0.1", extHolepunch, hp)
	if !bytes.Equal(hp, make([]byte, len(hp))) {
		t.Errorf("a holepunch message became %v, want zeros", hp)
	}
}

// The extended handshake grows the engine's per-name counter for every distinct extension; the gate
// keeps only ut_metadata and ut_pex, so a handshake of many names cannot exhaust memory.
func TestTheGateKeepsOnlyTheExtensionsItNeeds(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	NewGate(cfg)
	hs := pp.ExtendedHandshakeMessage{M: map[pp.ExtensionName]pp.ExtensionNumber{
		pp.ExtensionNameMetadata: 1, pp.ExtensionNamePex: 2, extHolepunch: 3, "ut_junk_0": 4, "ut_junk_1": 5,
	}}
	cfg.Callbacks.ReadExtendedHandshake(nil, &hs)
	if len(hs.M) != 2 || hs.M[pp.ExtensionNameMetadata] != 1 || hs.M[pp.ExtensionNamePex] != 2 {
		t.Errorf("handshake kept %v, want only ut_metadata and ut_pex", hs.M)
	}
}

// The engine records every request length it is sent, so the gate turns an outsized one into a
// keep-alive; a normal request is left for the engine to serve.
func TestTheGateDropsOutsizedRequests(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	NewGate(cfg)
	big := pp.Message{Type: pp.Request, Index: 1, Length: maxRequestLength + 1}
	cfg.Callbacks.ReadMessage(nil, &big)
	if !big.Keepalive {
		t.Errorf("an outsized request became %+v, want a keep-alive", big)
	}
	normal := pp.Message{Type: pp.Request, Index: 1, Length: 1 << 14}
	cfg.Callbacks.ReadMessage(nil, &normal)
	if normal.Keepalive || normal.Length != 1<<14 {
		t.Errorf("a normal request became %+v, want it untouched", normal)
	}
}

// v2 hash messages crash the pinned engine from any peer (finding 43); the product verifies pieces
// with v1 hashes, so the gate turns them into keep-alives before the engine reads them.
func TestTheGateDropsV2HashMessages(t *testing.T) {
	cfg := torrent.NewDefaultClientConfig()
	var seen []pp.MessageType
	cfg.Callbacks.ReadMessage = func(_ *torrent.PeerConn, m *pp.Message) { seen = append(seen, m.Type) }
	NewGate(cfg)
	for _, typ := range []pp.MessageType{pp.Hashes, pp.HashRequest, pp.HashReject} {
		m := pp.Message{Type: typ, Index: 7}
		cfg.Callbacks.ReadMessage(nil, &m)
		if !m.Keepalive || m.Index != 0 {
			t.Errorf("%v reached the engine as %+v", typ, m)
		}
	}
	piece := pp.Message{Type: pp.Piece, Index: 7}
	cfg.Callbacks.ReadMessage(nil, &piece)
	if piece.Keepalive || piece.Index != 7 || len(seen) != 4 {
		t.Errorf("a piece message became %+v; the earlier callback saw %v", piece, seen)
	}
}

// A refusal names the peers that sent the metadata, so the caller can retry without them.
func TestARefusedAssemblyNamesItsPeers(t *testing.T) {
	g := newTestGate(t)
	key := new(torrent.Torrent)
	a := g.register(key, 1<<20)
	g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, metadataMsg(pp.DataMetadataExtensionMsgType, 0, 20000, make([]byte, 1<<14)))
	g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, metadataMsg(pp.DataMetadataExtensionMsgType, 0, 20000, make([]byte, 1<<14)))
	g.onMessage(key, "10.0.0.2", pp.ExtensionNameMetadata, metadataMsg(pp.DataMetadataExtensionMsgType, 1, 99, []byte("x")))
	<-a.done
	_, err := a.result()
	var pe *PeerError
	if !errors.As(err, &pe) || !errors.Is(err, ErrInfoMismatch) || !slices.Equal(pe.Peers, []string{"10.0.0.1", "10.0.0.2"}) || !strings.Contains(err.Error(), "10.0.0.1, 10.0.0.2") {
		t.Errorf("result error %v, want ErrInfoMismatch naming 10.0.0.1 and 10.0.0.2", err)
	}
}

// A piece that cannot belong to the metadata ends the assembly, as a liar's info ends a fetch; a
// size over the limit ends it before any buffer exists.
func TestAPieceThatDoesNotFitEndsTheAssembly(t *testing.T) {
	const total = 20000 // 2 pieces
	for _, c := range []struct {
		name string
		msgs [][]byte
		want error
	}{
		{"a size over the limit", [][]byte{metadataMsg(pp.DataMetadataExtensionMsgType, 0, 1<<20+1, []byte("x"))}, infocheck.ErrInfoTooLarge},
		{"no size", [][]byte{metadataMsg(pp.DataMetadataExtensionMsgType, 0, 0, []byte("x"))}, ErrInfoMismatch},
		{"a piece out of range", [][]byte{metadataMsg(pp.DataMetadataExtensionMsgType, 2, total, []byte("x"))}, ErrInfoMismatch},
		{"a negative piece", [][]byte{metadataMsg(pp.DataMetadataExtensionMsgType, -1, total, []byte("x"))}, ErrInfoMismatch},
		{"a piece without its bytes", [][]byte{metadataMsg(pp.DataMetadataExtensionMsgType, 1, total, nil)}, ErrInfoMismatch},
		{"a changed size", [][]byte{
			metadataMsg(pp.DataMetadataExtensionMsgType, 0, total, make([]byte, 1<<14)),
			metadataMsg(pp.DataMetadataExtensionMsgType, 1, total+1, make([]byte, total+1-1<<14)),
		}, ErrInfoMismatch},
	} {
		g := newTestGate(t)
		key := new(torrent.Torrent)
		a := g.register(key, 1<<20)
		for _, m := range c.msgs {
			g.onMessage(key, "10.0.0.1", pp.ExtensionNameMetadata, m)
		}
		<-a.done
		if got, err := a.result(); got != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: %d bytes, %v; want %v", c.name, len(got), err, c.want)
		}
	}
}
