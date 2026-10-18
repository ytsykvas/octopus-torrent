package infocheck

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/filestore"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// fuzzLimits keep an admitted info small enough for the engine to apply it many times a second.
var fuzzLimits = Limits{Files: 1000, Pieces: 1 << 16, MetadataBytes: 1 << 20}

// engine is one client shared by every fuzz input of a process, created on first use and closed
// after the tests, before swarm.MainClassicIO checks that no engine goroutine is left.
var engine struct {
	once sync.Once
	peer *swarm.Peer
	dir  string
	err  error
}

func sharedClient(t *testing.T) *torrent.Client {
	t.Helper()
	engine.once.Do(func() {
		engine.dir, engine.err = os.MkdirTemp("", "infocheck-fuzz-")
		if engine.err == nil {
			engine.peer, engine.err = swarm.NewPeer(engine.dir, swarm.Options{})
		}
	})
	if engine.err != nil {
		t.Fatal(engine.err)
	}
	return engine.peer.Client()
}

// closeEngine closes the shared client, if a test started it, and removes its folder.
func closeEngine() error {
	if engine.peer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := engine.peer.Close(ctx)
	if rmErr := os.RemoveAll(engine.dir); err == nil {
		err = rmErr
	}
	return err
}

// FuzzCheckBytes holds the rule to its promise for any bytes: CheckBytes never panics, and bytes it
// admits never panic the pinned engine when the engine applies them the way Fetch does (through a
// filestore.Deferred that admits with Check) and is then asked for its files, pieces and stats.
// The seeds are every catalog fixture and every hostile shape under the fuzz size limit. The
// engine's piece-order assert (finding 33) is sampled, so a run catches that one only on the inputs
// it happens to check; no peer has pieces, so request planning (finding 39) is not exercised.
func FuzzCheckBytes(f *testing.F) {
	for _, fx := range fixture.All() {
		if mi, err := fx.MetaInfo(); err == nil {
			f.Add([]byte(mi.InfoBytes))
		}
	}
	for _, h := range fixture.HostileInfos() {
		if len(h.InfoBytes) <= fuzzLimits.MetadataBytes {
			f.Add(h.InfoBytes)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if _, err := CheckBytes(b, fuzzLimits); err != nil {
			return
		}
		applyAdmitted(t, sharedClient(t), b)
	})
}

// applyAdmitted applies info bytes that Check admitted and reads back what the product will read.
// The engine may still refuse them for its own reasons; it must not panic.
func applyAdmitted(t *testing.T, client *torrent.Client, b []byte) {
	t.Helper()
	store := filestore.NewDeferred(func(info *metainfo.Info) error { return Check(info, fuzzLimits) })
	lt, isNew := client.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: metainfo.HashBytes(b), Storage: store, DisableInitialPieceCheck: true})
	if !isNew {
		t.Fatal("the shared client still holds this infohash")
	}
	defer lt.Drop()
	if err := lt.SetInfoBytes(b); err != nil {
		return
	}
	for _, f := range lt.Files() {
		_, _, _ = f.Path(), f.Length(), f.Offset()
	}
	for i := range lt.NumPieces() {
		_ = lt.PieceState(i)
	}
	lt.DownloadAll()
	_ = lt.Stats()
}
