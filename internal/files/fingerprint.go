package files

import (
	"os"
	"syscall"
)

// Fingerprint is what an outside edit of a file cannot leave unchanged: its size, its inode, its
// modification time and its change time. A program can set the modification time back, but not the
// change time (task 009).
type Fingerprint struct {
	Size    int64
	Mtime   int64 // Unix nanoseconds
	Ctime   int64 // Unix nanoseconds
	Inode   uint64
	Dev     uint64
	Regular bool
}

// Stat fingerprints a path without following a symlink. A missing path is an error for which
// errors.Is(err, fs.ErrNotExist) holds.
func Stat(path string) (Fingerprint, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Fingerprint{}, err
	}
	st := info.Sys().(*syscall.Stat_t)
	return Fingerprint{
		Size:    st.Size,
		Mtime:   st.Mtimespec.Nano(),
		Ctime:   st.Ctimespec.Nano(),
		Inode:   st.Ino,
		Dev:     uint64(st.Dev),
		Regular: info.Mode().IsRegular(),
	}, nil
}

// FileSystem is the type name of the file system holding path, such as "apfs", "hfs" or "exfat".
func FileSystem(path string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "", err
	}
	name := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name), nil
}

// Probe is how Decide looks at the disk; System is the real one.
type Probe interface {
	Stat(path string) (Fingerprint, error)
	FileSystem(path string) (string, error)
}

// System probes the real file system.
type System struct{}

func (System) Stat(path string) (Fingerprint, error)  { return Stat(path) }
func (System) FileSystem(path string) (string, error) { return FileSystem(path) }
