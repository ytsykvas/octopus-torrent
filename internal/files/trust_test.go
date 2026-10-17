package files

import (
	"errors"
	"io/fs"
	"slices"
	"testing"
)

// fakeProbe is a test double for the disk: a file system type and the fingerprints it answers.
type fakeProbe struct {
	fsType  string
	fsErr   error
	stats   map[string]Fingerprint
	statErr map[string]error
}

func (f fakeProbe) Stat(path string) (Fingerprint, error) {
	if err := f.statErr[path]; err != nil {
		return Fingerprint{}, err
	}
	fp, ok := f.stats[path]
	if !ok {
		return Fingerprint{}, fs.ErrNotExist
	}
	return fp, nil
}

func (f fakeProbe) FileSystem(string) (string, error) { return f.fsType, f.fsErr }

const root = "/Downloads"

func fp(size, mtime, ctime int64) Fingerprint {
	return Fingerprint{Size: size, Mtime: mtime, Ctime: ctime, Inode: 7, Dev: 42, Regular: true}
}

// old is a modification time: the file was last written long ago.
const old = int64(1_700_000_000_000_000_000)

// layout is the torrent's three files over 10 pieces: a [0,4), b [4,7), c [7,10).
var layout = []FileRange{
	{Path: "/Downloads/t/a", First: 0, End: 4},
	{Path: "/Downloads/t/b", First: 4, End: 7},
	{Path: "/Downloads/t/c", First: 7, End: 10},
}

// cleanRecord is a clean quit that left a and b, and never wrote c.
func cleanRecord() *Record {
	return &Record{Clean: true, FileSystem: "apfs", Dev: 42, Files: []FileState{
		{Path: "/Downloads/t/a", Present: true, Fingerprint: fp(100, old, old)},
		{Path: "/Downloads/t/b", Present: true, Fingerprint: fp(200, old, old)},
		{Path: "/Downloads/t/c"},
	}}
}

func decide(rec *Record, probe Probe) Decision {
	return Decide(rec, root, layout, 10, probe)
}

// unchanged is a disk where everything is as the clean record left it.
func unchanged() fakeProbe {
	return fakeProbe{fsType: "apfs",
		stats: map[string]Fingerprint{
			root:             {Dev: 42, Regular: false},
			"/Downloads/t/a": fp(100, old, old),
			"/Downloads/t/b": fp(200, old, old),
		}}
}

func TestEverythingAsTheCleanQuitLeftItIsTrusted(t *testing.T) {
	d := decide(cleanRecord(), unchanged())
	if d.VerifyAll || len(d.VerifyPieces) != 0 || len(d.Missing) != 0 || d.Reason != "" {
		t.Fatalf("Decide = %+v, want trust", d)
	}
	if want := []string{"/Downloads/t/a", "/Downloads/t/b"}; !slices.Equal(d.Trusted, want) {
		t.Fatalf("Trusted = %v, want the two files that exist: %v", d.Trusted, want)
	}
	if got := d.Distrusted(10); len(got) != 0 {
		t.Fatalf("Distrusted = %v", got)
	}
}

func TestWithoutACleanQuitEverythingIsVerified(t *testing.T) {
	unclean := cleanRecord()
	unclean.Clean = false
	for name, rec := range map[string]*Record{"no record": nil, "an unclean quit": unclean} {
		d := decide(rec, unchanged())
		if !d.VerifyAll || d.Reason == "" {
			t.Errorf("%s: Decide = %+v, want VerifyAll with a reason", name, d)
		}
		if got := d.Distrusted(10); !slices.Equal(got, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}) {
			t.Errorf("%s: Distrusted = %v, want every piece", name, got)
		}
	}
}

