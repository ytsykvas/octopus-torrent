package pendingmagnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"

	"octopustorrent/internal/torrentengine/infocheck"
)

func TestVettedMetadataHashesToTheRequestedInfohash(t *testing.T) {
	_, mi := named(t, "v1-tree")
	info, err := vet(mi.HashInfoBytes(), mi.InfoBytes, limits)
	if err != nil || info.Name != "tree" {
		t.Fatalf("vet = %v, %v", info, err)
	}
	_, other := named(t, "v1-single")
	if _, err := vet(other.HashInfoBytes(), mi.InfoBytes, limits); !errors.Is(err, ErrInfoMismatch) {
		t.Errorf("another torrent's info: %v, want ErrInfoMismatch", err)
	}
}

// The engine keeps the first info it applied, while a later SetInfoBytes on the same torrent (another
// add path) replaces only the bytes (finding 31). Vetted bytes are refused unless they are the info
// the engine applied, so the folder is never opened with a stranger's file list.
func TestVettedBytesMustBeTheInfoTheEngineApplied(t *testing.T) {
	_, mi := named(t, "v1-tree")
	_, other := named(t, "v1-single")
	if err := sameInfo(applied(t, mi.InfoBytes), applied(t, other.InfoBytes)); !errors.Is(err, ErrInfoReplaced) {
		t.Errorf("bytes that are not the applied info: %v, want ErrInfoReplaced", err)
	}
	if err := sameInfo(applied(t, mi.InfoBytes), applied(t, mi.InfoBytes)); err != nil {
		t.Errorf("the applied info itself: %v", err)
	}
}

func applied(t *testing.T, infoBytes []byte) *metainfo.Info {
	t.Helper()
	var info metainfo.Info
	if err := bencode.Unmarshal(infoBytes, &info); err != nil {
		t.Fatal(err)
	}
	return &info
}

// A btih may be the first 20 bytes of a v2 infohash; the engine then rewrites the torrent's infohash.
// The check compares with what was requested, so such a magnet is a mismatch (v2 magnets are not
// supported, 007).
func TestATruncatedV2InfohashIsAMismatch(t *testing.T) {
	_, mi := named(t, "hybrid-tree")
	full := infohash_v2.HashBytes(mi.InfoBytes)
	if _, err := vet(*full.ToShort(), mi.InfoBytes, limits); !errors.Is(err, ErrInfoMismatch) {
		t.Errorf("truncated v2 hash: %v, want ErrInfoMismatch", err)
	}
}

// Bytes that hash right are still parsed only once infocheck has scanned them.
func TestMetadataThatCannotBeReadIsRefused(t *testing.T) {
	garbage := []byte("not bencode")
	if _, err := vet(metainfo.HashBytes(garbage), garbage, limits); !errors.Is(err, infocheck.ErrMalformed) {
		t.Errorf("unreadable info: %v, want infocheck.ErrMalformed", err)
	}
}

// A request that cannot work is refused before the engine is touched: the client is nil.
func TestARequestThatCannotWorkNeverReachesTheEngine(t *testing.T) {
	_, mi := named(t, "v1-single")
	done, cancel := context.WithCancel(context.Background())
	cancel()
	for _, c := range []struct {
		name string
		ctx  context.Context
		req  Request
		want error
	}{
		{"no wait", context.Background(), Request{InfoHash: mi.HashInfoBytes()}, ErrInvalidWait},
		{"negative wait", context.Background(), Request{InfoHash: mi.HashInfoBytes(), Wait: -time.Second}, ErrInvalidWait},
		{"no infohash", context.Background(), Request{Wait: time.Second}, ErrInvalidSource},
		{"no limits", context.Background(), Request{InfoHash: mi.HashInfoBytes(), Wait: time.Second}, infocheck.ErrInvalidLimits},
		{"canceled", done, Request{InfoHash: mi.HashInfoBytes(), Wait: time.Second, Limits: limits}, context.Canceled},
	} {
		if p, err := NewGate(torrent.NewDefaultClientConfig()).Fetch(c.ctx, nil, c.req); p != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: Fetch = %v, %v; want %v", c.name, p, err, c.want)
		}
	}
}
