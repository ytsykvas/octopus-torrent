package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/testutil/volume"
)

// openLibraryAt opens the library at path with set and closes it after the test.
func openLibraryAt(t *testing.T, path string, set settings) *Library {
	t.Helper()
	return &Library{openWith(t, path, set, librarySchema)}
}

// snapshot is everything the repositories read back, to compare a library before and after a change.
type snapshot struct {
	torrents      []Torrent
	settings      contracts.Settings
	settingsFound bool
	operation     Operation
}

func snapshotOf(t *testing.T, lib *Library) snapshot {
	t.Helper()
	s := snapshot{torrents: torrents(t, lib)}
	s.settings, s.settingsFound = settingsOf(t, lib)
	s.operation = operation(t, lib, opA)
	return s
}

// seed stores torrent A (three files, imported by operation A, still running) and settings.
func seed(t *testing.T, lib *Library) {
	t.Helper()
	if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idA, 1), importIntent(opA, newID(t))); err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveSettings(deadline(t, 5*time.Second), contracts.Settings{Theme: contracts.ThemeSystem, Language: "en"}); err != nil {
		t.Fatal(err)
	}
}

// writes are a change through every write method of the library.
func writes(t *testing.T) map[string]func(context.Context, *Library) error {
	t.Helper()
	other := sampleTorrent(newID(t), 0x77)
	otherOp := importIntent(newID(t), newID(t))
	return map[string]func(context.Context, *Library) error{
		"AddTorrent":      func(ctx context.Context, l *Library) error { return l.AddTorrent(ctx, other, otherOp) },
		"SetManualPaused": func(ctx context.Context, l *Library) error { return l.SetManualPaused(ctx, idA, true) },
		"SetFilesWanted":  func(ctx context.Context, l *Library) error { return l.SetFilesWanted(ctx, idA, []int{0, 1}, false) },
		"SaveSettings": func(ctx context.Context, l *Library) error {
			return l.SaveSettings(ctx, contracts.Settings{Theme: contracts.ThemeDark, Language: "uk", GlobalUploadDisabled: true})
		},
		"FinishOperation": func(ctx context.Context, l *Library) error {
			return l.FinishOperation(ctx, opA, OperationEnd{State: contracts.OperationSucceeded, FinishedAt: time.Now()})
		},
	}
}

// sameSnapshot fails the test when the library reads back other values than before.
func sameSnapshot(t *testing.T, what string, got, want snapshot) {
	t.Helper()
	sameTorrents(t, got.torrents, want.torrents)
	sameOperation(t, got.operation, want.operation)
	if got.settingsFound != want.settingsFound || !reflect.DeepEqual(got.settings, want.settings) {
		t.Errorf("%s: settings %+v (%v), want %+v (%v)", what, got.settings, got.settingsFound, want.settings, want.settingsFound)
	}
}

// Each write, failing at its COMMIT, gives the code that tells what happened, and stores nothing.
func TestAWriteThatFailsAtItsCommitStoresNothingAndSaysWhy(t *testing.T) {
	for name, write := range writes(t) {
		t.Run(name, func(t *testing.T) {
			t.Run("cancelled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				trigger := &armed{fn: cancel}
				set := defaults
				set.beforeCommit = trigger.beforeCommit
				path := filepath.Join(t.TempDir(), "library.sqlite")
				lib := openLibraryAt(t, path, set)
				seed(t, lib)
				before := snapshotOf(t, lib)
				trigger.on = true
				if problem := Problem(write(ctx, lib)); problem == nil || problem.Code != contracts.CodeOperationCancelled {
					t.Fatalf("%s cancelled before its COMMIT = %v, want operation.cancelled", name, problem)
				}
				sameSnapshot(t, name, snapshotOf(t, reopen(t, lib, path)), before)
			})
			t.Run("a file that cannot grow", func(t *testing.T) {
				restore := func() {}
				trigger := &armed{fn: func() { restore = capFileSize(t) }}
				set := defaults
				set.beforeCommit = trigger.beforeCommit
				path := filepath.Join(t.TempDir(), "library.sqlite")
				lib := openLibraryAt(t, path, set)
				seed(t, lib)
				before := snapshotOf(t, lib)
				truncateWAL(t, lib.store)
				trigger.on = true
				problem := Problem(write(deadline(t, 5*time.Second), lib))
				restore()
				if problem == nil || problem.Code != contracts.CodeLibraryOutcomeUnknown {
					t.Fatalf("%s whose COMMIT could not write = %v, want library.outcomeUnknown", name, problem)
				}
				// EFBIG leaves no commit frame, so nothing was stored; the caller is still told the
				// outcome is unknown, because an fsync failure after the commit frame looks the same.
				again := reopen(t, lib, path)
				sameSnapshot(t, name, snapshotOf(t, again), before)
				integrity(t, again.store)
			})
		})
	}
}

