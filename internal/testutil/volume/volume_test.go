package volume

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// recorder replaces testing.TB so the failure paths of this package can be asserted: Fatalf stops
// the helper like the real one (by panicking with stop, which attempt recovers), and the messages
// are collected.
type recorder struct {
	testing.TB
	dir      string
	failures []string
	cleanups []func()
}

type stop struct{}

func (r *recorder) Helper()          {}
func (r *recorder) TempDir() string  { return r.dir }
func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }
func (r *recorder) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(stop{})
}

// attempt runs f and reports whether it ended through Fatalf.
func (r *recorder) attempt(f func()) (fatal bool) {
	defer func() {
		if v := recover(); v != nil {
			if _, ok := v.(stop); !ok {
				panic(v)
			}
			fatal = true
		}
	}()
	f()
	return false
}

func (r *recorder) finish() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
	r.cleanups = nil
}

func (r *recorder) said(want string) bool {
	for _, f := range r.failures {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

func newRecorder(t *testing.T) *recorder {
	return &recorder{TB: t, dir: t.TempDir()}
}

// fake replaces the hdiutil runner for the duration of the test.
func fake(t *testing.T, f func(args []string) ([]byte, error)) {
	t.Helper()
	old := run
	run = f
	t.Cleanup(func() { run = old })
}

func TestMain(m *testing.M) {
	os.Exit(Main(m, true))
}

func TestAVolumeIsAMountOfItsOwnThatFillsUpAndKeepsItsFilesAcrossADetach(t *testing.T) {
	v := New(t, Options{Size: "8m", FS: "HFS+", Name: "OctopusVolumeTest"})
	var onVolume, onParent syscall.Stat_t
	if err := syscall.Stat(v.Mount, &onVolume); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Stat(filepath.Dir(v.Mount), &onParent); err != nil {
		t.Fatal(err)
	}
	if onVolume.Dev == onParent.Dev {
		t.Fatal("the mount point is on the same device as its parent folder: nothing is mounted")
	}
	keep := filepath.Join(v.Mount, "keep.txt")
	if err := os.WriteFile(keep, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	filler := Fill(t, v.Mount)
	if err := os.WriteFile(filepath.Join(v.Mount, "more.txt"), bytes.Repeat([]byte("x"), 1<<20), 0o644); err == nil {
		t.Fatal("a write on the filled volume succeeded")
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}

	v.Detach()
	entries, err := os.ReadDir(v.Mount)
	if err != nil || len(entries) != 0 {
		t.Fatalf("after Detach the mount folder = %v, %v; want an empty folder on the parent disk", entries, err)
	}
	v.Attach()
	if got, err := os.ReadFile(keep); err != nil || string(got) != "kept" {
		t.Fatalf("after Attach the file = %q, %v", got, err)
	}
}

func TestEveryFailureOfTheSystemToolFailsTheTest(t *testing.T) {
	boom := errors.New("hdiutil is not available")
	fail := func(verb string) func([]string) ([]byte, error) {
		return func(args []string) ([]byte, error) {
			if args[0] == verb {
				return []byte("output of " + verb), boom
			}
			return nil, nil
		}
	}
	for _, verb := range []string{"create", "attach"} {
		fake(t, fail(verb))
		r := newRecorder(t)
		if !r.attempt(func() { New(r, Options{Size: "8m", FS: "HFS+", Name: "X"}) }) {
			t.Fatalf("New with a failing %s did not stop the test", verb)
		}
		if !r.said("hdiutil "+verb) || !r.said("output of "+verb) {
			t.Fatalf("%s failure = %v, want the command and its output", verb, r.failures)
		}
		r.finish()
	}

	// A failing mount point folder: the temporary folder is a plain file.
	fake(t, func([]string) ([]byte, error) { return nil, nil })
	r := &recorder{TB: t, dir: filepath.Join(t.TempDir(), "missing", "folder")}
	if !r.attempt(func() { New(r, Options{Size: "8m", FS: "HFS+", Name: "X"}) }) {
		t.Fatal("New with an unusable temporary folder did not stop the test")
	}

	// Detach and Attach report their failures; the cleanup of an attached volume reports its own.
	fake(t, func([]string) ([]byte, error) { return nil, nil })
	r = newRecorder(t)
	v := New(r, Options{Size: "8m", FS: "HFS+", Name: "X"})
	fake(t, fail("detach"))
	if !r.attempt(v.Detach) || !r.said("hdiutil detach") {
		t.Fatalf("a failing detach = %v", r.failures)
	}
	r.failures = nil
	r.finish()
	if !r.said("hdiutil detach") {
		t.Fatalf("a failing cleanup detach = %v", r.failures)
	}
	fake(t, func([]string) ([]byte, error) { return nil, nil })
	v.attached = false
	fake(t, fail("attach"))
	if !r.attempt(v.Attach) || !r.said("hdiutil attach") {
		t.Fatalf("a failing re-attach = %v", r.failures)
	}
}

func TestADetachedVolumeNeedsNoDetachAtTheEndOfTheTest(t *testing.T) {
	var calls []string
	fake(t, func(args []string) ([]byte, error) {
		calls = append(calls, args[0])
		return nil, nil
	})
	r := newRecorder(t)
	v := New(r, Options{Size: "8m", FS: "HFS+", Name: "X"})
	v.Detach()
	r.finish()
	if got := strings.Join(calls, " "); got != "create attach detach" {
		t.Fatalf("calls = %q, want exactly one detach", got)
	}
}

func TestFillFailsOnAFolderItCannotWriteTo(t *testing.T) {
	r := newRecorder(t)
	if !r.attempt(func() { Fill(r, filepath.Join(t.TempDir(), "missing")) }) {
		t.Fatal("Fill in a missing folder did not stop the test")
	}
}

func TestFillStopsOnAnyErrorButAFullVolume(t *testing.T) {
	// /dev/full-like: a file whose writes fail with a different error than "no space left".
	old := chunkWrite
	chunkWrite = func(f *os.File, b []byte) (int, error) { return 0, errors.New("input/output error") }
	t.Cleanup(func() { chunkWrite = old })
	r := newRecorder(t)
	if !r.attempt(func() { Fill(r, t.TempDir()) }) || !r.said("filling the volume") {
		t.Fatalf("Fill = %v", r.failures)
	}
}

func TestMainFailsOnAnImageOfAnEndedRunAndOnImagesTheRunLeft(t *testing.T) {
	var out bytes.Buffer
	oldOut := output
	output = &out
	t.Cleanup(func() { output = oldOut })
	runner := runnerFunc(func() int { return 0 })

	// An image whose process is gone (pid 1 << 30 does not exist).
	fake(t, func([]string) ([]byte, error) {
		return []byte("image-path : /x/" + imagePrefix + "1073741824.dmg\nimage-path : /x/other.dmg\n"), nil
	})
	if code := Main(runner, true); code != 1 || !strings.Contains(out.String(), "ended run") {
		t.Fatalf("Main with an old image = %d %q", code, out.String())
	}
	out.Reset()
	if code := Main(runner, false); code != 0 || out.Len() != 0 {
		t.Fatalf("a child process must not check: %d %q", code, out.String())
	}

	// An image of a running process that is not this one is not a leftover; this process's own is.
	fake(t, func([]string) ([]byte, error) {
		return []byte("image-path : /x/" + imagePrefix + "1.dmg\nbad : /x/" + imagePrefix + "notanumber.dmg\n"), nil
	})
	out.Reset()
	if code := Main(runner, true); code != 1 || !strings.Contains(out.String(), "notanumber") {
		t.Fatalf("Main with an unparsable image name = %d %q", code, out.String())
	}

	out.Reset()
	mine := imagePrefix + strconv.Itoa(os.Getpid()) + ".dmg"
	fake(t, func([]string) ([]byte, error) { return []byte("image-path : /x/" + mine + "\n"), nil })
	// pid is this process, so it is not "ended"; after the run it is a left-over of this run.
	if code := Main(runner, true); code != 1 || !strings.Contains(out.String(), "left a disk image attached") {
		t.Fatalf("Main with this run's image = %d %q", code, out.String())
	}

	out.Reset()
	fake(t, func([]string) ([]byte, error) { return []byte("hdiutil trouble"), errors.New("exit status 1") })
	if code := Main(runner, true); code != 1 || !strings.Contains(out.String(), "hdiutil info") {
		t.Fatalf("Main when hdiutil info fails = %d %q", code, out.String())
	}
	out.Reset()
	fake(t, func([]string) ([]byte, error) { return nil, nil })
	failing := runnerFunc(func() int { return 3 })
	if code := Main(failing, true); code != 3 {
		t.Fatalf("Main must pass the run's code through, got %d", code)
	}
}

type runnerFunc func() int

func (f runnerFunc) Run() int { return f() }
