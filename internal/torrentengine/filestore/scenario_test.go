package filestore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// The scenarios run the real engine, through a closed loopback swarm, with our storage. Peers are
// connected only after the torrent is added, so what a restart reads from disk can be told from what
// arrives over the wire.

func TestEveryFormatLandsExactlyWhereLayoutSaysAndNothingElseIsWritten(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	before := listFiles(t, wd)
	for _, name := range []string{"v1-single", "v1-tree", "hybrid-single", "hybrid-tree"} {
		t.Run(name, func(t *testing.T) {
			fx, mi := named(t, name)
			seed := seeder(t, fx, mi, swarm.Options{})
			saveRoot, dataDir := t.TempDir(), t.TempDir()
			r := start(t, mi, saveRoot, dataDir, runOptions{})
			if r.addErr != nil {
				t.Fatal(r.addErr)
			}
			r.download(seed)
			info := r.torrent.Info()
			ownDir := r.peer.Dir
			if problems := r.quit(); len(problems) != 0 {
				t.Fatalf("session problems: %v", problems)
			}

			contentRoot, layout, err := Layout(saveRoot, info)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string][]byte{}
			for _, f := range fx.Files {
				want[fx.FilePath(saveRoot, f)] = fx.Content(f)
			}
			if len(layout) != len(want) {
				t.Fatalf("Layout lists %d files, the fixture has %d", len(layout), len(want))
			}
			for _, path := range layout {
				content, ok := want[path]
				if !ok {
					t.Errorf("Layout lists %s, which the fixture does not have", path)
					continue
				}
				if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, content) {
					t.Errorf("%s is %d bytes (%v), want the fixture's %d", path, len(got), err, len(content))
				}
			}
			var onDisk []string
			for _, rel := range listFiles(t, saveRoot) {
				onDisk = append(onDisk, filepath.Join(saveRoot, rel))
			}
			wantPaths := slices.Clone(layout)
			slices.Sort(wantPaths)
			if !slices.Equal(onDisk, wantPaths) {
				t.Errorf("below the save folder:\n%s\nwant exactly:\n%s", strings.Join(onDisk, "\n"), strings.Join(wantPaths, "\n"))
			}
			if _, err := os.Stat(contentRoot); err != nil {
				t.Errorf("the content root: %v", err)
			}
			if got := listFiles(t, dataDir); len(got) == 0 {
				t.Error("the data folder is empty, want the marks database")
			}
			for _, name := range listFiles(t, dataDir) {
				if !strings.HasPrefix(name, CompletionFile) {
					t.Errorf("the data folder holds %s", name)
				}
			}
			if got := listFiles(t, ownDir); len(got) != 0 {
				t.Errorf("the peer's own folder holds %v: something was stored by the default", got)
			}
		})
	}
	if after := listFiles(t, wd); !slices.Equal(before, after) {
		t.Errorf("the working directory changed from %v to %v", before, after)
	}
}

func TestATorrentAddedWithoutStorageOfItsOwnIsRefusedAndWritesNothing(t *testing.T) {
	_, mi := named(t, "v1-single")
	dataDir := t.TempDir()
	p := newPeer(t, swarm.Options{DefaultStorage: Refusing(), DataDir: dataDir})
	if _, err := p.Add(mi); err == nil {
		t.Fatal("a torrent without storage of its own was added")
	} else if !strings.Contains(err.Error(), ErrNoSaveFolder.Error()) {
		t.Fatalf("Add = %v, want %q", err, ErrNoSaveFolder)
	}
	if got := listFiles(t, dataDir); len(got) != 0 {
		t.Fatalf("the data folder holds %v", got)
	}
	if got := listFiles(t, p.Dir); len(got) != 0 {
		t.Fatalf("the peer folder holds %v", got)
	}
}

func TestTheGuardRefusesWhatWouldLeadThroughASymlinkOrTruncateAUsersFile(t *testing.T) {
	_, mi := named(t, "v1-tree") // its folder tree/ holds an empty file, empty.dat
	for name, prepare := range map[string]func(t *testing.T, saveRoot, outside string) (watch string){
		"a symlink at a file": func(t *testing.T, saveRoot, outside string) string {
			mustMkdir(t, filepath.Join(saveRoot, "tree", "docs"))
			target := filepath.Join(outside, "x")
			mustWrite(t, target, "user data")
			if err := os.Symlink(target, filepath.Join(saveRoot, "tree", "docs", "readme.txt")); err != nil {
				t.Fatal(err)
			}
			return target
		},
		"a symlink at the torrent's folder": func(t *testing.T, saveRoot, outside string) string {
			if err := os.Symlink(outside, filepath.Join(saveRoot, "tree")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(outside, "empty.dat")
		},
		"a user's file where the torrent has an empty one": func(t *testing.T, saveRoot, outside string) string {
			mustMkdir(t, filepath.Join(saveRoot, "tree"))
			path := filepath.Join(saveRoot, "tree", "empty.dat")
			mustWrite(t, path, "precious")
			return path
		},
	} {
		t.Run(name, func(t *testing.T) {
			saveRoot, outside := t.TempDir(), t.TempDir()
			watch := prepare(t, saveRoot, outside)
			before, _ := os.ReadFile(watch)
			r := start(t, mi, saveRoot, t.TempDir(), runOptions{})
			if !errors.Is(r.addErr, ErrUnsafePath) {
				t.Fatalf("adding the torrent = %v, want ErrUnsafePath", r.addErr)
			}
			if after, _ := os.ReadFile(watch); !bytes.Equal(after, before) {
				t.Fatalf("the user's data changed from %q to %q", before, after)
			}
			r.quit()
		})
	}
	// an empty regular file where the torrent has one is fine: nothing would be lost
	saveRoot := t.TempDir()
	mustMkdir(t, filepath.Join(saveRoot, "tree"))
	mustWrite(t, filepath.Join(saveRoot, "tree", "empty.dat"), "")
	r := start(t, mi, saveRoot, t.TempDir(), runOptions{})
	if r.addErr != nil {
		t.Fatalf("an empty file in place was refused: %v", r.addErr)
	}
	r.quit()
}
