package library

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
)

// newID is a fresh version 4 UUID.
func newID(t *testing.T) string {
	t.Helper()
	id, err := contracts.NewUUID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// infoOf is the info dictionary of sample torrent n: another n, other bytes and other infohashes.
func infoOf(n byte) []byte { return []byte("d4:name6:Sample4:salti" + strconv.Itoa(int(n)) + "ee") }

// v1Of and v2Of are the infohashes of info.
func v1Of(info []byte) *[20]byte { h := sha1.Sum(info); return &h }
func v2Of(info []byte) *[32]byte { h := sha256.Sum256(info); return &h }

// sampleTorrent is a valid record of v1 torrent n with three files.
func sampleTorrent(id string, n byte) Torrent {
	return Torrent{
		ID:             contracts.TorrentID(id),
		InfohashV1:     v1Of(infoOf(n)),
		Name:           "Sample",
		Info:           infoOf(n),
		Source:         contracts.SourceFile,
		SourceMetainfo: []byte("d4:infod4:name6:Sampleee"),
		SavePath:       "/Users/owner/Downloads",
		AddedAt:        time.Date(2026, 11, 4, 12, 0, 0, 123456789, time.UTC),
		Files: []TorrentFile{
			{Path: "Sample/a.txt", Size: 1, Wanted: true},
			{Path: "Sample/b.txt", Size: 2, Wanted: true},
			{Path: "Sample/c.txt", Size: 3, Wanted: true},
		},
	}
}

// importIntent is the journal entry of a confirmed import of the pending import preview.
func importIntent(op, preview string) OperationIntent {
	return OperationIntent{
		ID:        contracts.OperationID(op),
		RuntimeID: runA,
		Kind:      contracts.OperationConfirmImport,
		Request: &contracts.ConfirmImportRequest{
			OperationID: contracts.OperationID(op), PendingImportID: contracts.PendingImportID(preview),
			PreviewVersion: 1, SelectedFileIndexes: []int{0, 1, 2}, Destination: "/Users/owner/Downloads",
		},
		AcceptedAt: time.Date(2026, 11, 4, 11, 59, 59, 1, time.UTC),
	}
}

// reopen closes lib and opens the file again, so a read proves what reached the disk.
func reopen(t *testing.T, lib *Library, path string) *Library {
	t.Helper()
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := OpenLibrary(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatalf("OpenLibrary again: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	return again
}

// add stores a torrent with a fresh import operation, failing the test on an error.
func add(t *testing.T, lib *Library, torrent Torrent) {
	t.Helper()
	if err := lib.AddTorrent(deadline(t, 5*time.Second), torrent, importIntent(newID(t), newID(t))); err != nil {
		t.Fatalf("AddTorrent(%s): %v", torrent.ID, err)
	}
}

// torrents reads every record, failing the test on an error.
func torrents(t *testing.T, lib *Library) []Torrent {
	t.Helper()
	all, err := lib.Torrents(deadline(t, 5*time.Second))
	if err != nil {
		t.Fatalf("Torrents: %v", err)
	}
	return all
}

// sameTorrents compares records; times must be the same instant, and in UTC when read back.
func sameTorrents(t *testing.T, got, want []Torrent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d torrents, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		g, w := got[i], want[i]
		if !g.AddedAt.Equal(w.AddedAt) || g.AddedAt.Location() != time.UTC {
			t.Errorf("torrent %d added at %v, want %v in UTC", i, g.AddedAt, w.AddedAt)
		}
		g.AddedAt, w.AddedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("torrent %d =\n%+v\nwant\n%+v", i, g, w)
		}
	}
}

func TestAnAddedTorrentIsReadBackExactlyAfterAReopen(t *testing.T) {
	lib, path := openLibrary(t)
	hybrid := sampleTorrent(idA, 0xa1)
	hybrid.Name = "Ünïcødé — 日本語 \x00 and a NUL"
	hybrid.Info = []byte{0, 1, 2, 0, 0xff, 0}
	hybrid.InfohashV1, hybrid.InfohashV2 = v1Of(hybrid.Info), v2Of(hybrid.Info)
	hybrid.ManualPaused = true
	hybrid.SavePath = "/Volumes/Données/Torrents ☂"
	hybrid.AddedAt = time.Date(2262, 1, 1, 0, 0, 0, 999999999, time.FixedZone("UTC+5", 5*3600))
	hybrid.Files = []TorrentFile{
		{Path: "Ünïcødé/большой.bin", Size: math.MaxInt64, Wanted: false},
		{Path: "Ünïcødé/empty", Size: 0, Wanted: true},
	}
	magnet := sampleTorrent(idB, 0xc3)
	magnet.Source, magnet.SourceMetainfo = contracts.SourceMagnet, nil
	magnet.SourceMagnet = "magnet:?xt=urn:btih:c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3c3"
	magnet.AddedAt = time.Date(1678, 1, 1, 0, 0, 0, 0, time.UTC)
	add(t, lib, hybrid)
	add(t, lib, magnet)
	// Ordered by the time they were added: the magnet's is the earlier.
	want := []Torrent{magnet, hybrid}
	sameTorrents(t, torrents(t, lib), want)
	sameTorrents(t, torrents(t, reopen(t, lib, path)), want)
}

func TestAnAbsentSourceIsStoredAsNull(t *testing.T) {
	lib, _ := openLibrary(t)
	torrent := sampleTorrent(idA, 1)
	torrent.SourceMetainfo = []byte{} // empty, not nil: still absent
	add(t, lib, torrent)
	var metainfo, magnet string
	if err := lib.store.Read(deadline(t, 5*time.Second), func(tx txn) error {
		return tx.QueryRow("SELECT typeof(source_metainfo), typeof(source_magnet) FROM torrents").Scan(&metainfo, &magnet)
	}); err != nil {
		t.Fatal(err)
	}
	if metainfo != "null" || magnet != "null" {
		t.Fatalf("an absent source is stored as %s and %s, want null and null", metainfo, magnet)
	}
	if got := torrents(t, lib)[0]; got.SourceMetainfo != nil || got.SourceMagnet != "" {
		t.Fatalf("an absent source reads back as %q and %q", got.SourceMetainfo, got.SourceMagnet)
	}
}

func TestAnInvalidTorrentOrIntentIsRefusedAndWritesNothing(t *testing.T) {
	lib, _ := openLibrary(t)
	empty := rowCounts(t, lib.store)
	for name, change := range map[string]func(*Torrent, *OperationIntent){
		"an ID that is not a UUID":       func(tr *Torrent, _ *OperationIntent) { tr.ID = "not-a-uuid" },
		"an upper-case ID":               func(tr *Torrent, _ *OperationIntent) { tr.ID = "3F2A7C1E-5B4D-4E8F-9A6B-1C2D3E4F5A6B" },
		"no infohash":                    func(tr *Torrent, _ *OperationIntent) { tr.InfohashV1 = nil },
		"no info":                        func(tr *Torrent, _ *OperationIntent) { tr.Info = nil },
		"a v1 hash of other info":        func(tr *Torrent, _ *OperationIntent) { tr.InfohashV1 = v1Of(infoOf(2)) },
		"a v2 hash of other info":        func(tr *Torrent, _ *OperationIntent) { tr.InfohashV2 = v2Of(infoOf(2)) },
		"a source outside the contract":  func(tr *Torrent, _ *OperationIntent) { tr.Source = "email" },
		"a relative save path":           func(tr *Torrent, _ *OperationIntent) { tr.SavePath = "Downloads" },
		"the root as the save path":      func(tr *Torrent, _ *OperationIntent) { tr.SavePath = "/" },
		"an unclean save path":           func(tr *Torrent, _ *OperationIntent) { tr.SavePath = "/Users/owner/../owner" },
		"no files":                       func(tr *Torrent, _ *OperationIntent) { tr.Files = nil },
		"an empty file path":             func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "" },
		"a file path with a NUL":         func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "\x00a" },
		"a repeated file path":           func(tr *Torrent, _ *OperationIntent) { tr.Files[2].Path = tr.Files[0].Path },
		"an absolute file path":          func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "/etc/passwd" },
		"a file path leaving the folder": func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "../../etc/passwd" },
		"a file path through a parent":   func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "Sample/../b.txt" },
		"the save folder as a file":      func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "." },
		"a doubled separator":            func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "Sample//b.txt" },
		"a file path ending in a slash":  func(tr *Torrent, _ *OperationIntent) { tr.Files[1].Path = "Sample/" },
		"a negative size":                func(tr *Torrent, _ *OperationIntent) { tr.Files[0].Size = -1 },
		"no time":                        func(tr *Torrent, _ *OperationIntent) { tr.AddedAt = time.Time{} },
		"a time before 1678":             func(tr *Torrent, _ *OperationIntent) { tr.AddedAt = time.Date(1677, 1, 1, 0, 0, 0, 0, time.UTC) },
		"an operation ID not a UUID":     func(_ *Torrent, op *OperationIntent) { op.ID = "op" },
		"a runtime ID not a UUID":        func(_ *Torrent, op *OperationIntent) { op.RuntimeID = "" },
		"a kind outside the contract":    func(_ *Torrent, op *OperationIntent) { op.Kind = "relocateData" },
		"no request":                     func(_ *Torrent, op *OperationIntent) { op.Request = nil },
		"a nil request pointer":          func(_ *Torrent, op *OperationIntent) { op.Request = (*contracts.ConfirmImportRequest)(nil) },
		"an acceptance time after 2262":  func(_ *Torrent, op *OperationIntent) { op.AcceptedAt = time.Date(2263, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		torrent, op := sampleTorrent(idA, 1), importIntent(opA, newID(t))
		change(&torrent, &op)
		if err := lib.AddTorrent(deadline(t, 5*time.Second), torrent, op); !errors.Is(err, ErrInvalid) {
			t.Errorf("AddTorrent with %s = %v, want ErrInvalid", name, err)
		}
	}
	if got := rowCounts(t, lib.store); !mapsEqual(got, empty) {
		t.Fatalf("refused adds left rows: %v", got)
	}
}

