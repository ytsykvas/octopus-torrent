package fixture

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// content returns the bytes of one payload file: 32-byte blocks SHA-256("<set>/<path>:<i>") for
// i = 0, 1, … (i as a big-endian uint64), cut to the length. The same set and path give the same
// bytes in every format, on every machine.
func content(set string, path []string, length int64) []byte {
	seed := []byte(set + "/" + strings.Join(path, "/") + ":")
	out := make([]byte, 0, length+sha256.Size)
	for i := uint64(0); int64(len(out)) < length; i++ {
		block := sha256.Sum256(binary.BigEndian.AppendUint64(seed, i))
		out = append(out, block[:]...)
	}
	return out[:length]
}

// Content returns the bytes of one of the fixture's files.
func (f Fixture) Content(file File) []byte {
	return content(f.set, file.Path, file.Length)
}

// FilePath is where a client with the given storage directory keeps the file.
func (f Fixture) FilePath(dir string, file File) string {
	return filepath.Join(append([]string{dir, f.root}, file.Path...)...)
}

// WritePayload writes every file of the fixture under dir, as a complete seeder holds them.
func (f Fixture) WritePayload(dir string) error {
	if f.Files == nil {
		return fmt.Errorf("%s has no payload: it is a metainfo-only fixture", f.Name)
	}
	for _, file := range f.Files {
		path := f.FilePath(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, f.Content(file), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CorruptPiece flips one byte of the payload under dir: the first file byte of the given v1 piece.
// A client that verifies the data then lacks exactly that piece.
func (f Fixture) CorruptPiece(dir string, piece int) error {
	if f.Format == V2 {
		return fmt.Errorf("%s: a v2-only torrent has no piece index across files", f.Name)
	}
	if piece < 0 || piece >= f.Pieces {
		return fmt.Errorf("%s: piece %d is out of range 0…%d", f.Name, piece, f.Pieces-1)
	}
	start := int64(piece) * f.PieceLength
	i, offset := 0, int64(0)
	for s := f.segments[i]; s.file < 0 || s.length == 0 || offset+s.length <= start; s = f.segments[i] {
		offset += s.length
		i++
	}
	path := f.FilePath(dir, f.Files[f.segments[i].file])
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data[max(start-offset, 0)] ^= 0xff
	return os.WriteFile(path, data, 0o644)
}