func TestAFileSystemThatCannotBeTrustedForTimestampsAndInodesMeansEverythingIsVerified(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*fakeProbe, *Record)
	}{
		"exFAT":                {func(p *fakeProbe, _ *Record) { p.fsType = "exfat" }},
		"HFS+":                 {func(p *fakeProbe, r *Record) { p.fsType, r.FileSystem = "hfs", "hfs" }},
		"unknown":              {func(p *fakeProbe, _ *Record) { p.fsType = "" }},
		"not determinable":     {func(p *fakeProbe, _ *Record) { p.fsErr = errors.New("statfs failed") }},
		"another type":         {func(p *fakeProbe, r *Record) { r.FileSystem = "hfs" }},
		"another volume":       {func(p *fakeProbe, _ *Record) { p.stats[root] = Fingerprint{Dev: 43} }},
		"the folder is gone":   {func(p *fakeProbe, _ *Record) { delete(p.stats, root) }},
		"the folder is denied": {func(p *fakeProbe, _ *Record) { p.statErr = map[string]error{root: fs.ErrPermission} }},
	} {
		probe, rec := unchanged(), cleanRecord()
		tc.mutate(&probe, rec)
		if d := decide(rec, probe); !d.VerifyAll || d.Reason == "" {
			t.Errorf("%s: Decide = %+v, want VerifyAll", name, d)
		}
	}
}

func TestOnlyTheChangedFilesAreVerified(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*fakeProbe)
		want   []int
	}{
		"truncated":                 {func(p *fakeProbe) { p.stats["/Downloads/t/a"] = fp(50, old, old+1) }, []int{0, 1, 2, 3}},
		"rewritten, same size":      {func(p *fakeProbe) { p.stats["/Downloads/t/b"] = fp(200, old+1, old+1) }, []int{4, 5, 6}},
		"mtime restored, ctime not": {func(p *fakeProbe) { p.stats["/Downloads/t/b"] = fp(200, old, old+5) }, []int{4, 5, 6}},
		"replaced by another file": {func(p *fakeProbe) {
			other := fp(200, old, old)
			other.Inode = 99
			p.stats["/Downloads/t/b"] = other
		}, []int{4, 5, 6}},
		"became a folder": {func(p *fakeProbe) {
			dir := fp(200, old, old)
			dir.Regular = false
			p.stats["/Downloads/t/a"] = dir
		}, []int{0, 1, 2, 3}},
		"unreadable": {func(p *fakeProbe) { p.statErr = map[string]error{"/Downloads/t/a": fs.ErrPermission} }, []int{0, 1, 2, 3}},
		"appeared":   {func(p *fakeProbe) { p.stats["/Downloads/t/c"] = fp(10, old, old) }, []int{7, 8, 9}},
		"two files": {func(p *fakeProbe) {
			p.stats["/Downloads/t/a"] = fp(1, old, old)
			p.stats["/Downloads/t/c"] = fp(1, old, old)
		}, []int{0, 1, 2, 3, 7, 8, 9}},
	} {
		probe := unchanged()
		tc.change(&probe)
		d := decide(cleanRecord(), probe)
		if d.VerifyAll || len(d.Missing) != 0 || !slices.Equal(d.VerifyPieces, tc.want) {
			t.Errorf("%s: Decide = %+v, want pieces %v only", name, d, tc.want)
		}
		for _, f := range layout {
			if !slices.Contains(d.Trusted, f.Path) {
				continue
			}
			for piece := f.First; piece < f.End; piece++ {
				if slices.Contains(d.VerifyPieces, piece) {
					t.Errorf("%s: piece %d of the trusted file %s is also to be verified", name, piece, f.Path)
				}
			}
		}
		if got := d.Distrusted(10); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Distrusted = %v, want %v", name, got, tc.want)
		}
	}
}

func TestAFileThatWasThereAndIsGoneIsMissingNotVerified(t *testing.T) {
	probe := unchanged()
	delete(probe.stats, "/Downloads/t/b")
	d := decide(cleanRecord(), probe)
	if d.VerifyAll || !slices.Equal(d.Missing, []string{"/Downloads/t/b"}) || len(d.VerifyPieces) != 0 {
		t.Fatalf("Decide = %+v, want exactly b missing", d)
	}
}

func TestAFileNeverWrittenIsNotMissing(t *testing.T) {
	// c is absent in the record and absent now: a file of a partial download
	d := decide(cleanRecord(), unchanged())
	if len(d.Missing) != 0 || len(d.VerifyPieces) != 0 {
		t.Fatalf("Decide = %+v", d)
	}
}

