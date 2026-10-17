package library

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"octopustorrent/internal/testutil/volume"
)

var (
	hashA = [20]byte{1}
	hashB = [20]byte{2}
)

func openMarks(t *testing.T, path string) *CompletionStore {
	t.Helper()
	c, err := OpenCompletion(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatalf("OpenCompletion(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func save(t *testing.T, c *CompletionStore, marks ...Mark) {
	t.Helper()
	if err := c.SaveMarks(deadline(t, 10*time.Second), marks); err != nil {
		t.Fatalf("SaveMarks: %v", err)
	}
}

func load(t *testing.T, c *CompletionStore, hash [20]byte) map[int]bool {
	t.Helper()
	got, err := c.LoadMarks(deadline(t, 10*time.Second), hash)
	if err != nil {
		t.Fatalf("LoadMarks: %v", err)
	}
	return got
}

func TestMarksAreSavedPerTorrentAndOverwritten(t *testing.T) {
	c := openMarks(t, filepath.Join(t.TempDir(), "piece-completion.sqlite"))
	if got := load(t, c, hashA); len(got) != 0 {
		t.Fatalf("a new store holds %v", got)
	}
	save(t, c, Mark{hashA, 0, true}, Mark{hashA, 1, false}, Mark{hashB, 0, true})
	got := load(t, c, hashA)
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("marks of A = %v, want {0:true 1:false}", got)
	}
	if _, known := got[2]; known {
		t.Fatal("an unsaved piece is reported as known")
	}
	if other := load(t, c, hashB); len(other) != 1 || !other[0] {
		t.Fatalf("marks of B = %v", other)
	}
	save(t, c, Mark{hashA, 1, true}, Mark{hashA, 0, false})
	got = load(t, c, hashA)
	if got[0] || !got[1] {
		t.Fatalf("overwritten marks of A = %v, want {0:false 1:true}", got)
	}
	save(t, c) // an empty batch is accepted and writes nothing
}

func TestMarksSurviveAClosedAndReopenedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	c, err := OpenCompletion(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatal(err)
	}
	save(t, c, Mark{hashA, 7, true})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	again := openMarks(t, path)
	if got := load(t, again, hashA); len(got) != 1 || !got[7] {
		t.Fatalf("after a reopen = %v", got)
	}
}

func TestAMarkOfANegativePieceIsRefusedAndWritesNothing(t *testing.T) {
	c := openMarks(t, filepath.Join(t.TempDir(), "piece-completion.sqlite"))
	err := c.SaveMarks(deadline(t, 5*time.Second), []Mark{{hashA, 1, true}, {hashA, -1, true}})
	if err == nil {
		t.Fatal("a negative piece was accepted")
	}
	if got := load(t, c, hashA); len(got) != 0 {
		t.Fatalf("a refused batch left %v", got)
	}
}

