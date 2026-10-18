package pendingmagnet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	infohash_v2 "github.com/anacrolix/torrent/types/infohash-v2"
)

func TestVerifiedMetadataHashesToTheRequestedInfohash(t *testing.T) {
	_, mi := named(t, "v1-tree")
	info, err := verify(mi.HashInfoBytes(), mi.InfoBytes, applied(t, mi.InfoBytes))
	if err != nil || info.Name != "tree" {
		t.Fatalf("verify = %q, %v", info.Name, err)
	}
	_, other := named(t, "v1-single")
	if _, err := verify(other.HashInfoBytes(), mi.InfoBytes, applied(t, mi.InfoBytes)); !errors.Is(err, ErrInfoMismatch) {
		t.Errorf("another torrent's info: %v, want ErrInfoMismatch", err)
	}
}

// The engine keeps the first info it applied, while a later SetInfoBytes on the same torrent (another
// add path) replaces only the bytes (finding 31). Bytes that hash right are refused unless they are
// the info the engine applied, so the folder is never opened with a stranger's file list.
func TestVerifiedBytesMustBeTheInfoTheEngineApplied(t *testing.T) {
	_, mi := named(t, "v1-tree")
	_, other := named(t, "v1-single")
	if _, err := verify(mi.HashInfoBytes(), mi.InfoBytes, applied(t, other.InfoBytes)); !errors.Is(err, ErrInfoReplaced) {
		t.Errorf("bytes that are not the applied info: %v, want ErrInfoReplaced", err)
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
	v2 := *full.ToShort()
	if _, err := verify(v2, mi.InfoBytes, applied(t, mi.InfoBytes)); !errors.Is(err, ErrInfoMismatch) {
		t.Errorf("truncated v2 hash: %v, want ErrInfoMismatch", err)
	}
}

func TestMetadataThatCannotBeReadOrIsV2OnlyIsRefused(t *testing.T) {
	garbage := []byte("not bencode")
	if _, err := verify(metainfo.HashBytes(garbage), garbage, &metainfo.Info{}); !errors.Is(err, ErrBadInfo) {
		t.Errorf("unreadable info: %v, want ErrBadInfo", err)
	}
	v2only, err := bencode.Marshal(metainfo.Info{Name: "v2", PieceLength: 16 << 10, MetaVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verify(metainfo.HashBytes(v2only), v2only, applied(t, v2only)); !errors.Is(err, ErrV2Only) {
		t.Errorf("v2-only info: %v, want ErrV2Only", err)
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
		{"canceled", done, Request{InfoHash: mi.HashInfoBytes(), Wait: time.Second}, context.Canceled},
	} {
		if p, err := Fetch(c.ctx, nil, c.req); p != nil || !errors.Is(err, c.want) {
			t.Errorf("%s: Fetch = %v, %v; want %v", c.name, p, err, c.want)
		}
	}
}
