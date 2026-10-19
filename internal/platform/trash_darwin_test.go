//go:build darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"octopustorrent/internal/testutil/volume"
)

// The real NSFileManager Trasher moves a file on a mounted volume to that volume's own Trash, not
// the owner's ~/.Trash, so the test leaves nothing behind once the image is detached.
func TestSystemTrasherMovesAFileToTheVolumesTrash(t *testing.T) {
	vol := volume.New(t, volume.Options{Size: "8m", FS: "APFS", Name: "OctoTrash"})
	file := filepath.Join(vol.Mount, "payload.bin")
	if err := os.WriteFile(file, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	loc, err := NewSystemTrasher().TrashFile(file)
	if err != nil {
		t.Fatalf("TrashFile = %v", err)
	}
	mount, err := filepath.EvalSymlinks(vol.Mount) // /var is a symlink to /private/var on macOS
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc, mount) || !strings.Contains(loc, ".Trashes") {
		t.Errorf("new location %q, want it under the volume's .Trashes (%s)", loc, mount)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("the file is still at its old path: %v", err)
	}
	if _, err := os.Stat(loc); err != nil {
		t.Errorf("the file is not at its new location: %v", err)
	}
}

// A missing path yields the system error, not a crash, and moves nothing.
func TestSystemTrasherReturnsTheSystemErrorForAMissingFile(t *testing.T) {
	_, err := NewSystemTrasher().TrashFile(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Error("TrashFile of a missing path succeeded")
	}
}

// A path the native call cannot take is refused in Go, so NSString/NSURL never aborts the process.
func TestSystemTrasherRefusesABadPathInGo(t *testing.T) {
	for _, bad := range []string{"", "relative/x", "/a\x00b", "/\xff\xfe"} {
		if _, err := NewSystemTrasher().TrashFile(bad); !errors.Is(err, ErrBadPath) {
			t.Errorf("TrashFile(%q) = %v, want ErrBadPath", bad, err)
		}
	}
}

// Trash drives the real Trasher end to end on a volume, proving the orchestration and the native
// call together, not only each alone.
func TestTrashWithTheRealTrasherMovesAFileOnAVolume(t *testing.T) {
	vol := volume.New(t, volume.Options{Size: "8m", FS: "APFS", Name: "OctoTrashE2E"})
	root, err := filepath.EvalSymlinks(vol.Mount)
	if err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(root, "save") // a folder the torrent owns, not the volume root
	if err := os.Mkdir(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "keep.bin"), []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mount, "go.bin"), []byte("g"), 0o600); err != nil {
		t.Fatal(err)
	}
	results, err := Trash(mount, []string{"go.bin"}, NewSystemTrasher())
	if err != nil || len(results) != 1 || !results[0].Trashed {
		t.Fatalf("Trash = %v, %+v", err, results)
	}
	if _, err := os.Stat(filepath.Join(mount, "go.bin")); !os.IsNotExist(err) {
		t.Errorf("the trashed file is still at its path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mount, "keep.bin")); err != nil {
		t.Errorf("the neighbour was disturbed: %v", err)
	}
}
