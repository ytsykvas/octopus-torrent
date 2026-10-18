package pendingmagnet

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	pp "github.com/anacrolix/torrent/peer_protocol"

	"octopustorrent/internal/torrentengine/filestore"
	"octopustorrent/internal/torrentengine/infocheck"
	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

func TestFetchGetsVerifiedMetadataAndThePendingImportMovesNoPayload(t *testing.T) {
	for _, name := range []string{"v1-single", "v1-tree", "hybrid-tree"} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, name)
			sd := seeder(t, fx, mi, 0)
			l := newLeecher(t)
			before := tree(t, l.dirs()...)

			p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
			if err != nil {
				t.Fatal(err)
			}
			want, err := mi.UnmarshalInfo()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(p.InfoBytes(), mi.InfoBytes) || p.Info().Name != want.Name {
				t.Fatalf("the pending import holds %q, want the fixture's verified info %q", p.Info().Name, want.Name)
			}

			// The confirmation dialog is open: the seeder stays connected and nothing moves.
			time.Sleep(holdWindow)
			pending, ok := l.client.Torrent(mi.HashInfoBytes())
			if !ok || pending.Stats().ActivePeers == 0 {
				t.Error("the pending torrent lost its seeder while waiting for the confirmation")
			}
			if read, written := l.payload(); read != 0 || written != 0 {
				t.Errorf("before the confirmation the client read %d and wrote %d payload bytes", read, written)
			}
			if l.protocolRead() == 0 {
				t.Error("no protocol bytes were read: the metadata came from nowhere")
			}
			st, _ := sd.Client().Torrent(mi.HashInfoBytes())
			if got := swarm.Count(st).PayloadWritten; got != 0 {
				t.Errorf("the seeder wrote %d payload bytes before the confirmation", got)
			}
			if got := p.Touched(); got != 0 {
				t.Errorf("the engine touched the pending storage %d times", got)
			}
			sameTree(t, before, l.dirs()...)

			if err := p.Cancel(); err != nil {
				t.Fatal(err)
			}
			l.empty(t)
		})
	}
}

func TestFetchWithNoPeerEndsAtItsLimit(t *testing.T) {
	_, mi := named(t, "v1-single")
	l := newLeecher(t)
	const wait = 300 * time.Millisecond
	start := time.Now()
	p, err := l.gate.Fetch(context.Background(), l.client, Request{InfoHash: mi.HashInfoBytes(), Wait: wait, Limits: limits})
	if p != nil || !errors.Is(err, ErrNoMetadata) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch = %v, %v; want ErrNoMetadata wrapping DeadlineExceeded", p, err)
	}
	if took := time.Since(start); took < wait || took > wait+promptLimit {
		t.Errorf("Fetch returned after %v, want the limit %v", took, wait)
	}
	l.empty(t)
}

func TestACanceledFetchReturnsTheContextErrorAndLeavesNothing(t *testing.T) {
	_, mi := named(t, "v1-single")
	l := newLeecher(t)
	before := tree(t, l.dirs()...)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	p, err := l.gate.Fetch(ctx, l.client, Request{InfoHash: mi.HashInfoBytes(), Wait: transferLimit, Limits: limits})
	if p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch = %v, %v; want context.Canceled", p, err)
	}
	if took := time.Since(start); took > 200*time.Millisecond+promptLimit {
		t.Errorf("the cancel took %v to end the fetch", took)
	}
	l.empty(t)
	sameTree(t, before, l.dirs()...)
}

