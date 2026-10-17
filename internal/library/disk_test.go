package library

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// imagePrefix names every disk image these tests attach, followed by the test process's pid, so an
// image whose process is gone can be told apart from one another test run is using right now.
const imagePrefix = "octopus-library-full-"

// hdiutil is the system's own, never one found through PATH.
const hdiutil = "/usr/bin/hdiutil"

func TestMain(m *testing.M) {
	// The child processes of process_test.go run this binary too; only the parent checks.
	parent := os.Getenv(lockChildEnv) == "" && os.Getenv(commitChildEnv) == ""
	if left := leftImages(); parent && left != "" {
		fmt.Fprintf(os.Stderr, "a disk image of an ended run is still attached; detach it with hdiutil detach -force <mountpoint>:\n%s", left)
		os.Exit(1)
	}
	code := m.Run()
	if mine := attachedImages(func(pid int) bool { return pid == os.Getpid() }); parent && mine != "" {
		fmt.Fprintf(os.Stderr, "a test left a disk image attached:\n%s", mine)
		code = 1
	}
	os.Exit(code)
}

// leftImages returns the attached images whose test process no longer runs.
func leftImages() string {
	return attachedImages(func(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH })
}

// attachedImages returns the hdiutil lines naming an image of these tests whose pid matches.
func attachedImages(match func(pid int) bool) string {
	out, err := exec.Command(hdiutil, "info").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("hdiutil info: %v\n%s", err, out)
	}
	var found strings.Builder
	for _, line := range strings.Split(string(out), "\n") {
		_, rest, ok := strings.Cut(line, imagePrefix)
		if !ok {
			continue
		}
		digits, _, _ := strings.Cut(rest, ".")
		if pid, err := strconv.Atoi(digits); err != nil || match(pid) {
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
	image := filepath.Join(dir, fmt.Sprintf("%s%d.dmg", imagePrefix, os.Getpid()))
	mount := filepath.Join(dir, "mount")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(hdiutil, args...).CombinedOutput(); err != nil {
			t.Fatalf("hdiutil %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("create", "-size", "8m", "-fs", "HFS+", "-volname", "OctopusLibraryTest", "-layout", "NONE", image)
	if err := os.Mkdir(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	run("attach", "-nobrowse", "-noautoopen", "-mountpoint", mount, image)
	t.Cleanup(func() {
		if out, err := exec.Command(hdiutil, "detach", "-force", mount).CombinedOutput(); err != nil {
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
	s := openStore(t, path)
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
	// The closing checkpoint cannot copy the WAL into the database file without space; the committed
	// rows stay in the WAL, which the reopen proves.
	if err := s.Close(); !errors.Is(err, ErrFull) {
		t.Fatalf("Close on a full disk = %v, want ErrFull", err)
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, path)
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
