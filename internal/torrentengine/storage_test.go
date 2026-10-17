package torrentengine_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anacrolix/torrent/storage"

	"octopustorrent/internal/torrentengine/testutil/fixture"
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
			opts := storage.NewFileClientOpts{ClientBaseDir: dir}
			opts.UsePartFiles.Set(false)
			return storage.NewFileOpts(opts)
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

// placement writes the payload of the fixture through the storage, piece by piece, and lists where the
// engine put the files, relative to dir.
func placement(t *testing.T, fx fixture.Fixture, open func(dir string) storage.ClientImplCloser) []string {
	t.Helper()
	dir := t.TempDir()
	mi := metaInfo(t, fx)
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	st := open(dir)
	defer st.Close()
	impl, err := st.OpenTorrent(context.Background(), &info, mi.HashInfoBytes())
	if err != nil {
		t.Fatal(err)
	}
	var stream []byte
	for _, f := range fx.Files {
		stream = append(stream, fx.Content(f)...)
	}
	for i := range info.NumPieces() {
		p := info.Piece(i)
		end := min(p.Offset()+p.Length(), int64(len(stream)))
		if _, err := impl.Piece(p).WriteAt(stream[p.Offset():end], 0); err != nil {
			t.Fatal(err)
		}
	}
	var out []string
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func TestTheEnginesStorageConstructorsPlaceTheFilesDifferently(t *testing.T) {
	byFileOpts := func(dir string) storage.ClientImplCloser {
		opts := storage.NewFileClientOpts{ClientBaseDir: dir, PieceCompletion: storage.NewMapPieceCompletion()}
		opts.UsePartFiles.Set(false)
		return storage.NewFileOpts(opts)
	}
	byMMap := func(dir string) storage.ClientImplCloser {
		return storage.NewMMapWithCompletion(dir, storage.NewMapPieceCompletion())
	}
	byInfoHash := func(dir string) storage.ClientImplCloser { return storage.NewFileByInfoHash(dir) }

	// where NewFileOpts (part files off) and the mmap storage put the payload: below the chosen
	// folder, as <name> for a v1 single file, <name>/<name> for a BEP 52 single file, and
	// <name>/<path…> for a tree
	want := map[string][]string{
		"v1-single":        {"single.bin"},
		"hybrid-single":    {"single.bin/single.bin"},
		"v1-tree-no-empty": {"tree/docs/readme.txt", "tree/media/clips/clip.bin", "tree/media/readme.txt", "tree/Ünïcode/файл.bin"},
	}
	for name, files := range want {
		fx := named(t, name)
		for cname, open := range map[string]func(string) storage.ClientImplCloser{"NewFileOpts": byFileOpts, "NewMMapWithCompletion": byMMap} {
			if got := placement(t, fx, open); !slices.Equal(got, files) {
				t.Errorf("%s with %s: files at %v, want %v", name, cname, got, files)
			}
		}
		// NewFileByInfoHash adds a folder named after the infohash, leaves every file as <name>.part
		// (part files are on), and puts its database .torrent.db beside it, in the user's folder
		got := placement(t, fx, byInfoHash)
		for _, f := range files {
			found := false
			for _, g := range got {
				if len(g) > 41 && g[40] == '/' && g[41:] == f+".part" {
					found = true
				}
			}
			if !found {
				t.Errorf("%s with NewFileByInfoHash: %v has no <infohash>/%s.part", name, got, f)
			}
		}
		if !slices.Contains(got, ".torrent.db") {
			t.Errorf("%s with NewFileByInfoHash: %v has no .torrent.db in the base folder", name, got)
		}
	}
}