func TestADuplicateNamesTheExistingRecordAndAddsNothing(t *testing.T) {
	lib, _ := openLibrary(t)
	// A record of one hash of an info dictionary: v1 only, or v2 only.
	record := func(id string, n byte, v1 bool) {
		t.Helper()
		torrent := sampleTorrent(id, n)
		if !v1 {
			torrent.InfohashV1, torrent.InfohashV2 = nil, v2Of(torrent.Info)
		}
		add(t, lib, torrent)
	}
	both := newID(t) // the v1 record of info 0xc3, whose v2 record is another one
	record(idA, 0xa1, true)
	record(idB, 0xb2, false)
	record(both, 0xc3, true)
	record(newID(t), 0xc3, false)
	before := rowCounts(t, lib.store)
	for name, c := range map[string]struct {
		info   byte
		v1, v2 bool
		want   contracts.TorrentID
	}{
		"the same v1 hash":                       {0xa1, true, false, idA},
		"the same v2 hash":                       {0xb2, false, true, idB},
		"a hybrid whose v1 hash is the record's": {0xa1, true, true, idA},
		"a hybrid whose v2 hash is the record's": {0xb2, true, true, idB},
		"a hybrid matching two records":          {0xc3, true, true, contracts.TorrentID(both)},
	} {
		torrent := sampleTorrent(newID(t), c.info)
		torrent.InfohashV1, torrent.InfohashV2 = nil, nil
		if c.v1 {
			torrent.InfohashV1 = v1Of(torrent.Info)
		}
		if c.v2 {
			torrent.InfohashV2 = v2Of(torrent.Info)
		}
		err := lib.AddTorrent(deadline(t, 5*time.Second), torrent, importIntent(newID(t), newID(t)))
		var dup *DuplicateError
		if !errors.Is(err, ErrDuplicate) || !errors.As(err, &dup) || dup.Existing != c.want {
			t.Errorf("AddTorrent with %s = %v, want ErrDuplicate naming %s", name, err, c.want)
		}
	}
	if got := rowCounts(t, lib.store); !mapsEqual(got, before) {
		t.Fatalf("refused duplicates changed the rows: %v, want %v", got, before)
	}
}

