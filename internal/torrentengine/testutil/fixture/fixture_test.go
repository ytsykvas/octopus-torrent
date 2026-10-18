package fixture

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

var update = flag.Bool("update", false, "rewrite testdata/ from the generator")

func TestCatalogMatchesTheCommittedTestdata(t *testing.T) {
	if *update {
		if err := WriteCatalog("testdata"); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if err := WriteCatalog(dir); err != nil {
		t.Fatal(err)
	}
	generated, _ := os.ReadDir(dir)
	committed, _ := os.ReadDir("testdata")
	names := func(entries []os.DirEntry) (n []string) {
		for _, e := range entries {
			n = append(n, e.Name())
		}
		return n
	}
	if !slices.Equal(names(generated), names(committed)) {
		t.Fatalf("testdata has %q, the generator writes %q; run go test ./internal/torrentengine/testutil/fixture -args -update",
			names(committed), names(generated))
	}
	for _, name := range names(generated) {
		got, _ := os.ReadFile(filepath.Join(dir, name))
		want, _ := os.ReadFile(filepath.Join("testdata", name))
		if !bytes.Equal(got, want) {
			t.Errorf("testdata/%s differs from the generator; run go test ./internal/torrentengine/testutil/fixture -args -update", name)
		}
	}
}

func TestBuildingTwiceGivesTheSameBytes(t *testing.T) {
	first, second := buildCatalog(), buildCatalog()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("two builds of the catalog differ")
	}
	if !reflect.DeepEqual(first, All()) {
		t.Fatal("All() differs from a fresh build")
	}
	mutated := All()
	mutated[0].Metainfo[0], mutated[0].Files[0].Path[0] = 'x', "changed"
	if !reflect.DeepEqual(first, All()) {
		t.Fatal("changing a fixture returned by All() changed the catalog")
	}
}

func TestEngineNamesTheModuleThisBuildUses(t *testing.T) {
	info, _ := debug.ReadBuildInfo()
	for _, m := range info.Deps {
		if m.Path == "github.com/anacrolix/torrent" {
			if got := m.Path + " " + m.Version; got != EngineModule {
				t.Fatalf("EngineModule = %q, the build uses %q: re-record the engine results and update EngineModule", EngineModule, got)
			}
			return
		}
	}
	t.Fatal("the test binary does not use github.com/anacrolix/torrent")
}

func TestInfohashesAreTheHashesOfTheInfoDictionary(t *testing.T) {
	for _, fx := range valid(t) {
		mi := load(t, fx)
		v1, v2 := sha1.Sum(mi.InfoBytes), sha256.Sum256(mi.InfoBytes)
		wantV1, wantV2 := hex.EncodeToString(v1[:]), hex.EncodeToString(v2[:])
		if fx.Format == V2 {
			wantV1 = ""
		}
		if fx.Format == V1 {
			wantV2 = ""
		}
		if fx.InfohashV1 != wantV1 || fx.InfohashV2 != wantV2 {
			t.Errorf("%s: infohashes %q %q, want %q %q", fx.Name, fx.InfohashV1, fx.InfohashV2, wantV1, wantV2)
		}
	}
}

func TestFilesHaveTheirRecordedLengthAndChecksum(t *testing.T) {
	for _, fx := range valid(t) {
		for _, f := range fx.Files {
			content := fx.Content(f)
			sum := sha256.Sum256(content)
			if int64(len(content)) != f.Length || hex.EncodeToString(sum[:]) != f.SHA256 {
				t.Errorf("%s %q: %d bytes, sha256 %x; recorded %d, %s", fx.Name, f.Path, len(content), sum, f.Length, f.SHA256)
			}
		}
	}
}

func TestTheSamePayloadSetHasTheSameBytesInEveryFormat(t *testing.T) {
	for _, set := range [][]string{{"v1-single", "v2-single", "hybrid-single"}, {"v1-tree", "v2-tree", "hybrid-tree"}} {
		want := named(t, set[0]).Files
		for _, name := range set[1:] {
			if got := named(t, name).Files; !reflect.DeepEqual(got, want) {
				t.Errorf("%s files %v, want those of %s %v", name, got, set[0], want)
			}
		}
	}
}

func TestTheEngineParsesEveryFixtureAsRecorded(t *testing.T) {
	for _, fx := range All() {
		got, err := fx.ParseStage()
		want := fx.Engine
		if want == StageAdd || want == StageAccepted {
			want = ""
		}
		if got != want {
			t.Errorf("%s: rejected at %q (%v), recorded %q", fx.Name, got, err, fx.Engine)
		}
	}
}

