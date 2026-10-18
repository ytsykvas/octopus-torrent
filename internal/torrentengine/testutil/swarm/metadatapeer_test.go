package swarm

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	pp "github.com/anacrolix/torrent/peer_protocol"
)

func serveMetadata(t *testing.T, name string, opts MetadataOptions) (*MetadataPeer, metainfo.Hash, []byte) {
	t.Helper()
	mi := metaInfo(t, named(t, name))
	m, err := ServeMetadata(mi.HashInfoBytes(), mi.InfoBytes, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, mi.HashInfoBytes(), mi.InfoBytes
}

func TestAMetadataPeerServesItsInfoToAMagnetLeecher(t *testing.T) {
	m, h, _ := serveMetadata(t, "v1-tree", MetadataOptions{})
	leecher := peer(t, Options{})
	lt := leecher.AddInfohash(h)
	lt.AddPeers([]torrent.PeerInfo{m.PeerInfo()})
	if err := WaitInfo(deadline(t, transferLimit), lt); err != nil || lt.Info().Name != "tree" {
		t.Fatalf("the leecher got %v, %v", lt.Info(), err)
	}
	if m.Served() == 0 {
		t.Error("no metadata piece was counted")
	}
	addr := m.Addr()
	if got := m.PeerInfo(); got.Addr != addr || !got.Trusted {
		t.Errorf("PeerInfo = %+v, want the trusted listen address %v", got, addr)
	}
	m.Close()
	m.Close()
	if err := Refuses(deadline(t, closeLimit), addr); err != nil {
		t.Error(err)
	}
}

// rawPeer is the other end of a connection to a MetadataPeer, speaking the protocol by hand.
type rawPeer struct {
	conn net.Conn
	dec  pp.Decoder
}

func dialRaw(t *testing.T, m *MetadataPeer, h metainfo.Hash) *rawPeer {
	t.Helper()
	conn, err := net.Dial("tcp", m.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := pp.Handshake(deadline(t, closeLimit), conn, &h, [20]byte{}, pp.NewPeerExtensionBytes(pp.ExtensionBitLtep)); err != nil {
		t.Fatal(err)
	}
	return &rawPeer{conn: conn, dec: pp.Decoder{R: bufio.NewReader(conn), MaxLength: 1 << 20, Pool: &sync.Pool{New: func() any { b := make([]byte, 1<<14); return &b }}}}
}

func (r *rawPeer) send(t *testing.T, msg pp.Message) {
	t.Helper()
	b, err := msg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.conn.Write(b); err != nil {
		t.Fatal(err)
	}
}

func (r *rawPeer) next(t *testing.T) pp.Message {
	t.Helper()
	var msg pp.Message
	if err := r.dec.Decode(&msg); err != nil {
		t.Fatal(err)
	}
	return msg
}

func (r *rawPeer) request(t *testing.T, id pp.ExtensionNumber, piece int, kind pp.ExtendedMetadataRequestMsgType) {
	t.Helper()
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: id, ExtendedPayload: bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Piece: piece, Type: kind})})
}

func TestAMetadataPeerAnswersOnlyMetadataRequests(t *testing.T) {
	m, h, info := serveMetadata(t, "v1-single", MetadataOptions{})
	r := dialRaw(t, m, h)

	var theirs pp.ExtendedHandshakeMessage
	if msg := r.next(t); msg.Type != pp.Extended || msg.ExtendedID != pp.HandshakeExtendedID || bencode.Unmarshal(msg.ExtendedPayload, &theirs) != nil {
		t.Fatalf("first message %+v, want the extended handshake", msg)
	}
	id := theirs.M[pp.ExtensionNameMetadata]
	if id == 0 || theirs.MetadataSize != len(info) {
		t.Fatalf("extended handshake %+v, want ut_metadata and metadata_size %d", theirs, len(info))
	}

	const ours pp.ExtensionNumber = 3
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: pp.HandshakeExtendedID, ExtendedPayload: []byte("not bencode")})
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: pp.HandshakeExtendedID, ExtendedPayload: bencode.MustMarshal(pp.ExtendedHandshakeMessage{M: map[pp.ExtensionName]pp.ExtensionNumber{pp.ExtensionNameMetadata: ours}})})
	r.send(t, pp.Message{Type: pp.Interested})
	r.send(t, pp.Message{Type: pp.Piece, Index: 0, Begin: 0, Piece: []byte("a chunk nobody asked for")})
	r.send(t, pp.Message{Keepalive: true})
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: id, ExtendedPayload: []byte("not bencode")})
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: id + 1, ExtendedPayload: []byte("another extension")})
	r.request(t, id, 0, pp.RejectMetadataExtensionMsgType)
	r.request(t, id, 99, pp.RequestMetadataExtensionMsgType)
	r.request(t, id, -1, pp.RequestMetadataExtensionMsgType)
	r.request(t, id, 0, pp.RequestMetadataExtensionMsgType)

	for _, want := range []int{99, -1} {
		var reply pp.ExtendedMetadataRequestMsg
		msg := r.next(t)
		if err := bencode.Unmarshal(msg.ExtendedPayload, &reply); msg.ExtendedID != ours || err != nil || reply.Type != pp.RejectMetadataExtensionMsgType || reply.Piece != want {
			t.Fatalf("reply %+v (%v), want a reject of piece %d", reply, err, want)
		}
	}
	msg := r.next(t)
	var reply pp.ExtendedMetadataRequestMsg
	err := bencode.Unmarshal(msg.ExtendedPayload, &reply)
	var trailing bencode.ErrUnusedTrailingBytes
	if !errors.As(err, &trailing) || msg.ExtendedID != ours || reply.Type != pp.DataMetadataExtensionMsgType || reply.TotalSize != len(info) {
		t.Fatalf("reply %+v (%v), want the data of piece 0", reply, err)
	}
	if data := msg.ExtendedPayload[len(msg.ExtendedPayload)-len(info):]; !bytes.Equal(data, info) {
		t.Error("the data is not the info bytes")
	}
	if m.Served() != 1 {
		t.Errorf("Served = %d, want 1", m.Served())
	}

	m.Close()
	if _, err := r.conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("a read after Close = %v, want EOF", err)
	}
}

