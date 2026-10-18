//go:build darwin

// Package platform isolates access to macOS native capabilities in Go, so React reaches them only
// through the Wails facade and never through a shell or a third-party workaround (STACK-01). Its
// first capability is moving a payload file to the Trash (task 012): reversible, one result per
// file, inside the folder the torrent owns.
package platform

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	// ErrUnsafeFolder is a save folder too broad or ambiguous to delete within: not a clean absolute
	// path, not a real directory, a volume root, or the user's home. The real authority is the folder
	// a torrent owns (task 009's Layout); this is the backstop.
	ErrUnsafeFolder = errors.New("the folder is not a safe place to delete files within")
	// ErrUnsafeName is a name that is not a plain path inside the folder (absolute, or climbing out
	// with ".."). The engine never produces one; a caller must not pass one.
	ErrUnsafeName = errors.New("the name is not a plain path inside the folder")
	// ErrNotRegular is a name that is not a regular file (a directory, a symlink, a FIFO, a device).
	ErrNotRegular = errors.New("not a regular file")
	// ErrBadPath is a path the native Trasher must not receive: empty, relative, with a NUL, or not
	// valid UTF-8. Trash never produces one (it passes the opened file's own path); the guard protects
	// the exported Trasher interface, whose native call would otherwise abort the process.
	ErrBadPath = errors.New("the path is not a valid absolute UTF-8 path")
	// ErrSomeRemain says at least one file could not be moved to the Trash and stayed on disk; the
	// returned Results say which. The caller must not treat the operation as a success (DEL-04).
	ErrSomeRemain = errors.New("some files were not moved to the Trash")
)

// Trasher moves one file to the Trash by its absolute path and returns the item's new location
// there, or an error that leaves the file untouched. It never deletes irreversibly.
type Trasher interface {
	TrashFile(absPath string) (newLocation string, err error)
}

// Result is the outcome for one requested name. Trashed with no Err means the file is in the Trash
// at NewLocation; otherwise Err says why it stayed, and it is still on disk. A caller records the
// intent and this outcome separately from any SQLite write: the two are not one transaction. The
// Results, not the error text, are the per-file truth (DEL-04).
type Result struct {
	Name        string
	Trashed     bool
	NewLocation string
	Err         error
}

// Seams a test replaces to reach branches the real syscalls do not fail on.
var (
	openRoot = os.OpenRoot
	fdPath   = realFdPath
)

// Trash moves each of names, resolved inside saveDir, to the Trash with t, and returns one Result
// per distinct name. It never removes a file irreversibly: a name that is not a plain in-folder
// path, is not a regular file, or that the Trasher refuses stays on disk and is marked in its Result
// and in the returned ErrSomeRemain (DEL-04).
//
// Confinement at check time: os.Root resolves every name inside saveDir, so a name with "..", an
// absolute path or a symlink that leaves the folder is refused by the standard library. The file is
// opened without following a final symlink and moved by the path of the opened object
// (fcntl F_GETPATH), so the move acts on the file that was checked, not on whatever its name points
// to a moment later. A regular file swapped for another, or an intermediate directory swapped for a
// symlink, between the check and the move is a residual the path-based Trash API cannot fully close;
// the move is reversible and the residual is documented for the delete journal (task 032).
func Trash(saveDir string, names []string, t Trasher) ([]Result, error) {
	if err := checkFolder(saveDir); err != nil {
		return nil, err
	}
	root, err := openRoot(saveDir)
	if err != nil {
		return nil, fmt.Errorf("open the save folder as a root: %w", err)
	}
	defer root.Close()

	seen := make(map[string]bool, len(names))
	var results []Result
	var remained []string
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		r := Result{Name: name}
		loc, err := trashOne(root, name, t)
		if err != nil {
			r.Err = err
			remained = append(remained, name)
		} else {
			r.Trashed, r.NewLocation = true, loc
		}
		results = append(results, r)
	}
	if len(remained) > 0 {
		quoted := make([]string, len(remained))
		for i, n := range remained {
			quoted[i] = fmt.Sprintf("%q", n)
		}
		return results, fmt.Errorf("%w: %s", ErrSomeRemain, strings.Join(quoted, ", "))
	}
	return results, nil
}

// trashOne resolves one name inside root and moves the opened file to the Trash. It refuses a name
// that leaves the folder or is not a regular file, opens it without blocking (a FIFO would) and
// without following a final symlink, and moves it by the opened object's own path.
func trashOne(root *os.Root, name string, t Trasher) (string, error) {
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("%w: %s", ErrUnsafeName, name)
	}
	li, err := root.Lstat(name)
	if err != nil {
		return "", err
	}
	if !li.Mode().IsRegular() { // a directory, symlink, FIFO or device: refused before any open
		return "", fmt.Errorf("%w: %s", ErrNotRegular, name)
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil { // the name turned into a symlink since the Lstat, or is unreadable
		return "", err
	}
	defer f.Close()
	path, err := fdPath(f) // the opened object's own path, so the move cannot be redirected by name
	if err != nil {
		return "", err
	}
	return t.TrashFile(path) // f stays open across the move, pinning the vnode
}

// checkFolder refuses a save folder that is not a clean absolute path, is not a real directory, is a
// volume root, or is the user's home. It fails closed: a folder it cannot inspect is refused.
func checkFolder(saveDir string) error {
	if !filepath.IsAbs(saveDir) || filepath.Clean(saveDir) != saveDir {
		return fmt.Errorf("%w: %s is not a clean absolute path", ErrUnsafeFolder, saveDir)
	}
	fi, err := os.Lstat(saveDir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrUnsafeFolder, saveDir)
	}
	mount, err := mountPoint(saveDir)
	if err != nil {
		return err
	}
	if mount == saveDir {
		return fmt.Errorf("%w: %s is a volume root", ErrUnsafeFolder, saveDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if hi, err := os.Stat(home); err == nil && os.SameFile(fi, hi) {
		return fmt.Errorf("%w: %s is the home folder", ErrUnsafeFolder, saveDir)
	}
	return nil
}

// mountPoint is the mount point of the volume holding path (Statfs f_mntonname).
var mountPoint = func(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", err
	}
	name := make([]byte, 0, len(st.Mntonname))
	for _, c := range st.Mntonname {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name), nil
}

// realFdPath returns the path macOS currently associates with the open file (fcntl F_GETPATH), so a
// move acts on the file that was opened and checked, not on its name.
func realFdPath(f *os.File) (string, error) {
	buf := make([]byte, 1024) // MAXPATHLEN
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETPATH, uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return "", errno
	}
	if n := bytes.IndexByte(buf, 0); n >= 0 {
		buf = buf[:n]
	}
	return string(buf), nil
}