func TestPieceCountsMatchTheEngine(t *testing.T) {
	for _, fx := range valid(t) {
		info, _ := load(t, fx).UnmarshalInfo()
		if fx.Format != V2 && info.NumPieces() != fx.Pieces {
			t.Errorf("%s: the engine counts %d pieces, recorded %d", fx.Name, info.NumPieces(), fx.Pieces)
		}
		if info.PieceLength != fx.PieceLength {
			t.Errorf("%s: piece length %d, recorded %d", fx.Name, info.PieceLength, fx.PieceLength)
		}
	}
}

func TestV1TreeHasAPieceCrossingAFileBoundary(t *testing.T) {
	info, _ := load(t, named(t, "v1-tree")).UnmarshalInfo()
	filesInPiece := map[int64]int{}
	var offset int64
	for _, f := range info.UpvertedFiles() {
		if f.Length > 0 {
			for p := offset / info.PieceLength; p <= (offset+f.Length-1)/info.PieceLength; p++ {
				filesInPiece[p]++
			}
		}
		offset += f.Length
	}
	for _, n := range filesInPiece {
		if n > 1 {
			return
		}
	}
	t.Error("no v1 piece spans two files")
}

func TestHybridFilesStartOnPieceBoundariesAfterPadFiles(t *testing.T) {
	for _, name := range []string{"hybrid-single", "hybrid-tree"} {
		info, _ := load(t, named(t, name)).UnmarshalInfo()
		var offset int64
		pads := 0
		for f := range info.UpvertedV1Files() {
			if f.Attr == "p" {
				pads++
			} else if f.Length > 0 && offset%info.PieceLength != 0 {
				t.Errorf("%s: %q starts at %d, not on a piece boundary", name, f.BestPath(), offset)
			}
			offset += f.Length
		}
		if name == "hybrid-tree" && pads == 0 {
			t.Errorf("%s has no pad files", name)
		}
	}
}

