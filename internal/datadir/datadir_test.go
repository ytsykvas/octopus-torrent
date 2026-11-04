package datadir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unset is a process environment without the override.
func unset(string) (string, bool) { return "", false }

// set is a process environment whose override is dir.
func set(dir string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key != Override {
			return "", false
		}
		return dir, true
	}
}

// homeAt is a home folder lookup that answers dir.
func homeAt(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

// noHome fails the test if the home folder is looked up.
func noHome(t *testing.T) func() (string, error) {
	return func() (string, error) {
		t.Error("the home folder was looked up")
		return "", errors.New("no home")
	}
}

// real is path with every symlink resolved, as Resolve returns it (t.TempDir is under /var, a
// symlink to /private/var).
func real(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestTheDefaultFolderIsCreatedPrivateUnderHome(t *testing.T) {
	home := t.TempDir()
	dir, err := resolve(unset, homeAt(home), os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(real(t, home), "Library", "Application Support", "dev.tsykvas.octopustorrent")
	if dir != want || Default(home) != filepath.Join(home, "Library", "Application Support", "dev.tsykvas.octopustorrent") {
		t.Fatalf("resolve = %s, Default = %s; want %s", dir, Default(home), want)
	}
	for _, path := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		if got := perm(t, path); got != 0o700 {
			t.Errorf("%s has mode %v, want -rwx------", path, got)
		}
	}
}

func TestAnExistingPrivateFolderIsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "library.sqlite"), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(set(dir), noHome(t), os.Getuid())
	if err != nil || got != real(t, dir) {
		t.Fatalf("resolve = %q, %v; want %s", got, err, real(t, dir))
	}
	if perm(t, dir) != 0o700 {
		t.Fatal("the folder's mode changed")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "library.sqlite")); err != nil || string(data) != "kept" {
		t.Fatalf("the folder's file = %q, %v", data, err)
	}
}

func TestTheOverrideWinsAndIsCreatedWithItsParents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "data")
	got, err := resolve(set(dir), noHome(t), os.Getuid())
	if err != nil || got != real(t, dir) {
		t.Fatalf("resolve = %q, %v; want %s", got, err, dir)
	}
	for _, path := range []string{dir, filepath.Dir(dir), filepath.Dir(filepath.Dir(dir))} {
		if got := perm(t, path); got != 0o700 {
			t.Errorf("%s has mode %v, want -rwx------", path, got)
		}
	}
}

func TestASymlinkToAFolderIsResolved(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := resolve(set(link), noHome(t), os.Getuid())
	if err != nil || got != real(t, target) {
		t.Fatalf("resolve = %q, %v; want the target %s", got, err, real(t, target))
	}
}

func TestAnOverrideThatIsNotAnAbsoluteCleanPathIsRefusedAndCreatesNothing(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for name, dir := range map[string]string{
		"set but empty": "",
		"relative":      "data",
		"unclean":       filepath.Join(root, "a") + "/../data",
		"a trailing /":  filepath.Join(root, "data") + "/",
		"a NUL":         filepath.Join(root, "da\x00ta"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolve(set(dir), noHome(t), os.Getuid()); !errors.Is(err, ErrUnusable) || !strings.Contains(err.Error(), Override) {
				t.Fatalf("resolve(%q) = %v, want ErrUnusable naming %s", dir, err, Override)
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatalf("the folder holds %v, %v after the refusal", entries, err)
			}
		})
	}
}

func TestAFolderThatIsNotPrivateToItsOwnerIsRefused(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fileLink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(file, fileLink); err != nil {
		t.Fatal(err)
	}
	folder := func(mode os.FileMode) string {
		dir := filepath.Join(t.TempDir(), "data")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		// Chmod, not Mkdir's mode: the umask would clear the group and other bits.
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	for name, c := range map[string]struct {
		dir string
		uid int
	}{
		"a file":                {file, os.Getuid()},
		"a symlink to a file":   {fileLink, os.Getuid()},
		"under a file":          {filepath.Join(file, "data"), os.Getuid()},
		"writable by the group": {folder(0o770), os.Getuid()},
		"writable by others":    {folder(0o707), os.Getuid()},
		// Others could read a file in it whose own mode allows it (an older or restored library).
		"readable by the group":  {folder(0o750), os.Getuid()},
		"readable by others":     {folder(0o755), os.Getuid()},
		"searchable by others":   {folder(0o711), os.Getuid()},
		"owned by another user":  {folder(0o700), os.Getuid() + 1},
		"a sticky shared folder": {folder(0o1777), os.Getuid()},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolve(set(c.dir), noHome(t), c.uid); !errors.Is(err, ErrUnusable) || !strings.Contains(err.Error(), c.dir) {
				t.Fatalf("resolve(%s) = %v, want ErrUnusable naming the folder", c.dir, err)
			}
		})
	}
}

func TestAHomeThatCannotBeUsedIsRefused(t *testing.T) {
	for name, home := range map[string]func() (string, error){
		"a lookup error":  func() (string, error) { return "", errors.New("$HOME is not defined") },
		"a relative home": homeAt("Users/me"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := resolve(unset, home, os.Getuid()); !errors.Is(err, ErrUnusable) {
				t.Fatalf("resolve = %v, want ErrUnusable", err)
			}
		})
	}
}

func TestFromEnvironmentReadsTheProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv(Override, dir)
	if got, err := FromEnvironment(); err != nil || got != real(t, dir) {
		t.Fatalf("FromEnvironment with %s set = %q, %v; want %s", Override, got, err, dir)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Unsetenv(Override); err != nil { // restored by the Setenv above
		t.Fatal(err)
	}
	if got, err := FromEnvironment(); err != nil || got != real(t, Default(home)) {
		t.Fatalf("FromEnvironment without %s = %q, %v; want %s", Override, got, err, Default(home))
	}
}

func TestFromOverrideRequiresTheVariable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	t.Setenv(Override, dir)
	if got, err := FromOverride(); err != nil || got != real(t, dir) {
		t.Fatalf("FromOverride with %s set = %q, %v; want %s", Override, got, err, dir)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Unsetenv(Override); err != nil {
		t.Fatal(err)
	}
	if _, err := FromOverride(); !errors.Is(err, ErrUnusable) || !strings.Contains(err.Error(), Override) {
		t.Fatalf("FromOverride without %s = %v, want ErrUnusable naming it", Override, err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 0 {
		t.Fatalf("the home folder holds %v, %v: the default folder was created", entries, err)
	}
}