// The import is cancelled at the moment the metadata arrives. Whichever comes first, the import ends
// with nothing left: either the fetch reports the cancel, or it returned the metadata just before
// and the caller's cancel drops it.
func TestACancelAsTheMetadataArrivesLeavesNothing(t *testing.T) {
	fx, mi := named(t, "v1-tree")
	sd := seeder(t, fx, mi, 0)
	var yielded, canceled int
	defer func() {
		t.Logf("%d runs returned the metadata before the cancel, %d returned the cancel", yielded, canceled)
	}()
	for run := range 20 {
		l := newLeecher(t)
		before := tree(t, l.dirs()...)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		watched := make(chan struct{})
		go func() {
			defer close(watched)
			for {
				if pending, ok := l.client.Torrent(mi.HashInfoBytes()); ok {
					select {
					case <-pending.GotInfo():
						cancel()
					case <-done:
					}
					return
				}
				select {
				case <-done:
					return
				case <-time.After(time.Millisecond):
				}
			}
		}()
		p, err := l.gate.Fetch(ctx, l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		close(done)
		<-watched
		cancel()
		switch {
		case err == nil:
			yielded++
			if got := p.Touched(); got != 0 {
				t.Errorf("run %d: the engine touched the pending storage %d times", run, got)
			}
			if err := p.Cancel(); err != nil {
				t.Errorf("run %d: Cancel = %v", run, err)
			}
		case errors.Is(err, context.Canceled):
			canceled++
		default:
			t.Fatalf("run %d: Fetch = %v, want the metadata or context.Canceled", run, err)
		}
		l.empty(t)
		if read, written := l.payload(); read != 0 || written != 0 {
			t.Errorf("run %d: payload read %d, written %d", run, read, written)
		}
		sameTree(t, before, l.dirs()...)
	}
}

// A peer that sends another torrent's info dictionary under the requested infohash: the engine takes
// it (finding 11), the hash check refuses it, and the pending storage made none of its files.
func TestFetchRefusesMetadataThatDoesNotMatchTheInfohash(t *testing.T) {
	_, want := named(t, "v1-single")
	other, otherMi := named(t, "v1-tree")
	liar := newPeer(t, swarm.Options{})
	if _, err := liar.Lie(deadline(t, transferLimit), want.HashInfoBytes(), otherMi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	if other.Files[1].Length != 0 {
		t.Fatalf("fixture %s changed: file %v is not the empty one", other.Name, other.Files[1].Path)
	}

	l := newLeecher(t)
	before := tree(t, l.dirs()...)
	p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: want.HashInfoBytes(), Peers: swarm.PeerInfos(liar), Wait: transferLimit, Limits: limits})
	if p != nil || !errors.Is(err, ErrInfoMismatch) {
		t.Fatalf("Fetch from a liar = %v, %v; want ErrInfoMismatch", p, err)
	}
	l.empty(t)
	sameTree(t, before, l.dirs()...)
	if read, written := l.payload(); read != 0 || written != 0 {
		t.Errorf("payload read %d, written %d", read, written)
	}
}

