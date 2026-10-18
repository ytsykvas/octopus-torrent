package fixture

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"github.com/anacrolix/torrent/merkle"
)

// entry is one payload file of a spec: a "/"-separated path inside the torrent and its length.
type entry struct {
	path   string
	length int64
}

// spec describes one fixture. An invalid fixture changes the encoded dictionaries (mutate) or cuts
// the encoded file (cut) and keeps no payload.
type spec struct {
	name, purpose string
	set           string // payload set: the same set gives the same bytes in every format
	format        Format
	pieceLength   int64
	root          string // the torrent's folder; empty for a single-file torrent
	files         []entry
	engine        Stage
	mutate        func(info map[string]any, layers map[string]string)
	cut           int
}

// segment is one entry of the v1 byte stream: a payload file (an index into Files) or a pad file (-1).
type segment struct {
	file   int
	length int64
}

// dicts returns the fixture with its files and the info dictionary and piece layers before they are
// changed or encoded.
func (s spec) dicts() (Fixture, map[string]any, map[string]string) {
	fx := Fixture{Name: s.name, Purpose: s.purpose, Format: s.format, PieceLength: s.pieceLength, Engine: s.engine, set: s.set, root: s.root}
	for _, e := range s.files {
		path := strings.Split(e.path, "/")
		sum := sha256.Sum256(content(s.set, path, e.length))
		fx.Files = append(fx.Files, File{Path: path, Length: e.length, SHA256: hex.EncodeToString(sum[:])})
	}
	// BEP 52 file tree order: path components compared bytewise. v1 uses it too, so a hybrid's two
	// descriptions cover the same byte stream.
	slices.SortFunc(fx.Files, func(a, b File) int { return slices.Compare(a.Path, b.Path) })
	name := s.root
	if name == "" {
		name = fx.Files[0].Path[0]
		if s.format != V1 {
			// The pinned engine treats a BEP 52 file tree as a folder even with one file, so it keeps
			// a single-file v2 or hybrid torrent in a folder of the torrent's name.
			fx.root = name
		}
	}
	info := map[string]any{"name": name, "piece length": s.pieceLength}
	layers := map[string]string{}
	if s.format != V2 {
		fx.addV1(info, s.root == "")
	}
	if s.format != V1 {
		fx.addV2(info, layers)
	}
	return fx, info, layers
}

func (s spec) build() Fixture {
	fx, info, layers := s.dicts()
	if s.mutate != nil {
		s.mutate(info, layers)
		fx.Files = nil
	}
	infoBytes := encode(info)
	top := map[string]any{"info": raw(infoBytes)}
	if len(layers) > 0 {
		top["piece layers"] = layers
	}
	fx.Metainfo = encode(top)
	if s.cut > 0 {
		fx.Metainfo, fx.Files = fx.Metainfo[:s.cut], nil
	}
	if s.format != V2 {
		v1 := sha1.Sum(infoBytes)
		fx.InfohashV1 = hex.EncodeToString(v1[:])
	}
	if s.format != V1 {
		v2 := sha256.Sum256(infoBytes)
		fx.InfohashV2 = hex.EncodeToString(v2[:])
	}
	return fx
}

// addV1 adds the v1 description: the file list (or, for a single file, its length) and the SHA-1 piece hashes
// of the byte stream. A hybrid pads every non-empty file except the last to a piece boundary with
// a BEP 47 pad file, so v1 and v2 pieces cover the same bytes.
func (f *Fixture) addV1(info map[string]any, single bool) {
	var stream []byte
	var list []any
	for i, file := range f.Files {
		stream = append(stream, f.Content(file)...)
		list = append(list, map[string]any{"length": file.Length, "path": file.Path})
		f.segments = append(f.segments, segment{file: i, length: file.Length})
		if pad := (f.PieceLength - file.Length%f.PieceLength) % f.PieceLength; f.Format == Hybrid && i < len(f.Files)-1 && pad > 0 {
			stream = append(stream, make([]byte, pad)...)
			list = append(list, map[string]any{"attr": "p", "length": pad, "path": []any{".pad", strconv.FormatInt(pad, 10)}})
			f.segments = append(f.segments, segment{file: -1, length: pad})
		}
	}
	var pieces []byte
	for off := 0; off < len(stream); off += int(f.PieceLength) {
		sum := sha1.Sum(stream[off:min(off+int(f.PieceLength), len(stream))])
		pieces = append(pieces, sum[:]...)
	}
	f.Pieces = len(pieces) / sha1.Size
	info["pieces"] = string(pieces)
	if single {
		info["length"] = f.Files[0].Length
	} else {
		info["files"] = list
	}
}

// addV2 adds the BEP 52 description: meta version 2, the file tree with each non-empty file's
// merkle root of 16 KiB blocks, and the piece layer of every file longer than one piece.
func (f *Fixture) addV2(info map[string]any, layers map[string]string) {
	tree := map[string]any{}
	v2Pieces := 0
	for _, file := range f.Files {
		props := map[string]any{"length": file.Length}
		if file.Length > 0 {
			data := f.Content(file)
			root := merkle.NewHash()
			root.Write(data)
			props["pieces root"] = string(root.Sum(nil))
			var layer []byte
			for off := int64(0); off < file.Length; off += f.PieceLength {
				piece := merkle.NewHash()
				piece.Write(data[off:min(off+f.PieceLength, file.Length)])
				layer = piece.SumMinLength(layer, int(f.PieceLength))
				v2Pieces++
			}
			if file.Length > f.PieceLength {
				layers[props["pieces root"].(string)] = string(layer)
			}
		}
		node := tree
		for _, dir := range file.Path[:len(file.Path)-1] {
			child, ok := node[dir].(map[string]any)
			if !ok {
				child = map[string]any{}
				node[dir] = child
			}
			node = child
		}
		node[file.Path[len(file.Path)-1]] = map[string]any{"": props}
	}
	info["meta version"] = int64(2)
	info["file tree"] = tree
	if f.Format == V2 {
		f.Pieces = v2Pieces
	}
}
