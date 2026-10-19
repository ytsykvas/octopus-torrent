//go:build darwin

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// replace sets *seam to value for the test and restores it after.
func replace[T any](t *testing.T, seam *T, value T) {
	t.Helper()
	old := *seam
	*seam = value
	t.Cleanup(func() { *seam = old })
}

// fakeTrasher records the absolute paths it is asked to trash and answers from a script, so every
// branch of Trash is reached without moving a real file.
type fakeTrasher struct {
	called []string
	fail   map[string]error // keyed by base name; a nil entry succeeds
}

func (f *fakeTrasher) TrashFile(absPath string) (string, error) {
	f.called = append(f.called, absPath)
	if err := f.fail[filepath.Base(absPath)]; err != nil {
		return "", err
	}
	return "/Trash/" + filepath.Base(absPath), nil
}

// write makes a regular file under dir and returns its name.
func write(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestTrashMovesOnlyTheNamedFilesAndLeavesTheNeighboursAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.bin")
	write(t, dir, "b.bin")
	keep := write(t, dir, "neighbour.bin")
	tr := &fakeTrasher{}

	results, err := Trash(dir, []string{"a.bin", "b.bin"}, tr)
	if err != nil {
		t.Fatalf("Trash = %v", err)
	}
	for _, r := range results {
		if !r.Trashed || r.Err != nil || r.NewLocation == "" {
			t.Errorf("%s: %+v", r.Name, r)
		}
	}
	// The trasher is asked for the opened object's own path (F_GETPATH resolves /var → /private/var).
	for i, name := range []string{"a.bin", "b.bin"} {
		if filepath.Base(tr.called[i]) != name || !strings.HasSuffix(tr.called[i], name) {
			t.Errorf("trashed %q, want the path of %s", tr.called[i], name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
		t.Errorf("the neighbour was disturbed: %v", err)
	}
}

func TestTrashRefusesNamesThatAreNotPlainInFolderPaths(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An in-folder directory symlink: os.Root resolves ".." through it physically, so a lexical join
	// would name a file outside the folder. filepath.IsLocal rejects the name first (finding C1).
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(dir, "a")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "real.bin")
	tr := &fakeTrasher{}

	results, err := Trash(dir, []string{"a/../../" + filepath.Base(outside) + "/secret", "/etc/hosts", "real.bin"}, tr)
	if err == nil || !errors.Is(err, ErrSomeRemain) {
		t.Fatalf("Trash = %v, want ErrSomeRemain", err)
	}
	if len(tr.called) != 1 || filepath.Base(tr.called[0]) != "real.bin" {
		t.Errorf("trashed %v, want only real.bin", tr.called)
	}
	for _, r := range results[:2] {
		if r.Trashed || !errors.Is(r.Err, ErrUnsafeName) {
			t.Errorf("%s was not refused as unsafe: %+v", r.Name, r)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "secret")); err != nil {
		t.Errorf("a file outside the folder was trashed: %v", err)
	}
}

func TestTrashRefusesEveryNonRegularFileWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "target.bin")
	if err := os.Symlink("target.bin", filepath.Join(dir, "link")); err != nil { // an in-folder symlink
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil { // would block an O_RDONLY open
		t.Fatal(err)
	}
	tr := &fakeTrasher{}
	results, err := Trash(dir, []string{"sub", "link", "pipe"}, tr)
	if err == nil || !errors.Is(err, ErrSomeRemain) || len(tr.called) != 0 {
		t.Fatalf("Trash = %v, called %v", err, tr.called)
	}
	for _, r := range results {
		if !errors.Is(r.Err, ErrNotRegular) {
			t.Errorf("%s gave %v, want ErrNotRegular", r.Name, r.Err)
		}
	}
	// The symlink's target is untouched: the symlink was refused, not followed and trashed.
	if _, err := os.Lstat(filepath.Join(dir, "target.bin")); err != nil {
		t.Errorf("the symlink's target was disturbed: %v", err)
	}
}

func TestTrashPassesUnicodeNamesThrough(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "é.bin") // é precomposed (NFC)
	tr := &fakeTrasher{}
	// APFS is normalisation-insensitive, so the decomposed (NFD) name resolves to the same file.
	if _, err := Trash(dir, []string{"é.bin"}, tr); err != nil {
		t.Fatalf("Trash = %v", err)
	}
	if len(tr.called) != 1 || !strings.HasSuffix(tr.called[0], "é.bin") {
		t.Fatalf("trashed %v, want the one é file", tr.called)
	}
}