// infoWith is a fixture's info bytes changed as a hostile peer would change them.
func infoWith(t *testing.T, name string, edit func(m map[string]any)) []byte {
	t.Helper()
	_, mi := named(t, name)
	var m map[string]any
	if err := bencode.Unmarshal(mi.InfoBytes, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	return bencode.MustMarshal(m)
}

// A hostile peer serves an info whose SHA-1 is the magnet's btih, so it passes every hash check, and
// which crashes or stalls the pinned engine once the engine parses or applies it (findings 33–39),
// some before any storage hook. The gate keeps the pieces from the engine, and Fetch refuses the
// bytes before it gives them to the engine: Fetch ends at once and leaves nothing. Without the gate
// this test binary dies.
func TestFetchRefusesHostileMetadataBeforeTheEngineParsesIt(t *testing.T) {
	serve := func(t *testing.T, infoBytes []byte) (*swarm.MetadataPeer, metainfo.Hash) {
		t.Helper()
		h := metainfo.HashBytes(infoBytes)
		m, err := swarm.ServeMetadata(h, infoBytes, swarm.MetadataOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(m.Close)
		return m, h
	}
	fetch := func(t *testing.T, infoBytes []byte, l infocheck.Limits, want error) {
		t.Helper()
		m, h := serve(t, infoBytes)
		lc := newLeecher(t)
		before := tree(t, lc.dirs()...)
		start := time.Now()
		p, err := lc.gate.Fetch(deadline(t, transferLimit), lc.client, Request{InfoHash: h, Peers: []torrent.PeerInfo{m.PeerInfo()}, Wait: transferLimit, Limits: l})
		if p != nil || !errors.Is(err, want) {
			t.Fatalf("Fetch = %v, %v; want %v", p, err, want)
		}
		if took := time.Since(start); took > transferLimit/2 {
			t.Errorf("the refusal took %v: Fetch waited instead of ending at the refusal", took)
		}
		if m.Served() == 0 {
			t.Error("the hostile peer served nothing: the refusal was not about its metadata")
		}
		lc.empty(t)
		sameTree(t, before, lc.dirs()...)
		if read, written := lc.payload(); read != 0 || written != 0 {
			t.Errorf("payload read %d, written %d", read, written)
		}
	}
	for _, h := range fixture.HostileInfos() {
		t.Run(h.Name, func(t *testing.T) {
			want := map[int]error{33: infocheck.ErrV2Only, 37: infocheck.ErrMalformed}[h.Finding]
			fetch(t, h.InfoBytes, limits, cmp.Or(want, infocheck.ErrInconsistentInfo))
		})
	}
	// Finding 38: 20 000 nested folders, about 100 KB, cost the engine 18–30 s under its lock.
	t.Run("a deeply nested file tree", func(t *testing.T) {
		_, mi := named(t, "v1-single")
		deep := "9:file tree" + strings.Repeat("d1:a", 20_000) + "de" + strings.Repeat("e", 20_000)
		fetch(t, bytes.Replace(mi.InfoBytes, []byte("6:lengthi"), []byte(deep+"6:lengthi"), 1), limits, infocheck.ErrMalformed)
	})
	// Finding 39: a piece length past the engine's request index panics it once a peer has pieces.
	t.Run("a piece length of 2^32", func(t *testing.T) {
		fetch(t, infoWith(t, "v1-single", func(m map[string]any) {
			m["piece length"], m["pieces"] = int64(1)<<32, string(make([]byte, 20))
		}), limits, infocheck.ErrInfoTooLarge)
	})
	t.Run("more files than the limit", func(t *testing.T) {
		_, mi := named(t, "v1-tree")
		fetch(t, mi.InfoBytes, infocheck.Limits{Files: 2, Pieces: 1000, MetadataBytes: 1 << 20}, infocheck.ErrInfoTooLarge)
	})
	t.Run("more metadata than the limit", func(t *testing.T) {
		_, mi := named(t, "v1-tree")
		fetch(t, mi.InfoBytes, infocheck.Limits{Files: 100, Pieces: 1000, MetadataBytes: len(mi.InfoBytes) - 1}, infocheck.ErrInfoTooLarge)
	})
}

// A peer that serves the real metadata but also sends a message that crashes the pinned engine
// (findings 41–43): the gate neutralizes each, so Fetch still returns the import and the engine
// lives. Without the gate's callbacks the child dies (TestPinnedEnginePanicsOnAPeersMessage).
func TestFetchSurvivesAPeersCrashingMessages(t *testing.T) {
	for name, a := range map[string]struct {
		size int
		send func(send func(pp.Message), utMetadata pp.ExtensionNumber)
	}{
		"a lie about the metadata size": {1 << 62, nil},
		"a v2 hashes message": {0, func(send func(pp.Message), _ pp.ExtensionNumber) {
			send(pp.Message{Type: pp.Hashes, PiecesRoot: [32]byte{1}})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			_, mi := named(t, "v1-single")
			// The peer both serves the metadata and attacks: send the attack, then the pieces.
			served := make(chan struct{})
			m, err := swarm.ServeMetadata(mi.HashInfoBytes(), mi.InfoBytes, swarm.MetadataOptions{MetadataSize: a.size, Attack: func(send func(pp.Message), ut pp.ExtensionNumber) {
				if a.send != nil {
					a.send(send, ut)
				}
				for begin := 0; begin < len(mi.InfoBytes); begin += 1 << 14 {
					end := min(begin+1<<14, len(mi.InfoBytes))
					header := bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Type: pp.DataMetadataExtensionMsgType, Piece: begin >> 14, TotalSize: len(mi.InfoBytes)})
					send(pp.Message{Type: pp.Extended, ExtendedID: ut, ExtendedPayload: append(header, mi.InfoBytes[begin:end]...)})
				}
				close(served)
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			l := newLeecher(t)
			p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: []torrent.PeerInfo{m.PeerInfo()}, Wait: transferLimit, Limits: limits})
			if err != nil || p == nil {
				t.Fatalf("Fetch = %v, %v; want the import despite the attack", p, err)
			}
			<-served
		})
	}
}

// A peer that lies about one metadata piece ends the fetch, and the error names the peers that
// took part, so the caller can retry without them.
func TestFetchNamesThePeersThatSentRefusedMetadata(t *testing.T) {
	_, mi := named(t, "v1-tree")
	m, err := swarm.ServeMetadata(mi.HashInfoBytes(), mi.InfoBytes, swarm.MetadataOptions{Attack: func(send func(pp.Message), ut pp.ExtensionNumber) {
		send(pp.Message{Type: pp.Extended, ExtendedID: ut, ExtendedPayload: bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{Type: pp.DataMetadataExtensionMsgType, Piece: 99, TotalSize: len(mi.InfoBytes)})})
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	l := newLeecher(t)
	_, err = l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: []torrent.PeerInfo{m.PeerInfo()}, Wait: transferLimit, Limits: limits})
	host, _, _ := net.SplitHostPort(m.Addr().String())
	var pe *PeerError
	if !errors.As(err, &pe) || len(pe.Peers) != 1 || pe.Peers[0] != host {
		t.Fatalf("Fetch = %v; want a PeerError naming %s", err, host)
	}
}

func TestFetchOfAKnownInfohashLeavesTheExistingTorrentAlone(t *testing.T) {
	fx, mi := named(t, "v1-large")
	sd := seeder(t, fx, mi, 256<<10)
	l := newLeecher(t)
	root := t.TempDir()
	existing, _ := l.client.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), Storage: final(t, l, root)})
	if err := existing.SetInfoBytes(mi.InfoBytes); err != nil {
		t.Fatal(err)
	}
	existing.DownloadAll()
	swarm.Connect(existing, sd)

	if p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits}); p != nil || !errors.Is(err, ErrDuplicate) {
		t.Fatalf("Fetch of a known infohash = %v, %v; want ErrDuplicate", p, err)
	}
	_, single := named(t, "v1-single")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := l.gate.Fetch(ctx, l.client, Request{InfoHash: single.HashInfoBytes(), Wait: transferLimit, Limits: limits}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a cancelled fetch of another torrent = %v", err)
	}

	if isClosed(existing.Closed()) {
		t.Fatal("the existing torrent was closed")
	}
	if got := l.client.Torrents(); len(got) != 1 || got[0] != existing {
		t.Fatalf("the client holds %d torrents, want only the existing one", len(got))
	}
	if got := existing.BytesCompleted(); got == existing.Length() {
		t.Fatal("the existing torrent was already complete: the test shows nothing about its download")
	}
	if err := swarm.WaitComplete(deadline(t, transferLimit), existing); err != nil {
		t.Fatalf("the existing torrent stopped downloading: %v", err)
	}
	filesAreTheFixture(t, fx, mi, root)
}

