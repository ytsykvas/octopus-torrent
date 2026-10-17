package library

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// imagePrefix names every disk image these tests attach, so a leftover one can be found.
const imagePrefix = "octopus-library-full-"

func TestMain(m *testing.M) {
	if left := attachedImages(); left != "" {
		fmt.Fprintf(os.Stderr, "a disk image of an earlier run is still attached; detach it with hdiutil detach -force <mountpoint>:\n%s", left)
		os.Exit(1)
	}
	code := m.Run()
	if left := attachedImages(); left != "" {
		fmt.Fprintf(os.Stderr, "a test left a disk image attached:\n%s", left)
		code = 1
	}
	os.Exit(code)
}

// attachedImages returns the hdiutil lines naming an image of these tests.
func attachedImages() string {
	out, err := exec.Command("hdiutil", "info").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("hdiutil info: %v\n%s", err, out)
	}
	var found strings.Builder
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, imagePrefix) {
			found.WriteString(line + "\n")
		}
	}
	return found.String()
}

// smallVolume attaches a new 8 MiB HFS+ disk image inside the test's folder and returns its mount
// point; the image is detached when the test ends.
func smallVolume(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	image := filepath.Join(dir, imagePrefix+"volume.dmg")
	mount := filepath.Join(dir, "mount")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("hdiutil", args...).CombinedOutput(); err != nil {
			t.Fatalf("hdiutil %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("create", "-size", "8m", "-fs", "HFS+", "-volname", "OctopusLibraryTest", "-layout", "NONE", image)
	if err := os.Mkdir(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	run("attach", "-nobrowse", "-noautoopen", "-mountpoint", mount, image)
	t.Cleanup(func() {
		if out, err := exec.Command("hdiutil", "detach", "-force", mount).CombinedOutput(); err != nil {
			t.Errorf("hdiutil detach: %v\n%s", err, out)
		}
	})
	return mount
}

// fill writes zeros into a file on the volume until the volume has no space left.
func fill(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "filler")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	chunk := make([]byte, 64<<10)
	for {
		if _, err := f.Write(chunk); err != nil {
			if !strings.Contains(err.Error(), "no space left") {
				t.Fatalf("filling the volume: %v", err)
			}
			return path
		}
	}
}

func TestAFullDiskFailsTheWriteAndKeepsWhatWasCommitted(t *testing.T) {
	mount := smallVolume(t)
	path := filepath.Join(mount, "library.sqlite")
	s := open(t, path)
	must(t, s, "CREATE TABLE rows(id INTEGER PRIMARY KEY, n INTEGER, data BLOB)")
	for i := range 20 {
		must(t, s, "INSERT INTO rows(n) VALUES (?)", i)
	}
	before := identify(t, path)
	filler := fill(t, mount)
	err := s.Write(deadline(t, 10*time.Second), func(tx Tx) error {
		_, err := tx.Exec("INSERT INTO rows(n, data) VALUES (-1, zeroblob(1048576))")
		return err
	})
	if !errors.Is(err, ErrFull) {
		t.Fatalf("Write on a full disk = %v, want ErrFull", err)
	}
	t.Logf("Write on a full disk: %v", err)
	// Close may or may not manage its checkpoint without space; the committed rows are in the WAL
	// either way, which the reopen proves.
	t.Logf("Close on a full disk: %v", s.Close())
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	again := open(t, path)
	if n := count(t, again); n != 20 {
		t.Fatalf("%d rows after the full disk, want 20", n)
	}
	integrity(t, again)
	if identify(t, path).inode != before.inode {
		t.Fatal("the database file was recreated")
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAPageLimitFailsTheWriteLikeAFullDisk(t *testing.T) {
	s, _ := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		if _, err := tx.Exec("PRAGMA max_page_count = 8"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO rows(n, data) VALUES (2, zeroblob(1048576))")
		return err
	})
	if !errors.Is(err, ErrFull) {
		t.Fatalf("Write past max_page_count = %v, want ErrFull", err)
	}
	if n := count(t, s); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}