func TestTrashReportsEveryFileThatRemainsOnAFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ok.bin")
	write(t, dir, "locked.bin")
	tr := &fakeTrasher{fail: map[string]error{"locked.bin": errors.New("the Finder refused")}}

	results, err := Trash(dir, []string{"ok.bin", "locked.bin", "missing.bin"}, tr)
	if err == nil || !errors.Is(err, ErrSomeRemain) {
		t.Fatalf("Trash = %v, want ErrSomeRemain", err)
	}
	if !strings.Contains(err.Error(), "locked.bin") || !strings.Contains(err.Error(), "missing.bin") || strings.Contains(err.Error(), "ok.bin") {
		t.Errorf("the error %q must name locked.bin and missing.bin, not ok.bin", err)
	}
	if !results[0].Trashed || results[1].Err == nil || results[2].Err == nil {
		t.Errorf("results = %+v", results)
	}
	if _, err := os.Stat(filepath.Join(dir, "locked.bin")); err != nil {
		t.Errorf("a refused file was removed: %v", err)
	}
}

func TestTrashCollapsesDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.bin")
	tr := &fakeTrasher{}
	results, err := Trash(dir, []string{"a.bin", "a.bin"}, tr)
	if err != nil || len(results) != 1 || len(tr.called) != 1 {
		t.Fatalf("Trash = %v, %d results, %d calls; want one of each", err, len(results), len(tr.called))
	}
}

func TestTrashRefusesAnUnsafeFolderBeforeTouchingAnything(t *testing.T) {
	tr := &fakeTrasher{}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ dir, why string }{
		{"/", "volume root"},
		{home, "home"},
		{"relative/path", "not absolute"},
		{t.TempDir() + "/", "trailing slash (not clean)"},
		{link, "a symlink"},
		{file, "a regular file"},
		{filepath.Join(t.TempDir(), "does-not-exist"), "missing"},
	} {
		if _, err := Trash(c.dir, []string{"anything"}, tr); err == nil {
			t.Errorf("Trash(%q) accepted %s", c.dir, c.why)
		}
	}
	if len(tr.called) != 0 {
		t.Errorf("an unsafe folder still trashed %v", tr.called)
	}
}

// A mounted volume's root is refused, which the own-parent test alone would miss (finding M1).
func TestTrashRefusesAVolumeRoot(t *testing.T) {
	dir := t.TempDir()
	replace(t, &mountPoint, func(string) (string, error) { return dir, nil }) // saveDir is its own mount
	if err := checkFolder(dir); !errors.Is(err, ErrUnsafeFolder) || !strings.Contains(err.Error(), "volume root") {
		t.Errorf("a volume root = %v, want ErrUnsafeFolder", err)
	}
	// A folder whose volume cannot be determined is refused (fail closed).
	replace(t, &mountPoint, func(string) (string, error) { return "", errors.New("no statfs") })
	if _, err := Trash(t.TempDir(), nil, &fakeTrasher{}); err == nil {
		t.Error("a folder whose volume could not be determined was accepted")
	}
}

// checkFolder fails closed when the home folder cannot be determined.
func TestTrashRefusesWhenTheHomeIsUnknown(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := Trash(t.TempDir(), nil, &fakeTrasher{}); err == nil {
		t.Error("a missing HOME was accepted")
	}
}

// The real syscall helpers surface their errors: Statfs on a missing path, F_GETPATH on a closed fd.
func TestTheSystemHelpersSurfaceErrors(t *testing.T) {
	if _, err := mountPoint(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("mountPoint of a missing path succeeded")
	}
	dir := t.TempDir()
	write(t, dir, "x")
	f, err := os.Open(filepath.Join(dir, "x"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := realFdPath(f); err == nil {
		t.Error("realFdPath of a closed fd succeeded")
	}
}

func TestTrashSurfacesAFolderThatCannotBeOpenedAsARoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.bin")
	replace(t, &openRoot, func(string) (*os.Root, error) { return nil, errors.New("no root") })
	if _, err := Trash(dir, []string{"a.bin"}, &fakeTrasher{}); err == nil {
		t.Error("Trash accepted a folder whose root could not be opened")
	}
}

func TestTrashSurfacesAFailureToResolveTheOpenedPath(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.bin")
	replace(t, &fdPath, func(*os.File) (string, error) { return "", errors.New("no path") })
	results, err := Trash(dir, []string{"a.bin"}, &fakeTrasher{})
	if err == nil || results[0].Err == nil {
		t.Errorf("Trash = %v, %+v; want the resolve failure surfaced", err, results)
	}
}

// A file the owner cannot read (mode 0) passes the Lstat regular check but fails the open; it stays
// on disk and is named, with nothing deleted. This is the task's "missing write permission" case.
func TestTrashReportsAFileItCannotOpen(t *testing.T) {
	dir := t.TempDir()
	name := write(t, dir, "no-read.bin")
	if err := os.Chmod(filepath.Join(dir, name), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, name), 0o600) })
	tr := &fakeTrasher{}
	results, err := Trash(dir, []string{name}, tr)
	if err == nil || !errors.Is(err, ErrSomeRemain) || len(tr.called) != 0 {
		t.Fatalf("Trash = %v, called %v", err, tr.called)
	}
	if results[0].Err == nil || results[0].Trashed {
		t.Errorf("an unreadable file: %+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Errorf("the unreadable file was removed: %v", err)
	}
}