// Concurrent imports of one torrent: exactly one record, and every other import learns which.
func TestConcurrentAddsOfOneHashCommitExactlyOnce(t *testing.T) {
	lib, _ := openLibrary(t)
	const adders = 8
	ids := make([]string, adders)
	errs := make([]error, adders)
	var wg sync.WaitGroup
	for i := range adders {
		ids[i] = newID(t)
		torrent, op := sampleTorrent(ids[i], 0x5a), importIntent(newID(t), newID(t))
		wg.Go(func() { errs[i] = lib.AddTorrent(deadline(t, 30*time.Second), torrent, op) })
	}
	wg.Wait()
	all := torrents(t, lib)
	if len(all) != 1 {
		t.Fatalf("%d records of one hash, want 1", len(all))
	}
	for i, err := range errs {
		var dup *DuplicateError
		switch {
		case contracts.TorrentID(ids[i]) == all[0].ID && err != nil:
			t.Errorf("the stored add returned %v", err)
		case contracts.TorrentID(ids[i]) != all[0].ID && (!errors.As(err, &dup) || dup.Existing != all[0].ID):
			t.Errorf("a refused add returned %v, want ErrDuplicate naming %s", err, all[0].ID)
		}
	}
}

// Two libraries on one file are two writers (two instances of the application before 028's lock):
// one hash still commits once.
func TestTwoLibrariesOnOneFileAddOneHashOnce(t *testing.T) {
	lib, path := openLibrary(t)
	other, err := OpenLibrary(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, l := range []*Library{lib, other} {
		torrent, op := sampleTorrent(newID(t), 0x5a), importIntent(newID(t), newID(t))
		wg.Go(func() { errs[i] = l.AddTorrent(deadline(t, 30*time.Second), torrent, op) })
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) || !errors.Is(errors.Join(errs...), ErrDuplicate) {
		t.Fatalf("two libraries adding one hash = %v, want exactly one ErrDuplicate", errs)
	}
	if n := len(torrents(t, other)); n != 1 {
		t.Fatalf("%d records, want 1", n)
	}
}

func TestAReusedOperationIDIsRefusedAndToldApart(t *testing.T) {
	lib, _ := openLibrary(t)
	preview := newID(t)
	add := importIntent(opA, preview)
	if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idA, 1), add); err != nil {
		t.Fatal(err)
	}
	before := rowCounts(t, lib.store)
	other := importIntent(opA, newID(t)) // another preview: another request
	for name, c := range map[string]struct {
		op   OperationIntent
		same bool
	}{"the same request": {add, true}, "another request": {other, false}} {
		err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idB, 2), c.op)
		var exists *OperationExistsError
		if !errors.Is(err, ErrOperationExists) || !errors.As(err, &exists) || exists.ID != opA || exists.SameRequest != c.same {
			t.Errorf("AddTorrent with %s under a used operation ID = %v, want ErrOperationExists (same request %v)", name, err, c.same)
		}
	}
	if got := rowCounts(t, lib.store); !mapsEqual(got, before) {
		t.Fatalf("a refused operation changed the rows: %v", got)
	}
}