func TestShutdownEndsAFetchAndRefusesLaterCalls(t *testing.T) {
	fx, mi := named(t, "v1-tree")

	t.Run("during the wait", func(t *testing.T) {
		l := newLeecher(t)
		time.AfterFunc(200*time.Millisecond, func() { l.client.Close() })
		start := time.Now()
		if p, err := l.gate.Fetch(context.Background(), l.client, Request{InfoHash: mi.HashInfoBytes(), Wait: transferLimit, Limits: limits}); p != nil || !errors.Is(err, ErrClientClosed) {
			t.Fatalf("Fetch = %v, %v; want ErrClientClosed", p, err)
		}
		if took := time.Since(start); took > 200*time.Millisecond+promptLimit {
			t.Errorf("the shutdown took %v to end the fetch", took)
		}
	})

	t.Run("a fetch after the shutdown", func(t *testing.T) {
		l := newLeecher(t)
		l.client.Close()
		if p, err := l.gate.Fetch(context.Background(), l.client, Request{InfoHash: mi.HashInfoBytes(), Wait: transferLimit, Limits: limits}); p != nil || !errors.Is(err, ErrClientClosed) {
			t.Fatalf("Fetch = %v, %v; want ErrClientClosed", p, err)
		}
		if _, ok := l.client.Torrent(mi.HashInfoBytes()); ok {
			t.Error("the closed client still holds the torrent")
		}
	})

	t.Run("a confirmation after the shutdown", func(t *testing.T) {
		sd := seeder(t, fx, mi, 0)
		l := newLeecher(t)
		p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		storage := final(t, l, root)
		l.client.Close()
		if tor, err := p.Confirm(context.Background(), Final{Storage: storage, Download: true}); tor != nil || !errors.Is(err, ErrClientClosed) {
			t.Fatalf("Confirm = %v, %v; want ErrClientClosed", tor, err)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Errorf("the chosen folder got %d entries after the shutdown", len(entries))
		}
	})
}