func TestTreesHoldTheCasesTheyAreFor(t *testing.T) {
	for _, name := range []string{"v1-tree", "v2-tree", "hybrid-tree"} {
		var paths []string
		for _, f := range named(t, name).Files {
			paths = append(paths, strings.Join(f.Path, "/"))
		}
		for _, want := range []string{"docs/readme.txt", "media/readme.txt", "empty.dat", "Ünïcode/файл.bin", "media/clips/clip.bin"} {
			if !slices.Contains(paths, want) {
				t.Errorf("%s lacks %q: %q", name, want, paths)
			}
		}
	}
	for _, name := range []string{"v2-tree", "hybrid-tree"} {
		info, _ := load(t, named(t, name)).UnmarshalInfo()
		empty := info.FileTree.Dir["empty.dat"]
		if empty.File.Length != 0 || empty.File.PiecesRoot != "" {
			t.Errorf("%s: empty.dat is %+v, want length 0 and no pieces root", name, empty.File)
		}
		if err := metainfo.ValidatePieceLayers(load(t, named(t, name)).PieceLayers, &info.FileTree, info.PieceLength); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// v1-long must be larger than what the kernel can hold in flight on one loopback connection (send
// buffers grow to 4 MiB here), so a residual after a stop is never mistaken for completion (010).
func TestTheLongFixtureLeavesRoomForAResidual(t *testing.T) {
	fx := named(t, "v1-long")
	info, err := load(t, fx).UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.Files) != 1 || fx.Files[0].Length != 16<<20 || info.PieceLength != 64<<10 || info.NumPieces() != 256 {
		t.Fatalf("v1-long: files %v, piece length %d, %d pieces; want one 16 MiB file in 256 pieces of 64 KiB", fx.Files, info.PieceLength, info.NumPieces())
	}
}

func TestNoFixtureNamesATrackerOrAWebSeed(t *testing.T) {
	for _, fx := range valid(t) {
		mi := load(t, fx)
		if mi.Announce != "" || len(mi.AnnounceList) > 0 || len(mi.UrlList) > 0 || len(mi.Nodes) > 0 || mi.CreationDate != 0 || mi.CreatedBy != "" {
			t.Errorf("%s names a tracker, web seed, node or creation data: %+v", fx.Name, mi)
		}
	}
}

func TestWritePayloadCreatesEveryFileUnderTheDirectory(t *testing.T) {
	for _, name := range []string{"v1-single", "hybrid-tree"} {
		fx, dir := named(t, name), t.TempDir()
		if err := fx.WritePayload(dir); err != nil {
			t.Fatal(err)
		}
		for _, f := range fx.Files {
			got, err := os.ReadFile(fx.FilePath(dir, f))
			if err != nil || !bytes.Equal(got, fx.Content(f)) {
				t.Errorf("%s %q: %v, content equal %v", name, f.Path, err, bytes.Equal(got, fx.Content(f)))
			}
		}
	}
	for name, want := range map[string]string{"v1-tree": "/d/tree/docs/readme.txt", "v1-single": "/d/single.bin", "hybrid-single": "/d/single.bin/single.bin"} {
		fx := named(t, name)
		file := fx.Files[0]
		if name == "v1-tree" {
			file = File{Path: []string{"docs", "readme.txt"}}
		}
		if got := fx.FilePath("/d", file); got != want {
			t.Errorf("%s: FilePath = %q, want %q", name, got, want)
		}
	}
}

func TestCorruptPieceFlipsOneByteInsideThatPiece(t *testing.T) {
	for _, c := range []struct {
		name  string
		piece int
	}{{"v1-tree", 1}, {"hybrid-tree", 2}, {"v1-single", 3}} {
		fx, dir := named(t, c.name), t.TempDir()
		if err := fx.WritePayload(dir); err != nil {
			t.Fatal(err)
		}
		if err := fx.CorruptPiece(dir, c.piece); err != nil {
			t.Fatal(err)
		}
		info, _ := load(t, fx).UnmarshalInfo()
		changed, k := 0, 0
		var offset int64
		for f := range info.UpvertedV1Files() {
			if f.Attr != "p" {
				file := fx.Files[k]
				k++
				got, _ := os.ReadFile(fx.FilePath(dir, file))
				for i, b := range fx.Content(file) {
					if got[i] != b {
						changed++
						if piece := (offset + int64(i)) / info.PieceLength; piece != int64(c.piece) {
							t.Errorf("%s: a byte changed in piece %d, want %d", c.name, piece, c.piece)
						}
					}
				}
			}
			offset += f.Length
		}
		if changed != 1 {
			t.Errorf("%s: %d bytes changed, want 1", c.name, changed)
		}
	}
}

func TestErrorsAreReturnedNotHidden(t *testing.T) {
	if _, err := Named("no-such"); err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Errorf("Named(no-such) = %v", err)
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := named(t, "v1-tree").WritePayload(blocker); err == nil {
		t.Error("WritePayload under a file succeeded")
	}
	occupied := t.TempDir()
	if err := os.Mkdir(filepath.Join(occupied, "single.bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := named(t, "v1-single").WritePayload(occupied); err == nil {
		t.Error("WritePayload over a directory succeeded")
	}
	if err := WriteCatalog(filepath.Join(blocker, "x")); err == nil {
		t.Error("WriteCatalog into a missing directory succeeded")
	}
	dir := t.TempDir()
	if err := named(t, "v1-single").CorruptPiece(dir, 0); err == nil {
		t.Error("CorruptPiece without the payload succeeded")
	}
	if err := named(t, "v1-single").CorruptPiece(dir, 4); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("CorruptPiece(4) = %v, want out of range", err)
	}
	if err := named(t, "v2-single").CorruptPiece(dir, 0); err == nil || !strings.Contains(err.Error(), "v2") {
		t.Errorf("CorruptPiece on v2 = %v, want a refusal", err)
	}
	if err := named(t, "v1-single").WritePayload("relative"); err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Errorf("WritePayload into a relative directory = %v", err)
	}
	written := t.TempDir()
	if err := named(t, "v1-single").WritePayload(written); err != nil {
		t.Fatal(err)
	}
	if err := named(t, "v1-single").WritePayload(written); err == nil {
		t.Error("WritePayload replaced existing files")
	}
	if err := named(t, "v1-single").CorruptPiece(written, 0); err != nil {
		t.Fatal(err)
	}
	if err := named(t, "v1-single").CorruptPiece(written, 0); err == nil || !strings.Contains(err.Error(), "not this fixture's payload") {
		t.Errorf("CorruptPiece of changed data = %v", err)
	}
	if err := named(t, "invalid-truncated").WritePayload(dir); err == nil {
		t.Error("WritePayload of a metainfo-only fixture succeeded")
	}
	if err := named(t, "invalid-piece-layer").CorruptPiece(dir, 0); err == nil || !strings.Contains(err.Error(), "metainfo-only") {
		t.Errorf("CorruptPiece of a metainfo-only fixture = %v", err)
	}
}