func TestAMetadataPeerHangsUpOnAnotherTorrentOrNoHandshake(t *testing.T) {
	m, _, _ := serveMetadata(t, "v1-single", MetadataOptions{})
	other := metaInfo(t, named(t, "v1-tree")).HashInfoBytes()
	conn, err := net.Dial("tcp", m.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := pp.Handshake(deadline(t, closeLimit), conn, &other, [20]byte{}, pp.PeerExtensionBits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(conn); err != nil {
		t.Errorf("after a handshake for another torrent: %v, want the connection closed", err)
	}

	garbage, err := net.Dial("tcp", m.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer garbage.Close()
	if _, err := garbage.Write(make([]byte, 68)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(garbage); err != nil {
		t.Errorf("after a garbage handshake: %v, want the connection closed", err)
	}
}

// A peer may lie about the metadata size and send its own messages once it learns the engine's
// ut_metadata number, which is what a hostile peer does.
func TestAMetadataPeerCanLieAboutTheSizeAndSendItsOwnMessages(t *testing.T) {
	mi := metaInfo(t, named(t, "v1-single"))
	got := make(chan pp.ExtensionNumber, 1)
	m, err := ServeMetadata(mi.HashInfoBytes(), mi.InfoBytes, MetadataOptions{
		MetadataSize: 1 << 20,
		Attack: func(send func(pp.Message), utMetadata pp.ExtensionNumber) {
			send(pp.Message{Type: pp.Extended, ExtendedID: utMetadata, ExtendedPayload: bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Type: pp.RejectMetadataExtensionMsgType})})
			got <- utMetadata
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	r := dialRaw(t, m, mi.HashInfoBytes())
	var hs pp.ExtendedHandshakeMessage
	for hs.MetadataSize == 0 {
		msg := r.next(t)
		if msg.Type == pp.Extended && msg.ExtendedID == pp.HandshakeExtendedID {
			if err := bencode.Unmarshal(msg.ExtendedPayload, &hs); err != nil {
				t.Fatal(err)
			}
		}
	}
	if hs.MetadataSize != 1<<20 {
		t.Errorf("metadata_size %d, want the lie 1<<20", hs.MetadataSize)
	}
	const ours pp.ExtensionNumber = 3
	r.send(t, pp.Message{Type: pp.Extended, ExtendedID: pp.HandshakeExtendedID, ExtendedPayload: bencode.MustMarshal(pp.ExtendedHandshakeMessage{M: map[pp.ExtensionName]pp.ExtensionNumber{pp.ExtensionNameMetadata: ours}})})
	if msg := r.next(t); msg.ExtendedID != ours {
		t.Errorf("the attack sent on extended id %d, want ours %d", msg.ExtendedID, ours)
	}
	if utMetadata := <-got; utMetadata != ours {
		t.Errorf("the attack saw ut_metadata %d, want %d", utMetadata, ours)
	}
	if m.Served() != 0 {
		t.Errorf("an attacking peer served %d pieces, want 0", m.Served())
	}
}

func TestAMetadataPeerThatCannotListenSaysSo(t *testing.T) {
	replace(t, &listen, func(string, string) (net.Listener, error) { return nil, errors.New("no socket") })
	if _, err := ServeMetadata(metainfo.Hash{1}, []byte("d4:name1:xe"), MetadataOptions{}); err == nil {
		t.Error("ServeMetadata without a socket succeeded")
	}
}

// A peer that has every piece says so before anything else, so the engine plans requests to it.
func TestAMetadataPeerWithEveryPieceSendsItsBitfieldAndUnchokes(t *testing.T) {
	m, h, _ := serveMetadata(t, "v1-tree", MetadataOptions{HaveAll: true}) // 3 pieces
	r := dialRaw(t, m, h)
	if msg := r.next(t); msg.Type != pp.Bitfield || len(msg.Bitfield) < 3 || !msg.Bitfield[0] || !msg.Bitfield[2] {
		t.Fatalf("first message %+v, want a bitfield of three pieces", msg)
	}
	if msg := r.next(t); msg.Type != pp.Unchoke {
		t.Fatalf("second message %+v, want an unchoke", msg)
	}
	if msg := r.next(t); msg.Type != pp.Extended || msg.ExtendedID != pp.HandshakeExtendedID {
		t.Fatalf("third message %+v, want the extended handshake", msg)
	}
	if _, err := ServeMetadata(h, []byte("not an info"), MetadataOptions{HaveAll: true}); err == nil {
		t.Error("a peer with every piece of an info it cannot read started")
	}
}
