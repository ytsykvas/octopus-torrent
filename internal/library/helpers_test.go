package library

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"octopustorrent/internal/testutil/volume"
)

// deadline gives a context that ends with the test.
func deadline(t *testing.T, limit time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	t.Cleanup(cancel)
	return ctx
}

// rowsSchema is the test schema of the store's own tests: one table rows(id, n, data).
var rowsSchema = schema{name: "test rows", applicationID: 0x54524f57, migrations: []string{
	"CREATE TABLE rows(id INTEGER PRIMARY KEY, n INTEGER, data BLOB)",
}}

// openRows opens a store of rowsSchema in path with the default settings.
func openRows(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, defaults, rowsSchema)
}

// openStore opens a store of rowsSchema in path with the default settings and closes it after the
// test.
func openStore(t *testing.T, path string) *Store {
	t.Helper()
	return openWith(t, path, defaults, rowsSchema)
}

// openWith opens a store of sch in path with set and closes it after the test.
func openWith(t *testing.T, path string, set settings, sch schema) *Store {
	t.Helper()
	s, err := open(deadline(t, 10*time.Second), path, set, sch)
	if err != nil {
		t.Fatalf("open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// withTable opens a fresh store of rowsSchema.
func withTable(t *testing.T) (*Store, string) {
	t.Helper()
	return withTableUsing(t, defaults)
}

// withTableUsing is withTable with other settings.
func withTableUsing(t *testing.T, set settings) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	return tableAt(t, path, set), path
}

// tableAt opens a store of rowsSchema in path with set and closes it after the test.
func tableAt(t *testing.T, path string, set settings) *Store {
	t.Helper()
	return openWith(t, path, set, rowsSchema)
}

// smallVolume attaches an 8 MiB HFS+ volume for the test and returns where it is mounted: small
// enough to fill in a moment.
func smallVolume(t *testing.T) string {
	t.Helper()
	return volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctopusLibraryTest"}).Mount
}

// must runs one statement in its own write transaction.
func must(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	err := s.Write(deadline(t, 10*time.Second), func(tx Tx) error {
		_, err := tx.Exec(query, args...)
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

// count reads the number of rows through the read pool.
func count(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	err := s.Read(deadline(t, 10*time.Second), func(tx Tx) error {
		return tx.QueryRow("SELECT COUNT(*) FROM rows").Scan(&n)
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// integrity runs the full integrity check.
func integrity(t *testing.T, s *Store) {
	t.Helper()
	var result string
	err := s.Read(deadline(t, 30*time.Second), func(tx Tx) error {
		return tx.QueryRow("PRAGMA integrity_check").Scan(&result)
	})
	if err != nil || result != "ok" {
		t.Fatalf("integrity_check = %q, %v", result, err)
	}
}

// fileID is what proves a file was not reset: its content and its inode.
type fileID struct {
	sum   [32]byte
	inode uint64
}

func identify(t *testing.T, path string) fileID {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fileID{sha256.Sum256(data), info.Sys().(*syscall.Stat_t).Ino}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// shortBusy is the default settings with a busy timeout short enough for a test to wait it out.
func shortBusy(timeout time.Duration) settings {
	set := defaults
	set.busyTimeout = timeout
	return set
}

// readOnly makes dir read-only until the test ends.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}