func TestACancelledContextIsRefusedByEveryOperation(t *testing.T) {
	c := openMarks(t, filepath.Join(t.TempDir(), "piece-completion.sqlite"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SaveMarks(ctx, []Mark{{hashA, 0, true}}); !errors.Is(err, ErrCanceled) {
		t.Fatalf("SaveMarks = %v, want ErrCanceled", err)
	}
	if _, err := c.LoadMarks(ctx, hashA); !errors.Is(err, ErrCanceled) {
		t.Fatalf("LoadMarks = %v, want ErrCanceled", err)
	}
	if _, err := OpenCompletion(ctx, filepath.Join(t.TempDir(), "x.sqlite")); !errors.Is(err, ErrCanceled) {
		t.Fatalf("OpenCompletion = %v, want ErrCanceled", err)
	}
}

func TestAStoreThatCannotBeOpenedIsAnErrorAndCreatesNothing(t *testing.T) {
	ctx := deadline(t, 5*time.Second)
	missing := filepath.Join(t.TempDir(), "no", "such", "folder", "piece-completion.sqlite")
	if _, err := OpenCompletion(ctx, missing); !errors.Is(err, ErrCannotOpen) {
		t.Fatalf("a missing folder = %v, want ErrCannotOpen", err)
	}
	dir := t.TempDir()
	readOnly(t, dir)
	if _, err := OpenCompletion(ctx, filepath.Join(dir, "piece-completion.sqlite")); !errors.Is(err, ErrCannotOpen) {
		t.Fatalf("a read-only folder = %v, want ErrCannotOpen", err)
	}
	if got := names(t, dir); len(got) != 0 {
		t.Fatalf("the read-only folder now holds %v", got)
	}
	if _, err := OpenCompletion(ctx, t.TempDir()); err == nil {
		t.Fatal("a folder was opened as a store")
	}
}

func TestAFileThatIsNotADatabaseIsCorruptAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	garbage := []byte("this is not a database, but it is long enough for SQLite to read a header from it\n")
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCompletion(deadline(t, 5*time.Second), path); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("OpenCompletion = %v, want ErrCorrupt", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(garbage) {
		t.Fatalf("the file was changed: %q, %v", got, err)
	}
}

func TestAStoreOfAnotherVersionOrPurposeIsNeverCorruptAndNeverChanged(t *testing.T) {
	for name, setup := range map[string]func(*Store){
		"a newer version": func(s *Store) {
			must(t, s, "CREATE TABLE piece_completion(infohash BLOB NOT NULL, piece INTEGER NOT NULL, complete INTEGER NOT NULL, PRIMARY KEY(infohash, piece)) WITHOUT ROWID")
			must(t, s, "PRAGMA user_version = 2")
		},
		"tables of another purpose": func(s *Store) {
			must(t, s, "CREATE TABLE torrents(id INTEGER PRIMARY KEY)")
		},
		"another table at the same version": func(s *Store) {
			must(t, s, "CREATE TABLE torrents(id INTEGER PRIMARY KEY)")
			must(t, s, "PRAGMA user_version = 1")
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
			s := openStore(t, path)
			setup(s)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			before := identify(t, path)
			_, err := OpenCompletion(deadline(t, 5*time.Second), path)
			if !errors.Is(err, ErrUnsupportedVersion) || errors.Is(err, ErrCorrupt) {
				t.Fatalf("OpenCompletion = %v, want ErrUnsupportedVersion and not ErrCorrupt", err)
			}
			if identify(t, path) != before {
				t.Fatal("the store was changed")
			}
		})
	}
}

func TestAFullDiskFailsTheBatchWholeAndKeepsEarlierMarks(t *testing.T) {
	mount := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctopusMarksTest"}).Mount
	path := filepath.Join(mount, "piece-completion.sqlite")
	c := openMarks(t, path)
	save(t, c, Mark{hashA, 0, true}, Mark{hashA, 1, true})
	filler := volume.Fill(t, mount)
	// Twenty thousand rows need new pages that the full volume cannot give.
	batch := make([]Mark, 20000)
	for i := range batch {
		batch[i] = Mark{hashB, i, true}
	}
	err := c.SaveMarks(deadline(t, 20*time.Second), batch)
	if !errors.Is(err, ErrFull) {
		t.Fatalf("SaveMarks on a full disk = %v, want ErrFull", err)
	}
	if got := load(t, c, hashB); len(got) != 0 {
		t.Fatalf("%d marks of the failed batch are visible, want none", len(got))
	}
	if got := load(t, c, hashA); len(got) != 2 {
		t.Fatalf("earlier marks = %v", got)
	}
	if err := c.Close(); !errors.Is(err, ErrFull) {
		t.Fatalf("Close on a full disk = %v, want ErrFull", err)
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	again := openMarks(t, path)
	if got := load(t, again, hashA); len(got) != 2 {
		t.Fatalf("after the disk had space again = %v", got)
	}
}

func TestAStoreWhoseSchemaCannotBeCommittedOnAFullDiskIsAnError(t *testing.T) {
	t.Run("an empty database on a full disk", func(t *testing.T) {
		mount := volume.New(t, volume.Options{Size: "8m", FS: "HFS+", Name: "OctopusMarksTest"}).Mount
		path := filepath.Join(mount, "piece-completion.sqlite")
		empty, err := Open(deadline(t, 5*time.Second), path)
		if err != nil {
			t.Fatal(err)
		}
		if err := empty.Close(); err != nil {
			t.Fatal(err)
		}
		// 40 KiB stay free: enough for Open to create its WAL files, too little for the table.
		filler := volume.Fill(t, mount)
		if info, err := os.Stat(filler); err != nil {
			t.Fatal(err)
		} else if err := os.Truncate(filler, info.Size()-40<<10); err != nil {
			t.Fatal(err)
		}
		c, err := OpenCompletion(deadline(t, 10*time.Second), path)
		if c != nil {
			_ = c.Close() // never leave a store open on a volume that is about to go away
		}
		if !errors.Is(err, ErrFull) {
			t.Fatalf("OpenCompletion = %v, want ErrFull", err)
		}
	})
}

func TestATableChangedBehindTheStoresBackIsReportedNotHidden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	c := openMarks(t, path)
	save(t, c, Mark{hashA, 0, true})
	other := openStore(t, path)

	must(t, other, "DROP TABLE piece_completion")
	if err := c.SaveMarks(deadline(t, 5*time.Second), []Mark{{hashA, 1, true}}); err == nil {
		t.Fatal("SaveMarks into a missing table succeeded")
	}
	if _, err := c.LoadMarks(deadline(t, 5*time.Second), hashA); err == nil {
		t.Fatal("LoadMarks from a missing table succeeded")
	}

	must(t, other, "CREATE TABLE piece_completion(infohash BLOB, piece TEXT, complete INTEGER)")
	must(t, other, "INSERT INTO piece_completion VALUES (?, 'not a number', 1)", hashA[:])
	if _, err := c.LoadMarks(deadline(t, 5*time.Second), hashA); err == nil {
		t.Fatal("LoadMarks of a row with a text piece succeeded")
	}
}

func TestSchemaSetupOnAnEndedTransactionIsAnError(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "piece-completion.sqlite"))
	ctx := deadline(t, 5*time.Second)
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := ensureCompletionSchema(Tx{ctx, tx}); !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("ensureCompletionSchema on an ended transaction = %v, want sql.ErrTxDone", err)
	}
}
