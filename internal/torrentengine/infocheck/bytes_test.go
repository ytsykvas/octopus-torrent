package infocheck

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"

	"octopustorrent/internal/torrentengine/testutil/fixture"
)

// infoBytes is a fixture's info dictionary as the engine receives it.
func infoBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := bencode.Marshal(raw(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// nested is depth levels of open, the innermost value, and depth levels of close.
func nested(open, inner, close string, depth int) string {
	return strings.Repeat(open, depth) + inner + strings.Repeat(close, depth)
}

// withKey is a fixture's info bytes with one more raw key, which must sort after every other key.
func withKey(t *testing.T, name, key, raw string) []byte {
	t.Helper()
	b := infoBytes(t, name)
	return append(append(b[:len(b)-1:len(b)-1], bencode.MustMarshal(key)...), raw+"e"...)
}

func TestCheckBytesAdmitsEveryFixtureTheEngineHandles(t *testing.T) {
	for _, name := range []string{"v1-single", "v1-tree", "v1-tree-no-empty", "v1-large", "v1-long", "hybrid-single", "hybrid-tree", "invalid-piece-layer", "invalid-path-traversal"} {
		info, err := CheckBytes(infoBytes(t, name), roomy)
		if err != nil || info.PieceLength == 0 {
			t.Errorf("%s: %v, %+v", name, err, info)
		}
	}
}

// Every shape that panics the pinned engine is refused from its bytes, also the two the engine meets
// before any storage hook (findings 36, 37), and none panics the check.
func TestCheckBytesRefusesEveryShapeThatPanicsTheEngine(t *testing.T) {
	for _, h := range fixture.HostileInfos() {
		want := map[int]error{33: ErrV2Only, 37: ErrMalformed}[h.Finding]
		if want == nil {
			want = ErrInconsistentInfo
		}
		if _, err := CheckBytes(h.InfoBytes, roomy); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", h.Name, err, want)
		}
	}
}

// A file tree nested deep costs the engine its size times its depth under the client lock (finding
// 38): 100 KB of it took the engine 18–30 s. The scan refuses it from the bytes, without a parse.
func TestCheckBytesRefusesADeepFileTreeAtOnce(t *testing.T) {
	b := withKey(t, "v1-single", "zz", "0:") // keeps the keys sorted after "file tree" is added
	b = bytes.Replace(b, []byte("6:lengthi"), []byte("9:file tree"+nested("d1:a", "de", "e", 20_000)+"6:lengthi"), 1)
	start := time.Now()
	if _, err := CheckBytes(b, roomy); !errors.Is(err, ErrMalformed) {
		t.Errorf("%v, want ErrMalformed", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("the refusal took %v", d)
	}
}

func TestCheckBytesRefusesBytesTheEngineMustNotParse(t *testing.T) {
	small := Limits{Files: 1000, Pieces: 1000, MetadataBytes: 64}
	for _, c := range []struct {
		name string
		b    []byte
		l    Limits
		want error
		says string
	}{
		{"over the size limit", infoBytes(t, "v1-single"), small, ErrInfoTooLarge, "bytes"},
		{"nested one level too deep", withKey(t, "v1-single", "zz", nested("l", "i1e", "e", maxDepth)), roomy, ErrMalformed, "deeper than 64"},
		{"an unclosed list", []byte("d1:al"), roomy, ErrMalformed, "end inside"},
		{"bytes after the value", []byte("dei1e"), roomy, ErrMalformed, "after the value"},
		{"nothing at all", nil, roomy, ErrMalformed, "not a dictionary"},
		{"a key that is not a string", []byte("di1ei1ee"), roomy, ErrMalformed, "key"},
		{"an integer without digits", []byte("d1:aiee"), roomy, ErrMalformed, "integer"},
		{"an integer with a letter", []byte("d1:ai1xee"), roomy, ErrMalformed, "integer"},
		{"an unterminated integer", []byte("d1:ai12"), roomy, ErrMalformed, "integer"},
		{"a string longer than the bytes", []byte("d1:a3:xe"), roomy, ErrMalformed, "string"},
		{"a string length without a colon", []byte("d1:a1xe"), roomy, ErrMalformed, "string"},
		{"a string length that overflows", []byte("d1:a99999999999999999999:xe"), roomy, ErrMalformed, "string"},
		{"an unknown token", []byte("d1:axe"), roomy, ErrMalformed, "byte"},
		// The engine's decoder parses a long integer under an unknown key as a big integer, in
		// quadratic time under the client lock.
		{"an integer of 20 digits", withKey(t, "v1-single", "zz", "i"+strings.Repeat("9", 20)+"e"), roomy, ErrMalformed, "integer"},
		// The engine's decoder accepts a one-element list where it expects a dictionary, which would
		// hide the file tree from the work bound.
		{"the info inside a list", append(append([]byte("l"), infoBytes(t, "v1-single")...), 'e'), roomy, ErrMalformed, "dictionary"},
		{"a file tree inside a list", bytes.Replace(withKey(t, "v1-single", "zz", "0:"), []byte("6:lengthi"), []byte("9:file treeldee6:lengthi"), 1), roomy, ErrMalformed, "file tree"},
		{"a value the engine cannot read as an info", []byte("d4:name" + "i1ee"), roomy, ErrMalformed, "unmarshal"},
		{"an info Check refuses", infoBytes(t, "v2-single"), roomy, ErrV2Only, "v2-only"},
	} {
		info, err := CheckBytes(c.b, c.l)
		if info != nil || !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %v, %v; want %v saying %q", c.name, info, err, c.want, c.says)
		}
	}
	if _, err := CheckBytes(withKey(t, "v1-single", "zz", "i-"+strings.Repeat("9", 19)+"e"), roomy); err != nil {
		t.Errorf("an integer of 19 digits: %v", err)
	}
	at := withKey(t, "v1-single", "zz", nested("l", "i1e", "e", maxDepth-1))
	if _, err := CheckBytes(at, roomy); err != nil {
		t.Errorf("nested exactly as deep as allowed: %v", err)
	}
}

// The tree's work is the bytes the engine re-parses: every dict inside it, the tree included. An
// honest tree costs a few times its length; the bound is maxTreeWork times.
func TestTheFileTreeWorkIsBoundedAndHonestTreesStayWellUnder(t *testing.T) {
	for _, name := range []string{"hybrid-single", "hybrid-tree"} {
		b := infoBytes(t, name)
		_, length, work, err := scanValue(b)
		if err != nil || length == 0 || work > 4*length {
			t.Errorf("%s: work %d for a tree of %d bytes (%v)", name, work, length, err)
		}
	}
	// A chain of single-entry folders: each level re-parses the rest of the tree, so a chain of n
	// levels costs about n/2 times its length.
	chain := func(levels int) []byte {
		b := withKey(t, "v1-single", "zz", "0:")
		return bytes.Replace(b, []byte("6:lengthi"), []byte("9:file tree"+nested("d1:a", "de", "e", levels)+"6:lengthi"), 1)
	}
	if _, err := CheckBytes(chain(3*maxTreeWork), roomy); !errors.Is(err, ErrMalformed) || !strings.Contains(err.Error(), "file tree") {
		t.Errorf("a chain past the work bound: %v", err)
	}
	if _, err := CheckBytes(chain(maxTreeWork), roomy); !errors.Is(err, ErrInconsistentInfo) {
		t.Errorf("a chain within the work bound reaches Check, which refuses a tree in a v1 info: %v", err)
	}
}

// Every extended message a peer sends is bencode the engine decodes under its lock; CheckMessage
// applies the same bounds to its first value and allows the bytes after it (a metadata piece).
func TestCheckMessageBoundsWhatTheEngineDecodes(t *testing.T) {
	for _, ok := range []string{"d1:md11:ut_metadatai1eee", "d8:msg_typei1e5:piecei0eeDATA", "de"} {
		if err := CheckMessage([]byte(ok)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{
		nested("l", "i1e", "e", maxDepth+1),
		"d1:vi" + strings.Repeat("9", 20) + "ee",
		"d1:v134217727:e",
	} {
		if err := CheckMessage([]byte(bad)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%.30q...: %v, want ErrMalformed", bad, err)
		}
	}
	for bad, says := range map[string]string{"": "end inside", "e": "nothing open"} {
		if err := CheckMessage([]byte(bad)); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%q: %v, want %q", bad, err, says)
		}
	}
}