func TestPauseAndWantedAreStoredAndSurviveAReopen(t *testing.T) {
	lib, path := openLibrary(t)
	torrent := sampleTorrent(idA, 1)
	add(t, lib, torrent)
	ctx := deadline(t, 5*time.Second)
	if err := lib.SetManualPaused(ctx, idA, true); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetFilesWanted(ctx, idA, []int{2, 0, 2}, false); err != nil {
		t.Fatal(err)
	}
	torrent.ManualPaused = true
	torrent.Files[0].Wanted, torrent.Files[2].Wanted = false, false
	lib = reopen(t, lib, path)
	sameTorrents(t, torrents(t, lib), []Torrent{torrent})
	if err := lib.SetManualPaused(ctx, idA, false); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetFilesWanted(ctx, idA, []int{0}, true); err != nil {
		t.Fatal(err)
	}
	torrent.ManualPaused, torrent.Files[0].Wanted = false, true
	sameTorrents(t, torrents(t, reopen(t, lib, path)), []Torrent{torrent})
}

func TestChangesOfAnUnknownTorrentAreNotFound(t *testing.T) {
	lib, _ := openLibrary(t)
	add(t, lib, sampleTorrent(idA, 1))
	ctx := deadline(t, 5*time.Second)
	for name, err := range map[string]error{
		"SetManualPaused": lib.SetManualPaused(ctx, idB, true),
		"SetFilesWanted":  lib.SetFilesWanted(ctx, idB, []int{0}, false),
	} {
		var missing *NotFoundError
		if !errors.Is(err, ErrNotFound) || !errors.As(err, &missing) || missing.Torrent != idB {
			t.Errorf("%s of an unknown torrent = %v, want ErrNotFound naming it", name, err)
		}
	}
}