// A full disk at COMMIT is a known outcome for every write: nothing stored, library.diskFull.
func TestAWriteOnAFullDiskStoresNothingAndSaysSo(t *testing.T) {
	mount := smallVolume(t)
	path := filepath.Join(mount, "library.sqlite")
	var filler string
	trigger := &armed{fn: func() { filler = volume.Fill(t, mount) }}
	set := defaults
	set.beforeCommit = trigger.beforeCommit
	lib := openLibraryAt(t, path, set)
	seed(t, lib)
	before := snapshotOf(t, lib)
	for name, write := range writes(t) {
		truncateWAL(t, lib.store)
		trigger.on = true
		problem := Problem(write(deadline(t, 10*time.Second), lib))
		if err := os.Remove(filler); err != nil {
			t.Fatal(err)
		}
		if problem == nil || problem.Code != contracts.CodeLibraryDiskFull {
			t.Errorf("%s on a full disk = %v, want library.diskFull", name, problem)
		}
		sameSnapshot(t, name, snapshotOf(t, lib), before)
	}
	sameSnapshot(t, "after a reopen", snapshotOf(t, reopen(t, lib, path)), before)
}

// losses are the ways a library is lost while the application runs: each loses the library at path
// and returns the folder in which nothing may appear afterwards.
var losses = map[string]func(t *testing.T, path string) string{
	"deleted": func(t *testing.T, path string) string {
		for _, name := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
		return filepath.Dir(path)
	},
	"its folder moved": func(t *testing.T, path string) string {
		if err := os.Rename(filepath.Dir(path), filepath.Dir(path)+"-moved"); err != nil {
			t.Fatal(err)
		}
		return filepath.Dir(filepath.Dir(path))
	},
}

// seededAt opens a seeded library in a folder of its own, at <temp>/data/library.sqlite.
func seededAt(t *testing.T, set settings) (*Library, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "library.sqlite")
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	lib := openLibraryAt(t, path, set)
	seed(t, lib)
	return lib, path
}

// A library deleted or moved while the application runs is a lost library: every change is refused
// as unavailable, and nothing is created in its place.
func TestALostLibraryRefusesEveryChangeAndCreatesNothing(t *testing.T) {
	for name, lose := range losses {
		t.Run(name, func(t *testing.T) {
			lib, path := seededAt(t, defaults)
			parent := lose(t, path)
			before := names(t, parent)
			for method, write := range writes(t) {
				if problem := Problem(write(deadline(t, 5*time.Second), lib)); problem == nil || problem.Code != contracts.CodeLibraryUnavailable {
					t.Errorf("%s of a library %s = %v, want library.unavailable", method, name, problem)
				}
			}
			if _, err := lib.Torrents(deadline(t, 5*time.Second)); err == nil || Problem(err).Code != contracts.CodeLibraryUnavailable {
				t.Errorf("Torrents of a library %s = %v, want library.unavailable", name, err)
			}
			if got := names(t, parent); !reflect.DeepEqual(got, before) {
				t.Fatalf("the folder holds %v after the refused changes, want %v", got, before)
			}
		})
	}
}

// A library lost while a change is being committed is never reported as saved: SQLite commits into
// the deleted or moved file without an error, but the library at its path does not hold the change.
func TestALibraryLostWhileAChangeCommitsIsNeverConfirmed(t *testing.T) {
	for loss, lose := range losses {
		for method, write := range writes(t) {
			t.Run(loss+"/"+method, func(t *testing.T) {
				trigger := &armed{}
				set := defaults
				set.beforeCommit = trigger.beforeCommit
				lib, path := seededAt(t, set)
				trigger.fn, trigger.on = func() { lose(t, path) }, true
				err := write(deadline(t, 5*time.Second), lib)
				if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, ErrReplaced) {
					t.Fatalf("%s of a library %s at its commit = %v, want ErrOutcomeUnknown and ErrReplaced", method, loss, err)
				}
				if problem := Problem(err); problem.Code != contracts.CodeLibraryOutcomeUnknown {
					t.Fatalf("%s of a library %s at its commit maps to %v, want library.outcomeUnknown", method, loss, problem)
				}
			})
		}
	}
}