// Another holder of the same torrent drops it while the import waits or before it is confirmed.
func TestATorrentClosedByAnotherHolderEndsTheImport(t *testing.T) {
	fx, mi := named(t, "v1-single")

	t.Run("during the wait", func(t *testing.T) {
		l := newLeecher(t)
		go func() {
			for {
				if pending, ok := l.client.Torrent(mi.HashInfoBytes()); ok {
					time.Sleep(100 * time.Millisecond)
					pending.Drop()
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
		if p, err := l.gate.Fetch(context.Background(), l.client, Request{InfoHash: mi.HashInfoBytes(), Wait: transferLimit, Limits: limits}); p != nil || !errors.Is(err, ErrTorrentClosed) {
			t.Fatalf("Fetch = %v, %v; want ErrTorrentClosed", p, err)
		}
	})

	t.Run("before the confirmation", func(t *testing.T) {
		sd := seeder(t, fx, mi, 0)
		l := newLeecher(t)
		p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		pending, _ := l.client.Torrent(mi.HashInfoBytes())
		pending.Drop()
		root := t.TempDir()
		if tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, root)}); tor != nil || !errors.Is(err, ErrTorrentClosed) {
			t.Fatalf("Confirm = %v, %v; want ErrTorrentClosed", tor, err)
		}
		if entries, _ := os.ReadDir(root); len(entries) != 0 {
			t.Errorf("the chosen folder got %d entries", len(entries))
		}
	})
}