func valid(t *testing.T) (fixtures []Fixture) {
	t.Helper()
	for _, fx := range All() {
		if fx.Files != nil {
			fixtures = append(fixtures, fx)
		}
	}
	if len(fixtures) != 9 {
		t.Fatalf("%d fixtures with payload, want 9", len(fixtures))
	}
	return fixtures
}

func named(t *testing.T, name string) Fixture {
	t.Helper()
	fx, err := Named(name)
	if err != nil {
		t.Fatal(err)
	}
	return fx
}

func load(t *testing.T, fx Fixture) *metainfo.MetaInfo {
	t.Helper()
	mi, err := fx.MetaInfo()
	if err != nil {
		t.Fatalf("%s: %v", fx.Name, err)
	}
	return mi
}

func TestHostileInfosAreTheMeasuredShapes(t *testing.T) {
	hostile := HostileInfos()
	base := func(name string) metainfo.Info {
		fx, err := Named(name)
		if err != nil {
			t.Fatal(err)
		}
		mi, err := fx.MetaInfo()
		if err != nil {
			t.Fatal(err)
		}
		info, err := mi.UnmarshalInfo()
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	parsed := map[string]metainfo.Info{}
	for _, h := range hostile {
		if h.Finding < 33 || h.Finding > 37 || h.Panic == "" {
			t.Errorf("%s: finding %d, panic %q", h.Name, h.Finding, h.Panic)
		}
		if h.Finding == 37 {
			// The engine's decoder would overflow this test's stack; check the bytes instead.
			deep := "2:zz" + strings.Repeat("l", DeepNesting) + strings.Repeat("e", DeepNesting) + "e"
			if !bytes.HasSuffix(h.InfoBytes, []byte(deep)) || len(h.InfoBytes) > 16<<20 {
				t.Errorf("%s: %d bytes without the nested key at the end", h.Name, len(h.InfoBytes))
			}
			parsed[h.Name] = metainfo.Info{}
			continue
		}
		var info metainfo.Info
		if err := bencode.Unmarshal(h.InfoBytes, &info); err != nil {
			t.Fatalf("%s: the engine cannot read it: %v", h.Name, err)
		}
		parsed[h.Name] = info
	}
	if len(parsed) != 9 {
		t.Fatalf("%d hostile infos with distinct names, want 9", len(parsed))
	}
	if got := parsed["a hybrid without a piece length"]; got.PieceLength != 0 || got.MetaVersion != 2 {
		t.Errorf("a hybrid without a piece length: %d, meta version %d", got.PieceLength, got.MetaVersion)
	}
	if got, want := parsed["v2-only tree"], base("v2-tree"); !reflect.DeepEqual(got, want) {
		t.Error("the v2-only tree is not the v2-tree fixture's info")
	}
	if got := parsed["v2 with a v1 length and no pieces"]; got.Length != 57344 || len(got.Pieces) != 0 || got.MetaVersion != 2 {
		t.Errorf("v2 with a v1 length: length %d, %d piece bytes, meta version %d", got.Length, len(got.Pieces), got.MetaVersion)
	}
	tree := base("hybrid-tree")
	if root := parsed["a hybrid with a short pieces root"].FileTree.Dir["docs"].Dir["readme.txt"].File.PiecesRoot; len(root) != 5 {
		t.Errorf("the short pieces root has %d bytes", len(root))
	}
	if got := parsed["a hybrid whose root folder has a length"]; got.FileTree.File.Length != got.PieceLength || len(got.Pieces) != len(tree.Pieces)+20 {
		t.Errorf("the root folder's length is %d, %d piece bytes", got.FileTree.File.Length, len(got.Pieces))
	}
	if got := parsed["a hybrid one v1 piece short"]; len(got.Pieces) != len(tree.Pieces)-20 || got.PieceLength != tree.PieceLength {
		t.Errorf("a hybrid one piece short: %d piece bytes of %d", len(got.Pieces), len(tree.Pieces))
	}
	if got := parsed["a hybrid with half its piece length"]; got.PieceLength != tree.PieceLength/2 || !bytes.Equal(got.Pieces, tree.Pieces) {
		t.Errorf("a hybrid with half its piece length: %d", got.PieceLength)
	}
}

func TestAnUnknownSpecIsAProgrammingError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("specNamed of an unknown name did not panic")
		}
	}()
	specNamed("no such fixture")
}
