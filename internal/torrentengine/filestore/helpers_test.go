package filestore

import (
	g "github.com/anacrolix/generics"
	"os"
	"path/filepath"
	"testing"
)

func filepathStat(path string) (os.FileInfo, error) { return os.Stat(path) }

// writeFile returns the path of a regular file, for tests that need a path that is not a folder.
func writeFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func someFalse() g.Option[bool] { return g.Some(false) }
