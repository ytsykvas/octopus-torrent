// Package infocheck decides whether an info dictionary is safe to hand to the pinned engine. The
// engine applies an info without checking that its parts agree, and some shapes panic it: a v2-only
// info under a v1 infohash (finding 33), a "meta version 2" info whose file tree needs more pieces
// than its v1 "pieces" field holds (34), and a file tree node the engine reads unchecked (35). Check
// accepts only an info with a complete v1 part and, when it also has a v2 part, one that describes
// exactly the same files and pieces (BEP 52's hybrid rule), within the caller's limits. It walks the
// file tree itself and calls no engine method on it. It runs on received magnet metadata before the
// engine applies it (filestore.Deferred) and, from 019, on every .torrent before it is added.
package infocheck

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
)

var (
	// ErrV2Only is an info with no v1 part; the pinned engine panics on those (007, finding 33).
	ErrV2Only = errors.New("v2-only metadata is not supported")
	// ErrInconsistentInfo is an info whose fields do not describe one torrent: a v1 part that is
	// incomplete or at odds with itself, or a v2 part that disagrees with it.
	ErrInconsistentInfo = errors.New("the metadata does not describe one consistent torrent")
	// ErrInfoTooLarge is an info with more files or pieces than the caller allows.
	ErrInfoTooLarge = errors.New("the metadata exceeds the limits")
	// ErrInvalidLimits is limits that would refuse every torrent, or allow more metadata than the
	// engine's 16 MiB cap.
	ErrInvalidLimits = errors.New("the metadata limits must be positive and within the engine's")
)

const (
	// maxPieceLength is the largest piece length admitted: the engine keeps a piece's size in a
	// uint32 (finding 39); real torrents stay far below.
	maxPieceLength = 1 << 30
	// chunkSize is the engine's request size; maxChunks is how many its uint32 request index holds.
	chunkSize = 16 << 10
	maxChunks = math.MaxUint32
	// maxMetadataBytes is the engine's own cap on an info dictionary (BEP 9 metadata).
	maxMetadataBytes = 16 << 20
	// maxTreeWalks bounds pieces times file tree nodes. The engine walks the whole tree of a
	// "meta version 2" info once per piece per connected peer when it applies the info, under the
	// client lock (finding 40). The bound allows for a full set of peers (the engine's default cap
	// is 50), so a swarm the attacker controls cannot stall the client; it refuses a hybrid of more
	// than about a thousand files, which the product's own limits (022) may lower further.
	maxTreeWalks = 1 << 20
)

// Limits bound what an admitted info may describe. The numbers are the caller's.
type Limits struct {
	Files         int // v1 files, BEP 47 pad files included
	Pieces        int
	MetadataBytes int // the info dictionary's bencoded length
}

// Validate refuses limits that are not positive.
func (l Limits) Validate() error {
	if l.Files <= 0 || l.Pieces <= 0 || l.MetadataBytes <= 0 || l.MetadataBytes > maxMetadataBytes {
		return fmt.Errorf("%w: %+v", ErrInvalidLimits, l)
	}
	return nil
}

// Check reports whether the engine may apply info.
func Check(info *metainfo.Info, l Limits) error {
	if info.MetaVersion == 2 && len(info.Pieces) == 0 && info.Length == 0 && len(info.Files) == 0 {
		return ErrV2Only
	}
	files, pieces, err := v1View(info)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInconsistentInfo, err)
	}
	if chunks := int64(pieces) * ((info.PieceLength + chunkSize - 1) / chunkSize); info.PieceLength > maxPieceLength || chunks > maxChunks {
		return fmt.Errorf("%w: pieces of %d bytes in %d chunks; the engine indexes pieces of at most %d bytes in at most %d chunks", ErrInfoTooLarge, info.PieceLength, chunks, maxPieceLength, int64(maxChunks))
	}
	if files > l.Files || pieces > l.Pieces {
		return fmt.Errorf("%w: %d files and %d pieces, the limits are %d and %d", ErrInfoTooLarge, files, pieces, l.Files, l.Pieces)
	}
	switch {
	case info.MetaVersion == 2:
		if err := sameAsV2(info, pieces); err != nil {
			return err
		}
	case info.FileTree.NumEntries() != 0:
		return fmt.Errorf("%w: a file tree in meta version %d", ErrInconsistentInfo, info.MetaVersion)
	}
	return nil
}

// v1View checks the v1 fields on their own and returns their file and piece counts.
func v1View(info *metainfo.Info) (files, pieces int, err error) {
	switch {
	case info.MetaVersion < 0 || info.MetaVersion > 2:
		return 0, 0, fmt.Errorf("meta version %d", info.MetaVersion)
	case info.PieceLength <= 0:
		return 0, 0, fmt.Errorf("piece length %d", info.PieceLength)
	case len(info.Pieces) == 0 || len(info.Pieces)%20 != 0:
		return 0, 0, fmt.Errorf("pieces of %d bytes", len(info.Pieces))
	case info.Length != 0 && len(info.Files) != 0:
		return 0, 0, errors.New("both a length and a file list")
	case info.Length < 0:
		return 0, 0, fmt.Errorf("length %d", info.Length)
	}
	total, files := info.Length, 1
	if len(info.Files) != 0 {
		total, files = 0, len(info.Files)
		for i, f := range info.Files {
			if f.Length < 0 || total+f.Length < total {
				return 0, 0, fmt.Errorf("file %d of length %d", i, f.Length)
			}
			total += f.Length
		}
	}
	if total == 0 {
		return 0, 0, errors.New("no data")
	}
	pieces = len(info.Pieces) / 20
	if (total-1)/info.PieceLength+1 != int64(pieces) {
		return 0, 0, fmt.Errorf("%d bytes in pieces of %d need %d pieces, not %d", total, info.PieceLength, (total-1)/info.PieceLength+1, pieces)
	}
	return files, pieces, nil
}

