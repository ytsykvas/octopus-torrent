package filestore

import (
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"os"
	"path/filepath"
	"testing"
)

func filepathStat(path string) (os.FileInfo, error) { return os.Stat(path) }

// writeFile returns the path of a regular file, for tests that need a path that is not a folder.
func writeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// plainStorage is the engine's own file storage over dir, with part files off and the given
// completion, with none of our guard.
func plainStorage(dir string, completion storage.PieceCompletion) storage.ClientImplCloser {
	opts := storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: completion}
	opts.UsePartFiles.Set(false)
	return storage.NewFileOpts(opts)
}

// metaKey is the key of a piece of the torrent in the marks.
func metaKey(mi *metainfo.MetaInfo, piece int) metainfo.PieceKey {
	return metainfo.PieceKey{InfoHash: mi.HashInfoBytes(), Index: piece}
}
