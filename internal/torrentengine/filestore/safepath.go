package filestore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/files"
)

// checkPaths refuses a torrent whose files would be written through a symlink, or whose zero-length
// file sits on an existing file: the engine follows symlinks (finding 16) and truncates an existing
// file at a zero-length path when it opens the torrent (finding 8). The check runs on every open. It
// is not atomic with the engine's own open: a symlink put in place between the two is not seen.
func checkPaths(root string, info *metainfo.Info, ranges []files.FileRange) error {
	for i, f := range info.UpvertedFiles() {
		if err := checkPath(root, ranges[i].Path, f.Length == 0); err != nil {
			return err
		}
	}
	return nil
}

// checkPath looks at every component of path below root.
func checkPath(root, path string, empty bool) error {
	// Layout has already shown that path is below root.
	parts := strings.Split(strings.TrimPrefix(path, root+string(filepath.Separator)), string(filepath.Separator))
	current := root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing below this exists yet
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s is a symlink", ErrUnsafePath, current)
		}
		if i == len(parts)-1 && empty && !(info.Mode().IsRegular() && info.Size() == 0) {
			return fmt.Errorf("%w: %s exists and the engine would truncate it", ErrUnsafePath, current)
		}
	}
	return nil
}
