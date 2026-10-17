package library

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestADeletedDatabaseIsNotRecreatedAndNotWrittenTo(t *testing.T) {
	s, path := withTable(t)
	must(t, s, "INSERT INTO rows(n) VALUES (1)")
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(name); err != nil {
			t.Fatal(err)
		}
	}
	// A write would land in the deleted inode the writer still holds, and a new reader would create
	// an empty file at the path; both are refused.
	err := s.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (2)")
		return err
	})
	if !errors.Is(err, ErrReplaced) {
		t.Fatalf("Write after the file was deleted = %v, want ErrReplaced", err)
	}
	err = s.Read(deadline(t, 5*time.Second), func(Tx) error { return nil })
	if !errors.Is(err, ErrReplaced) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read after the file was deleted = %v, want ErrReplaced naming the missing file", err)
	}
	if got := names(t, filepath.Dir(path)); len(got) != 0 {
		t.Fatalf("the folder now holds %v", got)
	}
}

func TestAReplacedDatabaseIsNotWrittenTo(t *testing.T) {
	s, path := withTable(t)
	other := filepath.Join(t.TempDir(), "other.sqlite")
	if err := os.WriteFile(other, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(other, path); err != nil {
		t.Fatal(err)
	}
	err := s.Write(deadline(t, 5*time.Second), func(Tx) error { return nil })
	if !errors.Is(err, ErrReplaced) {
		t.Fatalf("Write after the file was replaced = %v, want ErrReplaced", err)
	}
}

func TestAReaderCannotWriteEvenWhenItAsks(t *testing.T) {
	s, _ := withTable(t)
	err := s.Read(deadline(t, 5*time.Second), func(tx Tx) error {
		if _, err := tx.Exec("PRAGMA query_only = 0"); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (1)")
		return err
	})
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("an insert through a reader = %v, want ErrReadOnly", err)
	}
	if n := count(t, s); n != 0 {
		t.Fatalf("%d rows, want 0", n)
	}
}

func TestAnEmptyFileBesideACommittedWALIsRefusedAndKept(t *testing.T) {
	s, path := withTable(t)
	for i := range 5 {
		must(t, s, "INSERT INTO rows(n) VALUES (?)", i)
	}
	// The commits are only in the WAL until Close checkpoints them; a copy of the WAL next to an
	// empty main file is what a partial restore or a copy of the main file alone leaves behind.
	wal, err := os.ReadFile(path + "-wal")
	if err != nil || len(wal) == 0 {
		t.Fatalf("the WAL holds %d bytes, %v; want the commits", len(wal), err)
	}
	for name, main := range map[string]bool{"empty main file": true, "no main file": false} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			copied := filepath.Join(dir, "library.sqlite")
			if err := os.WriteFile(copied+"-wal", wal, 0o600); err != nil {
				t.Fatal(err)
			}
			if main {
				if err := os.WriteFile(copied, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := identify(t, copied+"-wal")
			// SQLite itself would delete a WAL that sits next to an empty database.
			_, err := Open(deadline(t, 5*time.Second), copied)
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Open = %v, want ErrCorrupt", err)
			}
			if identify(t, copied+"-wal") != before {
				t.Fatal("the WAL changed")
			}
			if want := map[bool]int{true: 2, false: 1}[main]; len(names(t, dir)) != want {
				t.Fatalf("the folder holds %v", names(t, dir))
			}
		})
	}
}

func TestAPathThatIsNotCleanIsRefused(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		filepath.Join(dir, "real.sqlite") + "\x00/ignored/tail.sqlite",
		dir + "/./library.sqlite",
		dir + "/x/../library.sqlite",
		dir + "//library.sqlite",
	} {
		if _, err := Open(deadline(t, 5*time.Second), path); err == nil {
			t.Errorf("Open(%q) succeeded", path)
		}
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("the folder now holds %v", got)
	}
}

func TestADanglingSymlinkIsRefusedAndItsTargetNotCreated(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.sqlite")
	path := filepath.Join(dir, "library.sqlite")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	_, err := Open(deadline(t, 5*time.Second), path)
	if !errors.Is(err, ErrCannotOpen) {
		t.Fatalf("Open through a dangling symlink = %v, want ErrCannotOpen", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the symlink's target was created: %v", err)
	}
}

func TestANewDatabaseIsPrivateToItsOwner(t *testing.T) {
	_, path := withTable(t)
	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %v, want -rw-------", filepath.Base(name), mode)
		}
	}
}