func TestConfirmPutsTheFilesExactlyInTheChosenFolder(t *testing.T) {
	for _, name := range []string{"v1-tree", "hybrid-tree"} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, name)
			sd := seeder(t, fx, mi, 0)
			l := newLeecher(t)
			p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
			if err != nil {
				t.Fatal(err)
			}

			if tor, err := p.Confirm(context.Background(), Final{Download: true}); tor != nil || !errors.Is(err, filestore.ErrNoFinalStorage) {
				t.Fatalf("Confirm without a storage = %v, %v; want ErrNoFinalStorage", tor, err)
			}

			// The first folder holds a user's file at one of the torrent's paths. Its pieces are
			// cached as not complete, so the engine would overwrite the file: Confirm refuses the
			// folder before it is opened, nothing is created there, the user's file stays, and the
			// import is still waiting for a folder.
			first := t.TempDir()
			userFile := filepath.Join(first, "tree", "media", "readme.txt")
			if err := os.MkdirAll(filepath.Dir(userFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(userFile, []byte("the user's"), 0o644); err != nil {
				t.Fatal(err)
			}
			firstBefore := tree(t, first)
			if tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, first), Download: true}); tor != nil || !errors.Is(err, filestore.ErrPathExists) {
				t.Fatalf("Confirm into a folder with a file at a torrent path = %v, %v; want ErrPathExists", tor, err)
			}
			sameTree(t, firstBefore, first)

			// In the second folder the torrent's folder is a symlink to another folder: nothing
			// exists at the torrent's paths, but the guard refuses the folder when the engine
			// storage opens it, and the import is still waiting.
			linked, elsewhere := t.TempDir(), t.TempDir()
			if err := os.Symlink(elsewhere, filepath.Join(linked, "tree")); err != nil {
				t.Fatal(err)
			}
			linkedBefore := tree(t, linked, elsewhere)
			if tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, linked), Download: true}); tor != nil || !errors.Is(err, filestore.ErrUnsafePath) {
				t.Fatalf("Confirm into a folder whose torrent folder is a symlink = %v, %v; want ErrUnsafePath", tor, err)
			}
			sameTree(t, linkedBefore, linked, elsewhere)

			chosen := t.TempDir()
			dataBefore := tree(t, l.dirs()...)
			tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, chosen), Download: true, Upload: true})
			if err != nil {
				t.Fatal(err)
			}
			tor.DownloadAll()
			if err := swarm.WaitComplete(deadline(t, transferLimit), tor); err != nil {
				t.Fatalf("the confirmed torrent did not complete over the connection it kept: %v", err)
			}
			filesAreTheFixture(t, fx, mi, chosen)
			sameTree(t, firstBefore, first)
			sameTree(t, dataBefore, l.dirs()...)
			if got := p.Touched(); got != 0 {
				t.Errorf("the engine touched the pending storage %d times before the attach", got)
			}

			l.client.Close()
			filesAreTheFixture(t, fx, mi, chosen)
			if data, err := os.ReadFile(userFile); err != nil || string(data) != "the user's" {
				t.Errorf("the user's file in the refused folder is %q, %v", data, err)
			}
		})
	}
}