func TestAnInvalidChangeIsRefusedAndChangesNoFile(t *testing.T) {
	lib, _ := openLibrary(t)
	torrent := sampleTorrent(idA, 1)
	add(t, lib, torrent)
	ctx := deadline(t, 5*time.Second)
	for name, err := range map[string]error{
		"an index the torrent lacks":      lib.SetFilesWanted(ctx, idA, []int{0, 3}, false),
		"a negative index":                lib.SetFilesWanted(ctx, idA, []int{-1}, false),
		"no index":                        lib.SetFilesWanted(ctx, idA, nil, false),
		"a torrent ID that is not a UUID": lib.SetFilesWanted(ctx, "x", []int{0}, false),
		"a paused torrent ID not a UUID":  lib.SetManualPaused(ctx, "x", true),
	} {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("a change with %s = %v, want ErrInvalid", name, err)
		}
	}
	sameTorrents(t, torrents(t, lib), []Torrent{torrent})
}

// SetFilesWanted takes any number of indexes: a folder of a large torrent is one change, past
// SQLite's limit of 32766 bound parameters.
func TestAFolderOfManyFilesIsOneChange(t *testing.T) {
	lib, _ := openLibrary(t)
	torrent := sampleTorrent(idA, 1)
	torrent.Files = nil
	indexes := make([]int, 32767)
	for i := range indexes {
		torrent.Files = append(torrent.Files, TorrentFile{Path: "Sample/f" + strconv.Itoa(i), Size: 1, Wanted: true})
		indexes[i] = i
	}
	// Generous bounds: the race detector slows the transpiled SQLite many times over.
	if err := lib.AddTorrent(deadline(t, time.Minute), torrent, importIntent(opA, newID(t))); err != nil {
		t.Fatalf("AddTorrent of %d files: %v", len(indexes), err)
	}
	if err := lib.SetFilesWanted(deadline(t, time.Minute), idA, indexes, false); err != nil {
		t.Fatalf("SetFilesWanted of %d files: %v", len(indexes), err)
	}
	all, err := lib.Torrents(deadline(t, time.Minute))
	if err != nil || len(all) != 1 || len(all[0].Files) != len(indexes) {
		t.Fatalf("Torrents = %d records, %v", len(all), err)
	}
	if slices.ContainsFunc(all[0].Files, func(f TorrentFile) bool { return f.Wanted }) {
		t.Fatal("a file stayed wanted")
	}
}