// Using a library after Close is a bug of the quit order: no code of the library's own.
func TestAWriteAfterCloseIsUnexpected(t *testing.T) {
	lib, _ := openLibrary(t)
	seed(t, lib)
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if problem := Problem(lib.SetManualPaused(deadline(t, 5*time.Second), idA, true)); problem.Code != contracts.CodeInternalUnexpected {
		t.Fatalf("a write after Close = %v, want internal.unexpected", problem)
	}
}

// Torrents imported, paused, selected and the settings saved at once, while readers read: every
// snapshot is whole, one hash is stored once, and every change that returned nil survives a reopen.
func TestConcurrentChangesNeitherDuplicateNorLoseAnything(t *testing.T) {
	lib, path := openLibrary(t)
	const workers, rounds, groupFiles = 8, 6, 4
	ctx := deadline(t, time.Minute)
	paused := make([]Torrent, workers)
	for i := range paused {
		paused[i] = sampleTorrent(newID(t), byte(0x10+i))
		add(t, lib, paused[i])
	}
	selected := sampleTorrent(newID(t), 0x01)
	selected.Files = nil
	for i := range workers * groupFiles {
		selected.Files = append(selected.Files, TorrentFile{Path: "Many/" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Size: int64(i), Wanted: true})
	}
	add(t, lib, selected)
	fileCount := map[contracts.TorrentID]int{selected.ID: len(selected.Files)}

	var wg sync.WaitGroup
	shared := make([]error, workers)
	distinct := make([]Torrent, workers)
	failures := make(chan error, 8*workers*rounds)
	for i := range workers {
		distinct[i] = sampleTorrent(newID(t), byte(0x60+i))
		wg.Go(func() {
			shared[i] = lib.AddTorrent(ctx, sampleTorrent(newID(t), 0x5a), importIntent(newID(t), newID(t)))
		})
		wg.Go(func() {
			if err := lib.AddTorrent(ctx, distinct[i], importIntent(newID(t), newID(t))); err != nil {
				failures <- err
			}
		})
		wg.Go(func() {
			for r := range rounds {
				if err := lib.SetManualPaused(ctx, paused[i].ID, r%2 == 0); err != nil {
					failures <- err
				}
			}
		})
		wg.Go(func() {
			group := make([]int, groupFiles)
			for k := range group {
				group[k] = i*groupFiles + k
			}
			for r := range rounds {
				if err := lib.SetFilesWanted(ctx, selected.ID, group, r%2 == 1); err != nil {
					failures <- err
				}
			}
		})
	}
	saved := make([]contracts.Settings, rounds)
	wg.Go(func() {
		for r := range rounds {
			folder := "/Users/owner/Round " + string(rune('A'+r))
			saved[r] = contracts.Settings{DefaultSavePath: &folder, Theme: contracts.ThemeLight, Language: "fr", GlobalUploadDisabled: r%2 == 0}
			if err := lib.SaveSettings(ctx, saved[r]); err != nil {
				failures <- err
			}
		}
	})
	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				all, err := lib.Torrents(ctx)
				if err != nil {
					failures <- err
					return
				}
				for _, torrent := range all {
					if want, ok := fileCount[torrent.ID]; ok && len(torrent.Files) != want || !ok && len(torrent.Files) != 3 {
						failures <- errors.New("a reader saw torrent " + string(torrent.ID) + " without all its files")
					}
				}
			}
		})
	}
	wg.Wait()
	close(done)
	readers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	stored := 0
	for _, err := range shared {
		if err == nil {
			stored++
		} else if !errors.Is(err, ErrDuplicate) {
			t.Errorf("a concurrent add of the shared hash = %v, want nil or ErrDuplicate", err)
		}
	}
	if stored != 1 {
		t.Fatalf("%d adds of the shared hash were stored, want 1", stored)
	}

	lib = reopen(t, lib, path)
	all := torrents(t, lib)
	if len(all) != 2*workers+2 {
		t.Fatalf("%d records after a reopen, want %d", len(all), 2*workers+2)
	}
	byID := map[contracts.TorrentID]Torrent{}
	for _, torrent := range all {
		byID[torrent.ID] = torrent
	}
	lastPaused := (rounds-1)%2 == 0
	lastWanted := (rounds-1)%2 == 1
	for i := range workers {
		if got := byID[paused[i].ID].ManualPaused; got != lastPaused {
			t.Errorf("torrent %d paused = %v, want the last change %v", i, got, lastPaused)
		}
		if _, ok := byID[distinct[i].ID]; !ok {
			t.Errorf("the import of distinct torrent %d is lost", i)
		}
	}
	for i, f := range byID[selected.ID].Files {
		if f.Wanted != lastWanted {
			t.Errorf("file %d wanted = %v, want the last change %v", i, f.Wanted, lastWanted)
		}
	}
	if got, _ := settingsOf(t, lib); !reflect.DeepEqual(got, saved[rounds-1]) {
		t.Errorf("settings after a reopen = %+v, want the last save %+v", got, saved[rounds-1])
	}
}

