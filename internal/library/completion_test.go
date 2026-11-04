package library

import (
	"context"
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
	if err := c.SaveMarks(ctx, nil); !errors.Is(err, ErrCanceled) {
		t.Fatalf("SaveMarks of no marks = %v, want ErrCanceled", err)
	}
	if _, err := c.LoadMarks(ctx, hashA); !errors.Is(err, ErrCanceled) {
		t.Fatalf("LoadMarks = %v, want ErrCanceled", err)
	}
	if _, err := OpenCompletion(ctx, filepath.Join(t.TempDir(), "x.sqlite")); !errors.Is(err, ErrCanceled) {
		t.Fatalf("OpenCompletion = %v, want ErrCanceled", err)
	}
}

// The read-only folder and the other open failures are store_test.go's: OpenCompletion opens through
// Open.
func TestAStoreThatCannotBeOpenedIsAnError(t *testing.T) {
	ctx := deadline(t, 5*time.Second)
	missing := filepath.Join(t.TempDir(), "no", "such", "folder", "piece-completion.sqlite")
	if _, err := OpenCompletion(ctx, missing); !errors.Is(err, ErrCannotOpen) {
		t.Fatalf("a missing folder = %v, want ErrCannotOpen", err)
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

func TestANewStoreIsStampedAndReopensUnchanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	if err := openMarks(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	st := stateOf(t, path)
	if st.applicationID != 0x4F545043 || st.version != 1 {
		t.Fatalf("application_id %#x, user_version %d; want OTPC, 1", st.applicationID, st.version)
	}
	// STRICT, like every library table: SQLite refuses a value of another type instead of storing it.
	if strict := scalar[int](t, path, "SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = 'piece_completion'"); strict != 1 {
		t.Fatalf("piece_completion strict = %d, want 1", strict)
	}
	before := identify(t, path)
	if err := openMarks(t, path).Close(); err != nil {
		t.Fatal(err)
	}
	if identify(t, path) != before {
		t.Fatal("reopening a current store changed it")
	}
}

// 009's piece_completion table, written before the migration policy: no application_id.
const unstamped009 = "CREATE TABLE piece_completion(infohash BLOB NOT NULL, piece INTEGER NOT NULL, complete INTEGER NOT NULL, PRIMARY KEY(infohash, piece)) WITHOUT ROWID"

func TestAStoreOfAnotherVersionOrPurposeIsNeverCorruptAndNeverChanged(t *testing.T) {
	stamp := "PRAGMA application_id = 1330925635"
	for name, c := range map[string]struct {
		statements []string
		want       error
	}{
		"a newer version":                   {[]string{stamp, unstamped009, "PRAGMA user_version = 2"}, ErrUnsupportedVersion},
		"an unstamped store of 009":         {[]string{unstamped009, "PRAGMA user_version = 1"}, ErrUnknownSchema},
		"tables of another purpose":         {[]string{"CREATE TABLE torrents(id INTEGER PRIMARY KEY)"}, ErrUnknownSchema},
		"another table at the same version": {[]string{stamp, "CREATE TABLE torrents(id INTEGER PRIMARY KEY)", "PRAGMA user_version = 1"}, ErrUnknownSchema},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
			foreignDB(t, path, c.statements...)
			before := identify(t, path)
			_, err := OpenCompletion(deadline(t, 5*time.Second), path)
			if !errors.Is(err, c.want) || errors.Is(err, ErrCorrupt) {
				t.Fatalf("OpenCompletion = %v, want %v and not ErrCorrupt", err, c.want)
			}
			if identify(t, path) != before {
				t.Fatal("the store was changed")
			}
		})
	}
}

func TestAFullDiskFailsTheBatchWholeAndKeepsEarlierMarks(t *testing.T) {
	mount := smallVolume(t)
	path := filepath.Join(mount, "piece-completion.sqlite")
	c := openMarks(t, path)
	// Enough earlier marks that the WAL holds pages past the end of the database file, which the
	// closing checkpoint must append.
	earlier := make([]Mark, 2000)
	for i := range earlier {
		earlier[i] = Mark{hashA, i, true}
	}
	save(t, c, earlier...)
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
	if got := load(t, c, hashA); len(got) != len(earlier) {
		t.Fatalf("%d earlier marks, want %d", len(got), len(earlier))
	}
	if err := c.Close(); !errors.Is(err, ErrFull) {
		t.Fatalf("Close on a full disk = %v, want ErrFull", err)
	}
	if err := os.Remove(filler); err != nil {
		t.Fatal(err)
	}
	again := openMarks(t, path)
	if got := load(t, again, hashA); len(got) != len(earlier) {
		t.Fatalf("after the disk had space again: %d marks, want %d", len(got), len(earlier))
	}
}

func TestAStoreWhoseSchemaCannotBeCommittedOnAFullDiskIsAnError(t *testing.T) {
	mount := smallVolume(t)
	path := filepath.Join(mount, "piece-completion.sqlite")
	// An empty database in WAL: a schema without migrations creates nothing.
	empty, err := open(deadline(t, 5*time.Second), path, defaults, schema{name: "empty", applicationID: 1})
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
}

func TestATableChangedBehindTheStoresBackIsReportedNotHidden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	c := openMarks(t, path)
	save(t, c, Mark{hashA, 0, true})
	// Another program changes the table behind the store's back, through the driver alone.
	other := &Store{path: path, file: c.store.file, write: rawDB(t, path), read: rawDB(t, path)}
	t.Cleanup(func() { _ = other.write.Close(); _ = other.read.Close() })

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