func TestConfirmAndCancelRaceToExactlyOneOutcome(t *testing.T) {
	fx, mi := named(t, "v1-single")
	sd := seeder(t, fx, mi, 0)
	fetch := func(t *testing.T, l leecher) *Pending {
		t.Helper()
		p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	for run := range 5 {
		l := newLeecher(t)
		p := fetch(t, l)
		roots := []string{t.TempDir(), t.TempDir()}
		storages := []*filestore.TorrentStorage{final(t, l, roots[0]), final(t, l, roots[1])}
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i, s := range storages {
			wg.Go(func() { _, errs[i] = p.Confirm(context.Background(), Final{Storage: s}) })
		}
		wg.Wait()
		won := 0
		for i, err := range errs {
			entries, _ := os.ReadDir(roots[i])
			switch {
			case err == nil:
				won++
			case errors.Is(err, ErrConfirmed):
				if len(entries) != 0 {
					t.Errorf("run %d: the losing folder got %d entries", run, len(entries))
				}
			default:
				t.Errorf("run %d: Confirm = %v", run, err)
			}
		}
		if won != 1 {
			t.Errorf("run %d: %d confirmations won, want exactly one", run, won)
		}
	}

	for run := range 5 {
		l := newLeecher(t)
		p := fetch(t, l)
		root := t.TempDir()
		storage := final(t, l, root)
		var confirmErr, cancelErr error
		var tor *torrent.Torrent
		var wg sync.WaitGroup
		wg.Go(func() { tor, confirmErr = p.Confirm(context.Background(), Final{Storage: storage}) })
		wg.Go(func() { cancelErr = p.Cancel() })
		wg.Wait()
		switch {
		case confirmErr == nil && errors.Is(cancelErr, ErrConfirmed):
			if isClosed(tor.Closed()) {
				t.Errorf("run %d: the confirmed torrent was closed by the losing cancel", run)
			}
		case cancelErr == nil && errors.Is(confirmErr, ErrCanceled):
			l.empty(t)
			if entries, _ := os.ReadDir(root); len(entries) != 0 {
				t.Errorf("run %d: a cancelled import's folder got %d entries", run, len(entries))
			}
		default:
			t.Errorf("run %d: Confirm = %v, Cancel = %v; want exactly one to win", run, confirmErr, cancelErr)
		}
	}
}

func TestConfirmAppliesTheSwitches(t *testing.T) {
	fx, mi := named(t, "v1-large")

	t.Run("no download until started", func(t *testing.T) {
		sd := seeder(t, fx, mi, 0)
		l := newLeecher(t)
		p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, t.TempDir()), Upload: true})
		if err != nil {
			t.Fatal(err)
		}
		tor.DownloadAll()
		time.Sleep(holdWindow)
		if read, _ := l.payload(); read != 0 {
			t.Errorf("a torrent confirmed without start read %d payload bytes", read)
		}
	})

	t.Run("no upload while the global stop is on", func(t *testing.T) {
		sd := seeder(t, fx, mi, 128<<10)
		l := newLeecher(t)
		p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
		if err != nil {
			t.Fatal(err)
		}
		tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, t.TempDir()), Download: true})
		if err != nil {
			t.Fatal(err)
		}
		tor.DownloadAll()
		other := newPeer(t, swarm.Options{})
		ot, err := other.Leech(mi)
		if err != nil {
			t.Fatal(err)
		}
		swarm.Connect(ot, l.peer)
		time.Sleep(holdWindow)
		if tor.BytesCompleted() == 0 || tor.BytesCompleted() == tor.Length() {
			t.Fatalf("the confirmed torrent holds %d of %d bytes: it was not downloading during the check", tor.BytesCompleted(), tor.Length())
		}
		if got := swarm.Count(ot).PayloadRead; got != 0 {
			t.Errorf("a peer got %d payload bytes from a torrent confirmed under the global stop", got)
		}
	})
}

func TestCallsAfterTheEndAreRefused(t *testing.T) {
	fx, mi := named(t, "v1-single")
	sd := seeder(t, fx, mi, 0)
	l := newLeecher(t)
	p, err := l.gate.Fetch(deadline(t, transferLimit), l.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	tor, err := p.Confirm(context.Background(), Final{Storage: final(t, l, root), Download: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Cancel(); !errors.Is(err, ErrConfirmed) {
		t.Errorf("Cancel after Confirm = %v, want ErrConfirmed", err)
	}
	if again, err := p.Confirm(context.Background(), Final{Storage: final(t, l, t.TempDir())}); again != nil || !errors.Is(err, ErrConfirmed) {
		t.Errorf("a second Confirm = %v, %v; want ErrConfirmed", again, err)
	}
	tor.DownloadAll()
	if err := swarm.WaitComplete(deadline(t, transferLimit), tor); err != nil {
		t.Fatalf("the confirmed torrent stopped after the refused calls: %v", err)
	}
	filesAreTheFixture(t, fx, mi, root)

	l2 := newLeecher(t)
	p2, err := l2.gate.Fetch(deadline(t, transferLimit), l2.client, Request{InfoHash: mi.HashInfoBytes(), Peers: swarm.PeerInfos(sd), Wait: transferLimit, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(p2.Cancel(), p2.Cancel()); err != nil {
		t.Errorf("two Cancels = %v, want nil", err)
	}
	root2 := t.TempDir()
	if tor, err := p2.Confirm(context.Background(), Final{Storage: final(t, l2, root2)}); tor != nil || !errors.Is(err, ErrCanceled) {
		t.Errorf("Confirm after Cancel = %v, %v; want ErrCanceled", tor, err)
	}
	l2.empty(t)
	if entries, _ := os.ReadDir(root2); len(entries) != 0 {
		t.Errorf("a cancelled import's folder got %d entries", len(entries))
	}
}