// Another program changing the library's tables while it is open (a file edited by hand, a broken
// tool) makes a repository fail with SQLite's or the decoder's error; none returns success or
// panics. This reaches every error branch a valid file never takes.
func TestATableChangedBehindTheLibrarysBackIsReportedNotHidden(t *testing.T) {
	torrentsOnly := []string{"DROP TABLE payload_files", "DROP TABLE payload_records", "DROP TABLE torrent_files", "DROP TABLE torrents"}
	looseTorrents := "CREATE TABLE torrents(id, infohash_v1, infohash_v2, name, info, source, source_metainfo, source_magnet, save_path, manual_paused, added_at)"
	for name, c := range map[string]struct {
		change []string
		call   func(context.Context, *Library) error
	}{
		"the journal dropped, before an import": {
			[]string{"DROP TABLE operation_torrents", "DROP TABLE operations"},
			func(ctx context.Context, l *Library) error {
				return l.AddTorrent(ctx, sampleTorrent(idB, 2), importIntent(newID(t), newID(t)))
			},
		},
		"a column of the torrents renamed, before an import": {
			[]string{"ALTER TABLE torrents RENAME COLUMN name TO title"},
			func(ctx context.Context, l *Library) error {
				return l.AddTorrent(ctx, sampleTorrent(idB, 2), importIntent(newID(t), newID(t)))
			},
		},
		"a trigger refusing a file, during an import": {
			[]string{"CREATE TRIGGER refuse BEFORE INSERT ON torrent_files BEGIN SELECT RAISE(ABORT, 'refused'); END"},
			func(ctx context.Context, l *Library) error {
				return l.AddTorrent(ctx, sampleTorrent(idB, 2), importIntent(newID(t), newID(t)))
			},
		},
		"the pause column renamed": {
			[]string{"ALTER TABLE torrents RENAME COLUMN manual_paused TO paused"},
			func(ctx context.Context, l *Library) error { return l.SetManualPaused(ctx, idA, true) },
		},
		"the wanted column renamed": {
			[]string{"ALTER TABLE torrent_files RENAME COLUMN wanted TO selected"},
			func(ctx context.Context, l *Library) error { return l.SetFilesWanted(ctx, idA, []int{0}, false) },
		},
		"the torrents renamed, and an index the torrent lacks": {
			[]string{"ALTER TABLE torrents RENAME TO torrents_before"},
			func(ctx context.Context, l *Library) error { return l.SetFilesWanted(ctx, idA, []int{0, 9}, false) },
		},
		"the torrents dropped, before a read": {
			torrentsOnly,
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"a torrent without a name": {
			append(slices.Clone(torrentsOnly), looseTorrents,
				"INSERT INTO torrents VALUES ('"+idA+"', NULL, NULL, NULL, x'00', 'file', NULL, NULL, '/x', 0, 1)"),
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"a torrent with a short infohash": {
			append(slices.Clone(torrentsOnly), looseTorrents,
				"INSERT INTO torrents VALUES ('"+idA+"', x'0102', NULL, 'A', x'00', 'file', NULL, NULL, '/x', 0, 1)"),
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"a file without a path": {
			[]string{"DROP TABLE payload_files", "DROP TABLE torrent_files",
				"CREATE TABLE torrent_files(torrent_id, file_index, path, size, wanted)",
				"INSERT INTO torrent_files VALUES ('" + idA + "', 0, NULL, 1, 1)"},
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"a file of no torrent": {
			[]string{"DROP TABLE payload_files", "DROP TABLE torrent_files",
				"CREATE TABLE torrent_files(torrent_id, file_index, path, size, wanted)",
				"INSERT INTO torrent_files VALUES ('" + idB + "', 0, 'b', 1, 1)"},
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"a gap in a torrent's file indexes": {
			[]string{"DELETE FROM payload_files", "DELETE FROM torrent_files WHERE torrent_id = '" + idA + "' AND file_index = 1"},
			func(ctx context.Context, l *Library) error { _, err := l.Torrents(ctx); return err },
		},
		"the settings dropped": {
			[]string{"DROP TABLE settings"},
			func(ctx context.Context, l *Library) error { _, _, err := l.Settings(ctx); return err },
		},
		"the journal dropped, before a finish": {
			[]string{"DROP TABLE operation_torrents", "DROP TABLE operations"},
			func(ctx context.Context, l *Library) error {
				return l.FinishOperation(ctx, opA, OperationEnd{State: contracts.OperationSucceeded, FinishedAt: time.Now()})
			},
		},
		"the journal's torrents dropped, before a finish with an outcome": {
			[]string{"DROP TABLE operation_torrents"},
			func(ctx context.Context, l *Library) error {
				return l.FinishOperation(ctx, opA, OperationEnd{State: contracts.OperationSucceeded, FinishedAt: time.Now(),
					Outcomes: map[contracts.TorrentID]*contracts.AppError{idA: nil}})
			},
		},
		"the journal dropped, before a read": {
			[]string{"DROP TABLE operation_torrents", "DROP TABLE operations"},
			func(ctx context.Context, l *Library) error { _, _, err := l.Operation(ctx, opA); return err },
		},
		"an operation's error that is not an AppError": {
			[]string{"UPDATE operations SET state = 'failed', error = '[1]', finished_at = 1 WHERE operation_id = '" + opA + "'"},
			func(ctx context.Context, l *Library) error { _, _, err := l.Operation(ctx, opA); return err },
		},
		"a torrent's outcome that is not an AppError": {
			[]string{"UPDATE operation_torrents SET outcome = '[1]'"},
			func(ctx context.Context, l *Library) error { _, _, err := l.Operation(ctx, opA); return err },
		},
		"a journal torrent without an ID": {
			[]string{"DROP TABLE operation_torrents", "CREATE TABLE operation_torrents(operation_id, torrent_id, outcome)",
				"INSERT INTO operation_torrents VALUES ('" + opA + "', NULL, NULL)"},
			func(ctx context.Context, l *Library) error { _, _, err := l.Operation(ctx, opA); return err },
		},
	} {
		t.Run(name, func(t *testing.T) {
			lib, _ := openLibrary(t)
			seed(t, lib)
			other := behind(t, lib.store)
			for _, statement := range c.change {
				must(t, other, statement)
			}
			before := contents(t, other)
			err := c.call(deadline(t, 5*time.Second), lib)
			if err == nil {
				t.Fatal("the changed table went unnoticed")
			}
			if problem := Problem(err); problem.Code != contracts.CodeInternalUnexpected {
				t.Errorf("%v maps to %v, want internal.unexpected", err, problem)
			}
			if after := contents(t, other); after != before {
				t.Errorf("the failed call changed the tables:\n%s\nwant\n%s", after, before)
			}
		})
	}
}

// contents is every row of every table of the file, as text, read through s.
func contents(t *testing.T, s *store) string {
	t.Helper()
	var out strings.Builder
	err := s.Read(deadline(t, 5*time.Second), func(tx txn) error {
		var tables []string
		if err := queryEach(tx, func(rows *sql.Rows) error {
			var name string
			tables = append(tables, name)
			return rows.Scan(&tables[len(tables)-1])
		}, "SELECT name FROM sqlite_schema WHERE type = 'table' ORDER BY name"); err != nil {
			return err
		}
		for _, table := range tables {
			var lines []string
			if err := queryEach(tx, func(rows *sql.Rows) error {
				columns, _ := rows.Columns() // rows that scan have columns
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					return err
				}
				lines = append(lines, fmt.Sprintf("  %v\n", values))
				return nil
			}, `SELECT * FROM "`+table+`"`); err != nil {
				return err
			}
			// Some tables have no rowid; the rows are compared as a set.
			slices.Sort(lines)
			fmt.Fprintf(&out, "%s:\n%s", table, strings.Join(lines, ""))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.String()
}
