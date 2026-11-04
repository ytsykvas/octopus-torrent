// Package datadir finds the folder that holds the application's own data: the library and the
// piece-completion marks (task 014). It is ~/Library/Application Support/dev.tsykvas.octopustorrent,
// or the folder the OCTOPUS_DATA_DIR environment variable names, so that a workspace, a test, the gate
// or a development build never opens the owner's library. Decision:
// docs/decisions/library-schema-and-data-folder.md.
package datadir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Override is the environment variable that names the data folder instead of the default.
const Override = "OCTOPUS_DATA_DIR"

// LibraryFile is the library database's name in the data folder.
const LibraryFile = "library.sqlite"

// name is the data folder's name under Application Support: the application's bundle ID.
const name = "dev.tsykvas.octopustorrent"

// ErrUnusable is a data folder that cannot be used: the override is not an absolute, clean path, the
// home folder is unknown, or the folder cannot be created, is not a folder, is not the current
// user's, or is open to its group or others (any of their permission bits).
var ErrUnusable = errors.New("the data folder cannot be used")

// Default is the data folder of the user whose home folder is home.
func Default(home string) string {
	return filepath.Join(home, "Library", "Application Support", name)
}

// FromEnvironment resolves the data folder of this process: the override when it is set, the default
// otherwise. It returns the folder with every symlink resolved, created (0700, with its parents) when
// it was missing.
func FromEnvironment() (string, error) {
	return resolve(os.LookupEnv, os.UserHomeDir, os.Getuid())
}

// FromOverride resolves the folder the override names, and refuses to run without it: a development
// build never opens the owner's data folder.
func FromOverride() (string, error) {
	required := func() (string, error) {
		return "", fmt.Errorf("%s is not set, and a development build never opens the owner's data folder", Override)
	}
	return resolve(os.LookupEnv, required, os.Getuid())
}

// resolve chooses the folder (the override, read through lookupEnv, wins; home is looked up only
// without it), creates it when missing, and accepts it only when its resolved path is a folder of
// the user uid that its group and others cannot open at all: then no file in it is theirs to read,
// whatever the file's own mode.
func resolve(lookupEnv func(string) (string, bool), home func() (string, error), uid int) (string, error) {
	dir, err := choose(lookupEnv, home)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnusable, err)
	}
	err = os.MkdirAll(dir, 0o700)
	resolved := dir
	if err == nil {
		resolved, err = filepath.EvalSymlinks(dir)
	}
	var info os.FileInfo
	if err == nil {
		info, err = os.Stat(resolved)
	}
	if err == nil {
		owner := info.Sys().(*syscall.Stat_t).Uid
		if !info.IsDir() || int(owner) != uid || info.Mode().Perm()&0o077 != 0 {
			err = fmt.Errorf("%s is a %v of uid %d; want a folder of uid %d that only its owner can open (chmod 700)", resolved, info.Mode(), owner, uid)
		}
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", ErrUnusable, dir, err)
	}
	return resolved, nil
}

// choose is the folder before it is created: the override as given, or the default under home.
func choose(lookupEnv func(string) (string, bool), home func() (string, error)) (string, error) {
	if dir, ok := lookupEnv(Override); ok {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || strings.ContainsRune(dir, 0) {
			return "", fmt.Errorf("%s=%q is not an absolute, clean path", Override, dir)
		}
		return dir, nil
	}
	h, err := home()
	if err == nil && !filepath.IsAbs(h) {
		err = fmt.Errorf("the home folder %q is not absolute", h)
	}
	return Default(h), err
}
