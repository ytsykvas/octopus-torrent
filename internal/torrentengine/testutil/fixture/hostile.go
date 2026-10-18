package fixture

import "strings"

// Hostile is an info dictionary that a hostile peer can serve for a magnet whose btih is the SHA-1
// of these bytes, so it passes every hash check, and that panics the pinned engine when it is
// applied (findings 33–37). Tests use it to prove that such metadata is refused before the engine
// applies it; it is never a .torrent fixture, because adding it would panic the test's own client.
type Hostile struct {
	Name string
	// 33: v2-only info under a v1 infohash; 34: a file tree that needs more pieces than "pieces"
	// holds; 35: a file tree node the engine reads without checking it; 36: meta version 2 without
	// a piece length, which the engine divides by before any storage hook; 37: nesting deep enough
	// to overflow the stack of the engine's recursive decoder, also before any hook (the process
	// ends with a fatal error, not a panic).
	Finding   int
	Panic     string // a part of the pinned engine's panic message
	InfoBytes []byte
}

// DeepNesting is how many lists the finding-37 shape nests: 8 MB, under the engine's 16 MiB
// metadata cap.
const DeepNesting = 4_000_000

// HostileInfos returns every hostile info dictionary, built from the catalog's specs.
func HostileInfos() []Hostile {
	const assert, bounds = "piece request order has", "slice bounds out of range"
	shapes := []struct {
		name, base string
		finding    int
		panic      string
		mutate     func(info map[string]any, fx Fixture)
	}{
		{"v2-only single file", "v2-single", 33, assert, nil},
		{"v2-only tree", "v2-tree", 33, assert, nil},
		{"v2 with a v1 length and no pieces", "v2-single", 34, bounds, func(info map[string]any, fx Fixture) {
			info["length"] = fx.Files[0].Length
		}},
		{"a hybrid one v1 piece short", "hybrid-tree", 34, bounds, func(info map[string]any, _ Fixture) {
			info["pieces"] = info["pieces"].(string)[20:]
		}},
		{"a hybrid with half its piece length", "hybrid-tree", 34, bounds, func(info map[string]any, _ Fixture) {
			info["piece length"] = info["piece length"].(int64) / 2
		}},
		// The engine copies a pieces root into 32 bytes and panics on any other length.
		{"a hybrid with a short pieces root", "hybrid-tree", 35, "PiecesRootAsByteArray", func(info map[string]any, _ Fixture) {
			props := info["file tree"].(map[string]any)["docs"].(map[string]any)["readme.txt"].(map[string]any)[""].(map[string]any)
			props["pieces root"] = props["pieces root"].(string)[:5]
		}},
		// The engine counts the pieces of every file tree node, folders too, but lists only the
		// files: a folder with a length of one piece, a trailing pad and one more v1 piece make the
		// counts agree while a piece then lies in no file.
		{"a hybrid whose root folder has a length", "hybrid-tree", 35, "mustGetOnlyFile", func(info map[string]any, fx Fixture) {
			info["file tree"].(map[string]any)[""] = map[string]any{"length": fx.PieceLength}
			info["files"] = append(info["files"].([]any), map[string]any{"attr": "p", "length": fx.PieceLength, "path": []any{".pad", "tail"}})
			info["pieces"] = info["pieces"].(string) + string(make([]byte, 20))
		}},
		{"a hybrid without a piece length", "hybrid-tree", 36, "integer divide by zero", func(info map[string]any, _ Fixture) {
			delete(info, "piece length")
		}},
		{"an unknown key nested 4M levels", "v1-single", 37, "stack overflow", func(info map[string]any, _ Fixture) {
			info["zz"] = raw(strings.Repeat("l", DeepNesting) + strings.Repeat("e", DeepNesting))
		}},
	}
	var hostile []Hostile
	for _, s := range shapes {
		fx, info, _ := specNamed(s.base).dicts()
		if s.mutate != nil {
			s.mutate(info, fx)
		}
		hostile = append(hostile, Hostile{Name: s.name, Finding: s.finding, Panic: s.panic, InfoBytes: encode(info)})
	}
	return hostile
}

// specNamed is the spec of a catalog fixture; the names are this package's own.
func specNamed(name string) spec {
	for _, s := range specs() {
		if s.name == name {
			return s
		}
	}
	panic("fixture: no spec named " + name)
}
