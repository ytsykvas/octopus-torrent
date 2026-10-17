package torrentengine_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent/storage"
)

// Engine facts (task 009, finding 19): the engine's file storage constructors that open their own
// completion database fall back to an in-memory completion, with only a log line, when they cannot
// open it. A restart then forgets every piece. The product never uses them: it gives the engine a
// completion that fails loudly (internal/torrentengine/filestore).

// survivesARestart marks a piece complete through one storage and asks a second storage, made the
// same way over the same folder, what it knows about the piece.
func survivesARestart(t *testing.T, dir string, open func(dir string) storage.ClientImplCloser) bool {
	t.Helper()
	fx := named(t, "v1-single")
	info, err := metaInfo(t, fx).UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	ih := metaInfo(t, fx).HashInfoBytes()
	ctx := context.Background()

	first := open(dir)
	impl, err := first.OpenTorrent(ctx, &info, ih)
	if err != nil {
		t.Fatal(err)
	}
	_ = impl.Piece(info.Piece(0)).MarkComplete()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := open(dir)
	defer second.Close()
	impl, err = second.OpenTorrent(ctx, &info, ih)
	if err != nil {
		t.Fatal(err)
	}
	return impl.Piece(info.Piece(0)).Completion().Ok
}

func TestTheEnginesOwnConstructorsForgetEveryPieceWhenTheirDatabaseCannotBeOpened(t *testing.T) {
	constructors := map[string]func(dir string) storage.ClientImplCloser{
		"NewFile": func(dir string) storage.ClientImplCloser { return storage.NewFile(dir) },
		"NewMMap": func(dir string) storage.ClientImplCloser { return storage.NewMMap(dir) },
		"NewFileOpts, part files off, no completion": func(dir string) storage.ClientImplCloser {
			return storage.NewFileOpts(storage.NewFileClientOpts{ClientBaseDir: dir, UsePartFiles: g.Some(false)})
		},
	}
	for name, open := range constructors {
		t.Run(name, func(t *testing.T) {
			healthy := t.TempDir()
			if !survivesARestart(t, healthy, open) {
				t.Fatal("with a writable folder the piece was forgotten: the contrast is gone")
			}
			if _, err := os.Stat(filepath.Join(healthy, ".torrent.db")); err != nil {
				t.Errorf("the engine's database is not in the user's folder: %v", err)
			}

			// A folder at the database's path: the folder is writable, the database cannot be opened.
			broken := t.TempDir()
			if err := os.Mkdir(filepath.Join(broken, ".torrent.db"), 0o755); err != nil {
				t.Fatal(err)
			}
			if survivesARestart(t, broken, open) {
				t.Fatal("a piece survived a restart without a database: the silent fallback is gone")
			}
		})
	}
}