// file is one file of the torrent as the engine lays it out: its path, length and byte offset.
type file struct {
	path           []string
	length, offset int64
}

// sameAsV2 checks that the file tree, which the engine uses for the files and the piece count of a
// "meta version 2" info, describes the same files at the same offsets and the same pieces as the v1
// fields, whose piece hashes the engine uses.
func sameAsV2(info *metainfo.Info, pieces int) error {
	var v1 []file
	for f := range info.UpvertedV1Files() {
		if strings.Contains(f.Attr, "p") {
			continue
		}
		path := f.BestPath()
		if len(info.Files) == 0 {
			path = []string{info.BestName()}
		}
		v1 = append(v1, file{path: path, length: f.Length, offset: f.TorrentOffset})
	}
	v2Pieces, nodes, err := walkTree(info, v1)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInconsistentInfo, err)
	}
	if v2Pieces != pieces {
		return fmt.Errorf("%w: the file tree needs %d pieces, the v1 part has %d", ErrInconsistentInfo, v2Pieces, pieces)
	}
	if int64(pieces)*int64(nodes) > maxTreeWalks {
		return fmt.Errorf("%w: %d pieces over a file tree of %d nodes; the engine walks the tree for every piece", ErrInfoTooLarge, pieces, nodes)
	}
	return nil
}

// pathNode is a file tree node's path as a chain to its parent, so a deep tree costs one link per
// node instead of a copy of the path per node.
type pathNode struct {
	parent *pathNode
	key    string
	depth  int
}

// is reports whether the chain spells path.
func (p *pathNode) is(path []string) bool {
	if p.depth != len(path) {
		return false
	}
	for n := p; n.depth > 0; n = n.parent {
		if n.key != path[n.depth-1] {
			return false
		}
	}
	return true
}

// walkTree walks the file tree in the engine's order (keys sorted, depth first, each file at the
// next piece boundary), compares each file with the real v1 file of the same index and counts the
// pieces, without calling the engine's file tree methods: they trust every node (finding 35). A
// folder (a node with entries) may hold no file properties, which the engine would count as pieces
// of no file; a file needs a non-negative length and a 32-byte pieces root exactly when it is not
// empty, or the engine panics copying it. The walk ends at the first file that differs, so its cost
// is bounded by the v1 part, not by the tree.
func walkTree(info *metainfo.Info, v1 []file) (pieces, nodes int, err error) {
	if info.FileTree.NumEntries() == 0 {
		return 0, 0, errors.New("the file tree has no entries")
	}
	type node struct {
		tree *metainfo.FileTree
		path *pathNode
	}
	stack := []node{{tree: &info.FileTree, path: &pathNode{}}}
	var offset int64
	k := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nodes++
		if n.tree.NumEntries() > 0 {
			if n.tree.File != (metainfo.FileTreeFile{}) {
				return 0, 0, fmt.Errorf("the folder at depth %d has file properties", n.path.depth)
			}
			keys := slices.Sorted(maps.Keys(n.tree.Dir))
			for i := len(keys) - 1; i >= 0; i-- {
				if keys[i] != metainfo.FileTreePropertiesKey {
					child := n.tree.Dir[keys[i]]
					stack = append(stack, node{tree: &child, path: &pathNode{parent: n.path, key: keys[i], depth: n.path.depth + 1}})
				}
			}
			continue
		}
		f := n.tree.File
		if f.Length < 0 || (f.Length > 0) != (len(f.PiecesRoot) == 32) || f.Length == 0 && f.PiecesRoot != "" {
			return 0, 0, fmt.Errorf("file %d of the file tree: %d bytes, a %d-byte pieces root", k, f.Length, len(f.PiecesRoot))
		}
		if k == len(v1) {
			return 0, 0, fmt.Errorf("the file tree has more than the v1 part's %d files", len(v1))
		}
		if !n.path.is(v1[k].path) || f.Length != v1[k].length || offset != v1[k].offset {
			return 0, 0, fmt.Errorf("real v1 file %d (%d bytes at %d) is not the file tree's file %d (%d bytes at %d)", k, v1[k].length, v1[k].offset, k, f.Length, offset)
		}
		// The length is the v1 file's and the offset a v1 offset: the engine's chunk index bounds the
		// v1 total far below an overflow.
		count := (f.Length + info.PieceLength - 1) / info.PieceLength
		offset += count * info.PieceLength
		pieces += int(count)
		k++
	}
	if k != len(v1) {
		return 0, 0, fmt.Errorf("the file tree has %d files, the v1 part %d", k, len(v1))
	}
	return pieces, nodes, nil
}