func TestOnlyAPFSIsTrustedForTimestampsAndInodes(t *testing.T) {
	for _, fsType := range []string{"hfs", "exfat", "msdos", "smbfs", "nfs"} {
		rec := cleanRecord()
		rec.FileSystem = fsType
		probe := unchanged()
		probe.fsType = fsType
		if d := decide(rec, probe); !d.VerifyAll || d.Reason == "" {
			t.Errorf("%s: Decide = %+v, want VerifyAll", fsType, d)
		}
	}
}

func TestRangesAreMergedSortedAndKeptInsideTheTorrent(t *testing.T) {
	expected := []FileRange{
		{Path: "/Downloads/x", First: 3, End: 6},
		{Path: "/Downloads/y", First: 5, End: 8},
		{Path: "/Downloads/z", First: 8, End: 8},   // an empty file covers no piece
		{Path: "/Downloads/w", First: 9, End: 14},  // past the end of the torrent
		{Path: "/Downloads/v", First: -2, End: -1}, // before its start
	}
	var files []FileState
	stats := map[string]Fingerprint{root: {Dev: 42}}
	for _, f := range expected {
		files = append(files, FileState{Path: f.Path, Present: true, Fingerprint: fp(1, old, old)})
		stats[f.Path] = fp(2, old, old)
	}
	d := Decide(&Record{Clean: true, FileSystem: "apfs", Dev: 42, Files: files}, root, expected, 10, fakeProbe{fsType: "apfs", stats: stats})
	if want := []int{3, 4, 5, 6, 7, 9}; !slices.Equal(d.VerifyPieces, want) {
		t.Fatalf("VerifyPieces = %v, want %v", d.VerifyPieces, want)
	}
}

func TestTheRangesComeFromTheTorrentNotFromTheRecord(t *testing.T) {
	// a record that understates what a changed file covers must not shrink the verification
	probe := unchanged()
	probe.stats["/Downloads/t/a"] = fp(1, old, old)
	d := decide(cleanRecord(), probe)
	if !slices.Equal(d.VerifyPieces, []int{0, 1, 2, 3}) {
		t.Fatalf("VerifyPieces = %v, want the whole range of a from the layout", d.VerifyPieces)
	}
}

func TestAFileTheRecordDoesNotKnowIsVerifiedAndNeverTrusted(t *testing.T) {
	rec := cleanRecord()
	rec.Files = rec.Files[:1] // b and c are not in the record, though b exists
	d := decide(rec, unchanged())
	if d.VerifyAll || !slices.Equal(d.VerifyPieces, []int{4, 5, 6}) || slices.Contains(d.Trusted, "/Downloads/t/b") {
		t.Fatalf("Decide = %+v, want b verified and not trusted", d)
	}
	empty := &Record{Clean: true, FileSystem: "apfs", Dev: 42}
	if d := decide(empty, unchanged()); !slices.Equal(d.VerifyPieces, []int{0, 1, 2, 3, 4, 5, 6}) || len(d.Trusted) != 0 {
		t.Fatalf("an empty record trusted something: %+v", d)
	}
}

func TestARecordOfAnotherTorrentIsNeverTrusted(t *testing.T) {
	rec := cleanRecord()
	rec.Files = append(rec.Files, FileState{Path: "/Downloads/other/file", Present: true, Fingerprint: fp(1, old, old)})
	if d := decide(rec, unchanged()); !d.VerifyAll || d.Reason == "" {
		t.Fatalf("Decide = %+v, want everything verified", d)
	}
}

func TestAMovedFileIsMissingEvenAfterACrash(t *testing.T) {
	probe := unchanged()
	delete(probe.stats, "/Downloads/t/b")
	crashed := cleanRecord()
	crashed.Clean = false
	d := decide(crashed, probe)
	if !d.VerifyAll || !slices.Equal(d.Missing, []string{"/Downloads/t/b"}) {
		t.Fatalf("Decide = %+v, want everything verified and b missing: nothing may be downloaded in its place", d)
	}
	other := unchanged()
	delete(other.stats, "/Downloads/t/b")
	other.fsType = "exfat"
	if d := decide(cleanRecord(), other); !d.VerifyAll || !slices.Equal(d.Missing, []string{"/Downloads/t/b"}) {
		t.Fatalf("on another file system Decide = %+v, want b missing too", d)
	}
	if d := decide(nil, probe); !d.VerifyAll || len(d.Missing) != 0 {
		t.Fatalf("with no record nothing is known to be missing: %+v", d)
	}
}
