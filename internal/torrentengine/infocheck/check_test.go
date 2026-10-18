package infocheck

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/testutil/fixture"
)

var roomy = Limits{Files: 1000, Pieces: 1000, MetadataBytes: 16 << 20}

// raw is a fixture's info dictionary as a bencode map, so a test can build the shapes a hostile peer
// sends: the engine's own types would re-encode a file tree wrongly.
func raw(t *testing.T, name string) map[string]any {
	t.Helper()
	fx, err := fixture.Named(name)
	if err != nil {
		t.Fatal(err)
	}
	mi, err := fx.MetaInfo()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := bencode.Unmarshal(mi.InfoBytes, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// parse decodes a bencode map the way the engine decodes received info bytes.
func parse(t *testing.T, m map[string]any) *metainfo.Info {
	t.Helper()
	b, err := bencode.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var info metainfo.Info
	if err := bencode.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	return &info
}

func TestEveryFixtureTheEngineHandlesIsAdmitted(t *testing.T) {
	for _, name := range []string{"v1-single", "v1-tree", "v1-tree-no-empty", "v1-large", "v1-long", "hybrid-single", "hybrid-tree", "invalid-piece-layer", "invalid-path-traversal"} {
		if err := Check(parse(t, raw(t, name)), roomy); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Each shape here was measured on the pinned engine: it panics the engine when applied, or leaves
// the engine with two views of the torrent that disagree; or its v1 part is at odds with itself.
func TestInfoWhoseV1AndV2ViewsDisagreeIsRefused(t *testing.T) {
	type edit func(m map[string]any)
	pieces := func(n int) string { return string(make([]byte, 20*n)) }
	shapes := map[string]struct {
		base string
		edit edit
	}{
		"v2 with one piece":               {"v2-single", func(m map[string]any) { m["pieces"] = pieces(1) }},
		"v2 with a length and one piece":  {"v2-single", func(m map[string]any) { m["length"], m["pieces"] = int64(1), pieces(1) }},
		"v2 with too many v1 pieces":      {"v2-single", func(m map[string]any) { m["length"], m["pieces"] = int64(57344), pieces(9) }},
		"a hybrid with more v1 pieces":    {"hybrid-tree", func(m map[string]any) { m["pieces"] = m["pieces"].(string) + pieces(10) }},
		"a hybrid that drops a v1 file":   {"hybrid-tree", func(m map[string]any) { f := m["files"].([]any); m["files"] = f[:len(f)-1] }},
		"a hybrid with another file tree": {"hybrid-tree", func(m map[string]any) { m["file tree"] = raw(t, "v2-single")["file tree"] }},
		"a hybrid whose v1 part is one byte": {"hybrid-tree", func(m map[string]any) {
			delete(m, "files")
			m["length"], m["pieces"] = int64(1), pieces(1)
		}},
		"meta version 2 with no file tree": {"v1-tree", func(m map[string]any) { m["meta version"] = int64(2) }},
		"a v1 tree with a v2 file tree": {"v1-tree", func(m map[string]any) {
			m["meta version"], m["file tree"] = int64(2), raw(t, "v2-tree")["file tree"]
		}},
		// The same files and total, but the first pad is a byte short and the next a byte long: a
		// file then starts where the v2 view does not put it.
		"a hybrid whose pads are shifted": {"hybrid-tree", func(m map[string]any) {
			shift := int64(-1)
			for _, f := range m["files"].([]any) {
				file := f.(map[string]any)
				if attr, _ := file["attr"].(string); attr == "p" && shift <= 1 {
					file["length"] = file["length"].(int64) + shift
					shift += 2
				}
			}
			if shift != 3 {
				t.Fatal("hybrid-tree has fewer than two pad files")
			}
		}},
		// The v1 part hides the file tree's last file in a pad of the same size: a consistent v1
		// part with one file fewer.
		"a hybrid whose last file is a pad": {"hybrid-tree", func(m map[string]any) {
			files := m["files"].([]any)
			last := files[len(files)-1].(map[string]any)
			last["attr"], last["path"] = "p", []any{".pad", "hidden"}
		}},
		// A trailing pad and a piece for it: every file sits where the file tree puts it, but the v1
		// part has one piece more.
		"a hybrid with a trailing pad piece": {"hybrid-tree", func(m map[string]any) {
			m["files"] = append(m["files"].([]any), map[string]any{"attr": "p", "length": int64(32 << 10), "path": []any{".pad", "tail"}})
			m["pieces"] = m["pieces"].(string) + pieces(1)
		}},
		"meta version 3":              {"v1-single", func(m map[string]any) { m["meta version"] = int64(3) }},
		"both a length and files":     {"v1-tree", func(m map[string]any) { m["length"] = int64(1) }},
		"neither a length nor files":  {"v1-single", func(m map[string]any) { delete(m, "length") }},
		"no piece length":             {"v1-single", func(m map[string]any) { m["piece length"] = int64(0) }},
		"pieces not a multiple of 20": {"v1-single", func(m map[string]any) { m["pieces"] = m["pieces"].(string)[1:] }},
		"a piece count at odds":       {"v1-single", func(m map[string]any) { m["pieces"] = m["pieces"].(string) + pieces(1) }},
		"a negative file length":      {"v1-tree", func(m map[string]any) { m["files"].([]any)[0].(map[string]any)["length"] = int64(-1) }},
		"a negative single length":    {"v1-single", func(m map[string]any) { m["length"] = int64(-1) }},
		"a hybrid single file whose name differs from the file tree's": {"hybrid-single", func(m map[string]any) {
			m["name"] = "renamed"
		}},
	}
	for name, s := range shapes {
		m := raw(t, s.base)
		s.edit(m)
		if err := Check(parse(t, m), roomy); !errors.Is(err, ErrInconsistentInfo) {
			t.Errorf("%s: %v, want ErrInconsistentInfo", name, err)
		}
	}
	for _, name := range []string{"invalid-hybrid-mismatch", "invalid-pieces-length", "invalid-negative-length"} {
		if err := Check(parse(t, raw(t, name)), roomy); !errors.Is(err, ErrInconsistentInfo) {
			t.Errorf("fixture %s: %v, want ErrInconsistentInfo", name, err)
		}
	}
}

// A file tree node the engine reads without checking is refused by the walk itself, before any
// engine method touches the tree (finding 35): each shape names the node the error must describe.
func TestAFileTreeNodeTheEngineWouldTrustIsRefused(t *testing.T) {
	props := func(m map[string]any, path ...string) map[string]any {
		node := m["file tree"].(map[string]any)
		for _, p := range path {
			node = node[p].(map[string]any)
		}
		return node[""].(map[string]any)
	}
	readme := func(m map[string]any) map[string]any { return props(m, "docs", "readme.txt") }
	shapes := map[string]struct {
		edit func(m map[string]any)
		want string
	}{
		"a folder with file properties": {func(m map[string]any) {
			m["file tree"].(map[string]any)["docs"].(map[string]any)[""] = map[string]any{"length": int64(1)}
		}, "the folder at depth 1 has file properties"},
		"a negative file length":       {func(m map[string]any) { readme(m)["length"] = int64(-1) }, "file 0 of the file tree: -1 bytes"},
		"a file without a pieces root": {func(m map[string]any) { delete(readme(m), "pieces root") }, "file 0 of the file tree: 10000 bytes, a 0-byte pieces root"},
		"a long pieces root":           {func(m map[string]any) { readme(m)["pieces root"] = strings.Repeat("r", 33) }, "a 33-byte pieces root"},
		"an empty file with a pieces root": {func(m map[string]any) {
			props(m, "empty.dat")["pieces root"] = strings.Repeat("r", 32)
		}, "file 1 of the file tree: 0 bytes, a 32-byte pieces root"},
		"a file tree that is one file": {func(m map[string]any) {
			m["file tree"] = map[string]any{"": map[string]any{"length": int64(10000), "pieces root": strings.Repeat("r", 32)}}
		}, "the file tree has no entries"},
		"a file longer than the v1 file": {func(m map[string]any) {
			readme(m)["length"] = int64(math.MaxInt64)
		}, "real v1 file 0 (10000 bytes at 0) is not the file tree's file 0 (9223372036854775807 bytes at 0)"},
		// The walk stops at the first file past the v1 count, however many the tree holds ("Ü~"
		// sorts after the fixture's last file, "Ünïcode/файл.bin").
		"more files in the tree than in the v1 part": {func(m map[string]any) {
			tree := m["file tree"].(map[string]any)
			for i := range 50 {
				tree[fmt.Sprintf("Ü~%02d", i)] = map[string]any{"": map[string]any{"length": int64(1), "pieces root": strings.Repeat("r", 32)}}
			}
		}, "the file tree has more than the v1 part's 5 files"},
		"a file tree with fewer files": {func(m map[string]any) {
			delete(m["file tree"].(map[string]any), "Ünïcode")
		}, "the file tree has 4 files, the v1 part 5"},
	}
	for name, s := range shapes {
		m := raw(t, "hybrid-tree")
		s.edit(m)
		err := Check(parse(t, m), roomy)
		if !errors.Is(err, ErrInconsistentInfo) || !strings.Contains(err.Error(), s.want) {
			t.Errorf("%s: %v, want ErrInconsistentInfo saying %q", name, err, s.want)
		}
	}
}

// The engine indexes chunks of 16 KiB in a uint32 and keeps a piece's size in one (finding 39): a
// piece length over 2^30, or more chunks than the index holds, is refused, at and one past each bound.
func TestInfoPastTheEnginesIndexIsRefused(t *testing.T) {
	single := func(pieceLength, pieces int64) *metainfo.Info {
		m := raw(t, "v1-single")
		m["piece length"], m["length"], m["pieces"] = pieceLength, (pieces-1)*pieceLength+1, string(make([]byte, 20*pieces))
		return parse(t, m)
	}
	const maxPL, perPiece = int64(1) << 30, int64(1) << 16 // chunks in a piece of 2^30
	roomier := Limits{Files: 1, Pieces: 1 << 20}
	for _, c := range []struct {
		name       string
		pl, pieces int64
		admitted   bool
	}{
		{"the largest piece length", maxPL, 1, true},
		{"a piece length one byte longer", maxPL + 1, 1, false},
		{"a piece length of 2^32", 1 << 32, 1, false},
		{"the most chunks the index holds", maxPL, (1<<32 - 1) / perPiece, true},
		{"one piece more", maxPL, (1<<32-1)/perPiece + 1, false},
	} {
		err := Check(single(c.pl, c.pieces), roomier)
		if c.admitted && err != nil || !c.admitted && !errors.Is(err, ErrInfoTooLarge) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// hybrid is a consistent hybrid info of n one-piece files: the v1 file list and the file tree name
// the same files. The hashes are zeros; Check does not verify data.
func hybrid(n int) *metainfo.Info {
	const pl = 16 << 10
	files, tree := make([]any, n), map[string]any{}
	for i := range n {
		name := fmt.Sprintf("f%06d", i)
		files[i] = map[string]any{"length": int64(pl), "path": []any{name}}
		tree[name] = map[string]any{"": map[string]any{"length": int64(pl), "pieces root": strings.Repeat("r", 32)}}
	}
	b := bencode.MustMarshal(map[string]any{"name": "h", "piece length": int64(pl), "pieces": string(make([]byte, 20*n)), "files": files, "file tree": tree, "meta version": int64(2)})
	var info metainfo.Info
	if err := bencode.Unmarshal(b, &info); err != nil {
		panic(err)
	}
	return &info
}

// The engine walks the whole file tree of a "meta version 2" info for every piece and peer when it
// applies the info, under the client lock (finding 40): 50 000 files froze it for 43 s. A hybrid
// whose pieces times tree nodes exceed the bound is refused.
func TestAHybridTooCostlyForTheEngineIsRefused(t *testing.T) {
	big := Limits{Files: 1 << 20, Pieces: 1 << 20, MetadataBytes: 16 << 20}
	if err := Check(hybrid(500), big); err != nil {
		t.Errorf("500 files: %v", err)
	}
	if err := Check(hybrid(2000), big); !errors.Is(err, ErrInfoTooLarge) || !strings.Contains(err.Error(), "nodes") {
		t.Errorf("2000 files: %v, want ErrInfoTooLarge", err)
	}
}

// A file tree is the v2 part: a v1 info that carries one is refused, so the engine never keeps a
// tree nothing checked.
func TestAFileTreeOutsideMetaVersion2IsRefused(t *testing.T) {
	m := raw(t, "v1-single")
	m["file tree"] = raw(t, "v2-single")["file tree"]
	if err := Check(parse(t, m), roomy); !errors.Is(err, ErrInconsistentInfo) {
		t.Errorf("%v, want ErrInconsistentInfo", err)
	}
}

// A single-file hybrid built by hand from a v2 info: a v1 length and pieces that match the file
// tree make it the same torrent in both views.
func TestAConsistentHybridIsAdmitted(t *testing.T) {
	m := raw(t, "v2-single")
	m["length"], m["pieces"] = int64(57344), string(make([]byte, 20*4))
	if err := Check(parse(t, m), roomy); err != nil {
		t.Error(err)
	}
}

func TestTheLimitsAreTheCallers(t *testing.T) {
	tree := parse(t, raw(t, "v1-tree")) // 5 files, 3 pieces
	if err := Check(tree, Limits{Files: 5, Pieces: 3}); err != nil {
		t.Errorf("exactly at the limits: %v", err)
	}
	if err := Check(tree, Limits{Files: 4, Pieces: 3}); !errors.Is(err, ErrInfoTooLarge) {
		t.Errorf("one file over: %v", err)
	}
	if err := Check(tree, Limits{Files: 5, Pieces: 2}); !errors.Is(err, ErrInfoTooLarge) {
		t.Errorf("one piece over: %v", err)
	}
	hybrid := parse(t, raw(t, "hybrid-tree"))
	files := 0
	for range hybrid.UpvertedV1Files() {
		files++
	}
	if err := Check(hybrid, Limits{Files: files - 1, Pieces: 1000}); !errors.Is(err, ErrInfoTooLarge) {
		t.Errorf("a hybrid's pad files count as files: %v", err)
	}
}

func TestLimitsThatCannotWorkAreRefused(t *testing.T) {
	for _, l := range []Limits{{}, {Files: 1, Pieces: 1}, {Files: 1, MetadataBytes: 1}, {Pieces: 1, MetadataBytes: 1}, {Files: -1, Pieces: 1, MetadataBytes: 1}, {Files: 1, Pieces: 1, MetadataBytes: 16<<20 + 1}} {
		if err := l.Validate(); !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("%+v: %v", l, err)
		}
	}
	if err := roomy.Validate(); err != nil {
		t.Error(err)
	}
}

// Two files of 2^62 bytes overflow an int64 total; the sum is refused at the file that overflows it,
// not after it has wrapped around.
func TestATotalThatOverflowsIsRefusedWhereItOverflows(t *testing.T) {
	m := raw(t, "v1-tree")
	for _, f := range m["files"].([]any)[:2] {
		f.(map[string]any)["length"] = int64(1 << 62)
	}
	err := Check(parse(t, m), roomy)
	if !errors.Is(err, ErrInconsistentInfo) || !strings.Contains(err.Error(), "file 1 of length 4611686018427387904") {
		t.Errorf("Check = %v, want a refusal at file 1", err)
	}
}
