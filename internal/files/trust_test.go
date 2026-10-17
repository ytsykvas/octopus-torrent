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

// cleanRecord is a clean quit with three files over 10 pieces: a [0,4), b [4,7), c [7,10).
func cleanRecord() *Record {
	return &Record{Clean: true, FileSystem: "apfs", Dev: 42, Files: []FileState{
		{Path: "/Downloads/t/a", Present: true, Fingerprint: fp(100, old, old), FirstPiece: 0, EndPiece: 4},
		{Path: "/Downloads/t/b", Present: true, Fingerprint: fp(200, old, old), FirstPiece: 4, EndPiece: 7},
		{Path: "/Downloads/t/c", Present: false, FirstPiece: 7, EndPiece: 10},
	}}
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
	d := Decide(cleanRecord(), root, 10, unchanged())
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
		d := Decide(rec, root, 10, unchanged())
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
		if d := Decide(rec, root, 10, probe); !d.VerifyAll || d.Reason == "" {
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
		d := Decide(cleanRecord(), root, 10, probe)
		if d.VerifyAll || len(d.Missing) != 0 || !slices.Equal(d.VerifyPieces, tc.want) {
			t.Errorf("%s: Decide = %+v, want pieces %v only", name, d, tc.want)
		}
		for _, f := range cleanRecord().Files {
			if !slices.Contains(d.Trusted, f.Path) {
				continue
			}
			for piece := f.FirstPiece; piece < f.EndPiece; piece++ {
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
	d := Decide(cleanRecord(), root, 10, probe)
	if d.VerifyAll || !slices.Equal(d.Missing, []string{"/Downloads/t/b"}) || len(d.VerifyPieces) != 0 {
		t.Fatalf("Decide = %+v, want exactly b missing", d)
	}
}

func TestAFileNeverWrittenIsNotMissing(t *testing.T) {
	// c is absent in the record and absent now: a file of a partial download
	d := Decide(cleanRecord(), root, 10, unchanged())
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
		if d := Decide(rec, root, 10, probe); !d.VerifyAll || d.Reason == "" {
			t.Errorf("%s: Decide = %+v, want VerifyAll", fsType, d)
		}
	}
}

func TestRangesAreMergedSortedAndKeptInsideTheTorrent(t *testing.T) {
	rec := &Record{Clean: true, FileSystem: "apfs", Dev: 42, Files: []FileState{
		{Path: "/Downloads/x", Present: true, Fingerprint: fp(1, old, old), FirstPiece: 3, EndPiece: 6},
		{Path: "/Downloads/y", Present: true, Fingerprint: fp(1, old, old), FirstPiece: 5, EndPiece: 8},
		{Path: "/Downloads/z", Present: true, Fingerprint: fp(1, old, old), FirstPiece: 8, EndPiece: 8},  // empty file
		{Path: "/Downloads/w", Present: true, Fingerprint: fp(1, old, old), FirstPiece: 9, EndPiece: 14}, // past the end
	}}
	probe := fakeProbe{fsType: "apfs", stats: map[string]Fingerprint{
		root:           {Dev: 42},
		"/Downloads/x": fp(2, old, old), "/Downloads/y": fp(2, old, old),
		"/Downloads/z": fp(2, old, old), "/Downloads/w": fp(2, old, old),
	}}
	d := Decide(rec, root, 10, probe)
	if want := []int{3, 4, 5, 6, 7, 9}; !slices.Equal(d.VerifyPieces, want) {
		t.Fatalf("VerifyPieces = %v, want %v", d.VerifyPieces, want)
	}
}
