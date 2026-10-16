package fixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
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

// WritePayload writes every file of the fixture under dir, as a complete seeder holds them. dir must
// be absolute, and no file may exist yet: the payload never replaces anything.
func (f Fixture) WritePayload(dir string) error {
	if f.Files == nil {
		return fmt.Errorf("%s has no payload: it is a metainfo-only fixture", f.Name)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%s: the payload directory %q is not absolute", f.Name, dir)
	}
	for _, file := range f.Files {
		path := f.FilePath(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := writeNew(path, f.Content(file)); err != nil {
			return err
		}
	}
	return nil
}

// writeNew writes a file that must not exist yet.
func writeNew(path string, data []byte) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return errors.Join(err, out.Close())
}

// CorruptPiece flips one byte of the payload under dir: the first file byte of the given v1 piece.
// A client that verifies the data then lacks exactly that piece.
func (f Fixture) CorruptPiece(dir string, piece int) error {
	if f.Files == nil {
		return fmt.Errorf("%s has no payload: it is a metainfo-only fixture", f.Name)
	}
	if f.Format == V2 {
		return fmt.Errorf("%s: a v2-only torrent has no piece index across files", f.Name)
	}
	if piece < 0 || piece >= f.Pieces {
		return fmt.Errorf("%s: piece %d is out of range 0…%d", f.Name, piece, f.Pieces-1)
	}
	start := int64(piece) * f.PieceLength
	// The first payload segment that holds a byte at or after start; pads and empty files hold none.
	i, offset := 0, int64(0)
	for {
		seg := f.segments[i]
		if seg.file >= 0 && seg.length > 0 && offset+seg.length > start {
			break
		}
		offset += seg.length
		i++
	}
	path := f.FilePath(dir, f.Files[f.segments[i].file])
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, f.Content(f.Files[f.segments[i].file])) {
		return fmt.Errorf("%s is not this fixture's payload; it is left unchanged", path)
	}
	data[max(start-offset, 0)] ^= 0xff
	return os.WriteFile(path, data, 0o644)
}
