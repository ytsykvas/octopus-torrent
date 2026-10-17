package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"octopustorrent/internal/testutil/volume"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAFingerprintChangesWithEverythingAnOutsideEditCanChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.bin")
	write(t, path, "0123456789")
	base, err := Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !base.Regular || base.Size != 10 || base.Inode == 0 || base.Dev == 0 || base.Mtime == 0 || base.Ctime == 0 {
		t.Fatalf("fingerprint of a new file = %+v", base)
	}
	if again, _ := Stat(path); again != base {
		t.Fatal("an unchanged file has another fingerprint")
	}

	// the same size, other bytes
	time.Sleep(20 * time.Millisecond)
	write(t, path, "abcdefghij")
	if rewritten, _ := Stat(path); rewritten.Size != base.Size || rewritten == base || rewritten.Mtime == base.Mtime {
		t.Fatalf("a same-size rewrite: %+v -> %+v", base, rewritten)
	}

	// the same size and the same mtime: only ctime tells
	rewritten, _ := Stat(path)
	if err := os.Chtimes(path, time.Unix(0, rewritten.Mtime-int64(time.Hour)), time.Unix(0, rewritten.Mtime-int64(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if restored, _ := Stat(path); restored.Ctime == rewritten.Ctime || restored.Mtime == rewritten.Mtime {
		t.Fatalf("restoring an mtime must show in ctime: %+v -> %+v", rewritten, restored)
	}

	// another file under the same name
	other := filepath.Join(dir, "b.bin")
	write(t, other, "abcdefghij")
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	if replaced, _ := Stat(path); replaced.Inode == rewritten.Inode {
		t.Fatalf("a replaced file kept its inode: %+v", replaced)
	}
}

func TestAFingerprintOfWhatIsNotARegularFileSaysSo(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"a folder": dir, "a symlink": link} {
		fp, err := Stat(path)
		if err != nil || fp.Regular {
			t.Errorf("%s = %+v, %v; want a fingerprint that is not Regular", name, fp, err)
		}
	}
}

func TestStatOfAMissingFileIsNotExist(t *testing.T) {
	if _, err := Stat(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat = %v, want fs.ErrNotExist", err)
	}
}

func TestTheFileSystemTypeIsTheVolumesOwn(t *testing.T) {
	if got, err := FileSystem(t.TempDir()); err != nil || got != "apfs" {
		t.Fatalf("FileSystem of the temporary folder = %q, %v; want apfs on this Mac", got, err)
	}
	for fs, want := range map[string]string{"HFS+": "hfs", "ExFAT": "exfat"} {
		// ExFAT labels are at most 11 characters
		v := volume.New(t, volume.Options{Size: "8m", FS: fs, Name: "Octo" + want})
		got, err := FileSystem(v.Mount)
		if err != nil || got != want {
			t.Errorf("FileSystem of a %s volume = %q, %v; want %q", fs, got, err, want)
		}
	}
	if _, err := FileSystem(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("FileSystem of a missing path succeeded")
	}
}

func TestSystemIsTheProbeOfTheRealFileSystem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	write(t, path, "x")
	var probe Probe = System{}
	if fp, err := probe.Stat(path); err != nil || fp.Size != 1 {
		t.Fatalf("Stat = %+v, %v", fp, err)
	}
	if name, err := probe.FileSystem(path); err != nil || name == "" {
		t.Fatalf("FileSystem = %q, %v", name, err)
	}
}

func TestAnIdentityTellsTwoFoldersApartByTheirCreationTimeToo(t *testing.T) {
	dir := t.TempDir()
	first, err := IdentityOf(dir)
	if err != nil || first.Dev == 0 || first.Inode == 0 || first.Birth == 0 || !first.Dir {
		t.Fatalf("IdentityOf = %+v, %v", first, err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if viaLink, err := IdentityOf(link); err != nil || viaLink != first {
		t.Fatalf("IdentityOf a symlink = %+v, %v; want the folder it points to: %+v", viaLink, err, first)
	}
	if file, _ := IdentityOf(writeFileIn(t, dir)); file.Dir {
		t.Fatal("a file has the identity of a folder")
	}
	other, _ := IdentityOf(t.TempDir())
	if other == first {
		t.Fatal("two folders have the same identity")
	}
	if _, err := IdentityOf(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("IdentityOf a missing path = %v", err)
	}
}

func writeFileIn(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "plain")
	write(t, path, "x")
	return path
}
