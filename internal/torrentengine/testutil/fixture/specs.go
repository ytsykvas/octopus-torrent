package fixture

import "slices"

const (
	singleLength = 57344    // 3.5 pieces of 16 KiB
	largeLength  = 1 << 20  // 64 pieces of 16 KiB
	longLength   = 16 << 20 // 256 pieces of 64 KiB
)

var (
	singleFiles = []entry{{"single.bin", singleLength}}
	// treeFiles: repeated names in different folders, a zero-length file, a Unicode name and, with
	// 32 KiB pieces, v1 pieces that span two files.
	treeFiles = []entry{
		{"docs/readme.txt", 10000},
		{"empty.dat", 0},
		{"media/clips/clip.bin", 40000},
		{"media/readme.txt", 20000},
		{"Ünïcode/файл.bin", 5000},
	}
)

func specs() []spec {
	v1Single := spec{name: "v1-single", purpose: "single v1 file of 3.5 pieces", set: "single", format: V1, pieceLength: 16 << 10, files: singleFiles, engine: StageAccepted}
	v1Tree := spec{name: "v1-tree", purpose: "v1 tree: nested folders, repeated names, an empty file, a Unicode name, pieces across files",
		set: "tree", format: V1, pieceLength: 32 << 10, root: "tree", files: treeFiles, engine: StageAccepted}
	hybridTree := spec{name: "hybrid-tree", purpose: "hybrid tree with BEP 47 pad files, an empty file and a piece layer",
		set: "tree", format: Hybrid, pieceLength: 32 << 10, root: "tree", files: treeFiles, engine: StageAccepted}
	return []spec{
		v1Single,
		v1Tree,
		{name: "v1-tree-no-empty", purpose: "v1-tree without the empty file: the control for the pinned engine's empty-file chunk failure",
			set: "tree", format: V1, pieceLength: 32 << 10, root: "tree", files: slices.DeleteFunc(slices.Clone(treeFiles), func(e entry) bool { return e.length == 0 }),
			engine: StageAccepted},
		{name: "v1-large", purpose: "1 MiB v1 file for rate-limited transfers, measurement and cancellation", set: "large", format: V1,
			pieceLength: 16 << 10, files: []entry{{"large.bin", largeLength}}, engine: StageAccepted},
		{name: "v1-long", purpose: "16 MiB v1 file for stop measurements: larger than the data a loopback connection holds in flight", set: "long", format: V1,
			pieceLength: 64 << 10, files: []entry{{"long.bin", longLength}}, engine: StageAccepted},
		{name: "v2-single", purpose: "v2-only single file; the pinned engine panics when it is added", set: "single", format: V2,
			pieceLength: 16 << 10, files: singleFiles, engine: StageAdd},
		{name: "v2-tree", purpose: "v2-only tree; the pinned engine panics when it is added", set: "tree", format: V2,
			pieceLength: 32 << 10, root: "tree", files: treeFiles, engine: StageAdd},
		{name: "hybrid-single", purpose: "hybrid single file", set: "single", format: Hybrid, pieceLength: 16 << 10, files: singleFiles, engine: StageAccepted},
		hybridTree,
		truncated(v1Single, "invalid-truncated", "v1-single cut in the middle of the info dictionary", StageLoad, 60),
		invalid(v1Single, "invalid-info-type", "piece length is a string, not an integer", StageInfo, func(info map[string]any, _ map[string]string) {
			info["piece length"] = "16384"
		}),
		invalid(v1Single, "invalid-pieces-length", "pieces is not a multiple of 20 bytes", StageAdd, func(info map[string]any, _ map[string]string) {
			pieces := info["pieces"].(string)
			info["pieces"] = pieces[:len(pieces)-1]
		}),
		invalid(v1Tree, "invalid-negative-length", "a file with length -1", StageAdd, func(info map[string]any, _ map[string]string) {
			info["files"].([]any)[0].(map[string]any)["length"] = int64(-1)
		}),
		invalid(v1Tree, "invalid-path-traversal", "a path component \"..\" that leaves the save folder; the engine accepts it", StageAccepted, func(info map[string]any, _ map[string]string) {
			info["files"].([]any)[0].(map[string]any)["path"] = []any{"..", "escape.bin"}
		}),
		invalid(v1Tree, "invalid-empty-traversal", "the empty file's path is [\"..\", \"victim.txt\"]: one level above the torrent folder; the engine accepts it and truncates an existing file there on add", StageAccepted, func(info map[string]any, _ map[string]string) {
			zeroLength(info)["path"] = []any{"..", "victim.txt"}
		}),
		invalid(v1Tree, "invalid-embedded-separator", "the empty file's path is one component \"a/../../x\", which resolves to the save folder", StageAccepted, func(info map[string]any, _ map[string]string) {
			zeroLength(info)["path"] = []any{"a/../../x"}
		}),
		invalid(v1Tree, "invalid-dot-components", "path components \".\" and \"\" in the empty file's path", StageAccepted, func(info map[string]any, _ map[string]string) {
			zeroLength(info)["path"] = []any{".", "", "dots.dat"}
		}),
		invalid(hybridTree, "invalid-hybrid-mismatch", "a hybrid whose v1 file list disagrees with its file tree; the engine accepts it", StageAccepted, func(info map[string]any, _ map[string]string) {
			info["files"].([]any)[0].(map[string]any)["length"] = int64(10001)
		}),
		invalid(hybridTree, "invalid-piece-layer", "a hybrid whose piece layer does not hash to its pieces root", StagePieceLayers, func(_ map[string]any, layers map[string]string) {
			for root, layer := range layers {
				layers[root] = "\xff" + layer[1:]
			}
		}),
	}
}

// zeroLength is the v1 file entry of the tree's empty file, empty.dat: second in path order,
// after docs/readme.txt.
func zeroLength(info map[string]any) map[string]any {
	return info["files"].([]any)[1].(map[string]any)
}

// invalid derives a metainfo-only fixture from base by changing its info dictionary or piece layers.
func invalid(base spec, name, purpose string, engine Stage, mutate func(map[string]any, map[string]string)) spec {
	base.name, base.purpose, base.engine, base.mutate = name, purpose, engine, mutate
	return base
}

// truncated derives a metainfo-only fixture from base whose metainfo file ends after at bytes.
func truncated(base spec, name, purpose string, engine Stage, at int) spec {
	base.name, base.purpose, base.engine, base.cut = name, purpose, engine, at
	return base
}
