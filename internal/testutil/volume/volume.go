// Package volume attaches small disk images for tests that need a real, separate file system: a
// volume that fills up, one that is detached while in use, or one with a given format. Images are
// named after the test process, so a leftover of an ended run can be told from another run's image.
package volume

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// imagePrefix names every image these tests attach, followed by the test process's pid.
const imagePrefix = "octopus-volume-"

// hdiutil is the system's own, never one found through PATH.
const hdiutil = "/usr/bin/hdiutil"

// Test seams: the system tool, the output of Main and the write that Fill uses.
var (
	run = func(args []string) ([]byte, error) {
		return exec.Command(hdiutil, args...).CombinedOutput()
	}
	output     io.Writer = os.Stderr
	chunkWrite           = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
)

// Options describe a new volume.
type Options struct {
	Size string // hdiutil size, such as "8m"
	FS   string // hdiutil file system, such as "HFS+" or "ExFAT"
	Name string // the volume name
}

// Volume is an attached disk image.
type Volume struct {
	// Mount is the folder the image is mounted on. It is a folder of the test's temporary
	// directory, so after Detach it stays behind as an empty folder on the parent disk.
	Mount string

	tb       testing.TB
	image    string
	attached bool
}

// New creates an image inside the test's temporary folder and mounts it. The volume is detached
// when the test ends.
func New(tb testing.TB, o Options) *Volume {
	tb.Helper()
	dir := tb.TempDir()
	v := &Volume{
		tb:    tb,
		image: filepath.Join(dir, fmt.Sprintf("%s%d.dmg", imagePrefix, os.Getpid())),
		Mount: filepath.Join(dir, "mount"),
	}
	v.hdiutil("create", "-size", o.Size, "-fs", o.FS, "-volname", o.Name, "-layout", "NONE", v.image)
	if err := os.Mkdir(v.Mount, 0o700); err != nil {
		tb.Fatalf("create the mount point: %v", err)
	}
	v.Attach()
	tb.Cleanup(func() {
		if v.attached {
			v.detach(tb.Errorf)
		}
	})
	return v
}

// Attach mounts the image again, with its content, after Detach.
func (v *Volume) Attach() {
	v.tb.Helper()
	v.hdiutil("attach", "-nobrowse", "-noautoopen", "-mountpoint", v.Mount, v.image)
	v.attached = true
}

// Detach removes the volume at once, as when a drive is pulled: open files do not hold it.
func (v *Volume) Detach() {
	v.tb.Helper()
	v.detach(v.tb.Fatalf)
}

func (v *Volume) detach(fail func(string, ...any)) {
	v.tb.Helper()
	if out, err := run([]string{"detach", "-force", v.Mount}); err != nil {
		fail("hdiutil detach: %v\n%s", err, out)
		return
	}
	v.attached = false
}

func (v *Volume) hdiutil(args ...string) {
	v.tb.Helper()
	if out, err := run(args); err != nil {
		v.tb.Fatalf("hdiutil %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// Fill writes zeros into a file in dir until the file system has no space left, and returns the
// file's path. It writes in shrinking chunks, so not even a few blocks stay free.
func Fill(tb testing.TB, dir string) string {
	tb.Helper()
	path := filepath.Join(dir, "filler")
	f, err := os.Create(path)
	if err != nil {
		tb.Fatalf("create the filler: %v", err)
	}
	defer f.Close()
	for _, size := range []int{64 << 10, 4 << 10, 512, 1} {
		chunk := make([]byte, size)
		for {
			if _, err := chunkWrite(f, chunk); err != nil {
				if !strings.Contains(err.Error(), "no space left") {
					tb.Fatalf("filling the volume: %v", err)
				}
				break
			}
		}
	}
	return path
}

// Runner runs a package's tests; *testing.M is one.
type Runner interface{ Run() int }

// Main is the TestMain body of a package that attaches volumes:
//
//	os.Exit(volume.Main(m, os.Getenv(childMarker) == ""))
//
// Only the parent test process checks (parent), because child processes run the same binary. It
// refuses to start while an image of an ended run is still attached, and fails the package when a
// test left one of its own attached.
func Main(m Runner, parent bool) int {
	if parent {
		if left := attachedImages(func(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }); left != "" {
			fmt.Fprintf(output, "a disk image of an ended run is still attached; detach it with hdiutil detach -force <mountpoint>:\n%s", left)
			return 1
		}
	}
	code := m.Run()
	if parent {
		if mine := attachedImages(func(pid int) bool { return pid == os.Getpid() }); mine != "" {
			fmt.Fprintf(output, "a test left a disk image attached:\n%s", mine)
			code = 1
		}
	}
	return code
}

// attachedImages returns the `hdiutil info` lines naming an image of these tests whose pid matches,
// or the failure of hdiutil itself.
func attachedImages(match func(pid int) bool) string {
	out, err := run([]string{"info"})
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
