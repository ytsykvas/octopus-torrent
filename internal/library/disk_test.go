package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"octopustorrent/internal/testutil/volume"
)

func TestMain(m *testing.M) {
	// The child processes of process_test.go run this binary too; only the parent checks the images.
	parent := os.Getenv(lockChildEnv) == "" && os.Getenv(commitChildEnv) == ""
	os.Exit(volume.Main(m, parent))
}

func TestAFullDiskFailsTheWriteAndKeepsWhatWasCommitted(t *testing.T) {
	mount := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctopusLibraryTest"}).Mount
	path := filepath.Join(mount, "library.sqlite")
	s := openStore(t, path)
	must(t, s, "CREATE TABLE rows(id INTEGER PRIMARY KEY, n INTEGER, data BLOB)")
	for i := range 20 {
		must(t, s, "INSERT INTO rows(n) VALUES (?)", i)
	}
	before := identify(t, path)
	filler := volume.Fill(t, mount)
	err := s.Write(deadline(t, 10*time.Second), func(tx Tx) error {
		_, err := tx.Exec("INSERT INTO rows(n, data) VALUES (-1, zeroblob(1048576))")
		return err
	})
	if !errors.Is(err, ErrFull) {
		t.Fatalf("Write on a full disk = %v, want ErrFull", err)
	}
	t.Logf("Write on a full disk: %v", err)
	// The closing checkpoint cannot copy the WAL into the database file without space; the committed
	// rows stay in the WAL, which the reopen proves.
	if err := s.Close(); !errors.Is(err, ErrFull) {
		t.Fatalf("Close on a full disk = %v, want ErrFull", err)
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	again := openStore(t, path)
	if n := count(t, again); n != 20 {
		t.Fatalf("%d rows after the full disk, want 20", n)
	}
	integrity(t, again)
	if identify(t, path).inode != before.inode {
		t.Fatal("the database file was recreated")
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAPageLimitFailsTheWriteLikeAFullDisk(t *testing.T) {
	s, _ := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		if _, err := tx.Exec("PRAGMA max_page_count = 8"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO rows(n, data) VALUES (2, zeroblob(1048576))")
		return err
	})
	if !errors.Is(err, ErrFull) {
		t.Fatalf("Write past max_page_count = %v, want ErrFull", err)
	}
	if n := count(t, s); n != 1 {
		t.Fatalf("%d rows, want 1", n)
	}
}
