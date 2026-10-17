package files

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
)

// FileState is what a run knew about one payload file when it left: whether the file existed, and its
// fingerprint as the run itself last left it.
type FileState struct {
	Path        string
	Present     bool
	Fingerprint Fingerprint
}

// FileRange is where a file of the torrent is and which pieces it covers: [First, End). It comes from
// the torrent, never from a stored record.
type FileRange struct {
	Path       string
	First, End int
}

// Record is what a run leaves for the next one. Clean says it ended without a storage problem, after the
// payload was synced. Task 029 keeps it in the library.
type Record struct {
	Clean      bool
	FileSystem string // type of the file system of the save folder
	Dev        uint64 // device of the save folder
	Files      []FileState
}

// Decision is what to do with the marks of a torrent at startup.
type Decision struct {
	// VerifyAll says no mark is trusted: the engine hashes every piece before it believes it.
	VerifyAll bool
	Reason    string
	// VerifyPieces are the pieces to hash when VerifyAll is false; every other mark is trusted.
	VerifyPieces []int
	// Trusted are the files that exist and are exactly as the last run left them, when VerifyAll is
	// false.
	Trusted []string
	// Missing are files the last run left that are gone. Nothing is added and nothing is
	// downloaded for them: the owner is told, and chooses a location (task 030).
	Missing []string
}

// Distrusted lists the pieces whose marks must not be trusted, for a torrent of the given number of
// pieces.
func (d Decision) Distrusted(pieces int) []int {
	if !d.VerifyAll {
		return d.VerifyPieces
	}
	all := make([]int, pieces)
	for i := range all {
		all[i] = i
	}
	return all
}

// trustedFileSystems keep nanosecond timestamps, a real change time and stable inodes. HFS+ keeps
// whole seconds, so an edit in the second of the last write leaves a fingerprint taken right after
// that write unchanged; exFAT makes up inodes and has no change time. Both are verified in full.
var trustedFileSystems = []string{"apfs"}

// Decide applies the rehash rule (task 009): marks are trusted only for files that are exactly as the
// last clean run left them. expected is the torrent's own layout, and pieces its piece count.
//
//  1. A file the record lists as present that is gone is Missing, whatever else is true. Nothing is
//     added and nothing is downloaded in its place.
//  2. No record, or the earlier run did not end cleanly: verify everything.
//  3. A record that lists a file the torrent does not have is another torrent's: verify everything.
//  4. A file system that cannot be trusted, another type than recorded, or another device: verify
//     everything. A replugged volume is not a missing file.
//  5. A file whose fingerprint differs, that the record does not know or says was absent, that is no
//     regular file, or that cannot be read: verify its pieces, from the torrent's layout.
//  6. Otherwise trust.
func Decide(rec *Record, saveRoot string, expected []FileRange, pieces int, probe Probe) Decision {
	if rec == nil {
		return Decision{VerifyAll: true, Reason: "there is no record of an earlier run"}
	}
	var d Decision
	for _, f := range rec.Files {
		if _, err := probe.Stat(f.Path); errors.Is(err, fs.ErrNotExist) && f.Present {
			d.Missing = append(d.Missing, f.Path)
		}
	}
	if reason := untrustworthy(rec, saveRoot, expected, probe); reason != "" {
		d.VerifyAll, d.Reason = true, reason
		return d
	}

	recorded := map[string]FileState{}
	for _, f := range rec.Files {
		recorded[f.Path] = f
	}
	verify := map[int]bool{}
	for _, want := range expected {
		f, known := recorded[want.Path]
		cur, err := probe.Stat(want.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue // never written, or missing and reported above
		case err != nil, !known, !f.Present, !cur.Regular, cur != f.Fingerprint:
		default:
			d.Trusted = append(d.Trusted, want.Path)
			continue
		}
		for p := max(want.First, 0); p < min(want.End, pieces); p++ {
			verify[p] = true
		}
	}
	for p := range verify {
		d.VerifyPieces = append(d.VerifyPieces, p)
	}
	slices.Sort(d.VerifyPieces)
	return d
}

// untrustworthy says why no mark may be believed at all, or "".
func untrustworthy(rec *Record, saveRoot string, expected []FileRange, probe Probe) string {
	if !rec.Clean {
		return "the earlier run did not end cleanly"
	}
	for _, f := range rec.Files {
		if !slices.ContainsFunc(expected, func(e FileRange) bool { return e.Path == f.Path }) {
			return "the record describes files this torrent does not have"
		}
	}
	fsType, err := probe.FileSystem(saveRoot)
	if err != nil {
		return fmt.Sprintf("the file system of the save folder cannot be determined: %v", err)
	}
	if !slices.Contains(trustedFileSystems, fsType) {
		return fmt.Sprintf("the file system %q does not keep reliable timestamps and inodes", fsType)
	}
	if fsType != rec.FileSystem {
		return fmt.Sprintf("the save folder is on %q, it was on %q", fsType, rec.FileSystem)
	}
	root, err := probe.Stat(saveRoot)
	if err != nil {
		return fmt.Sprintf("the save folder cannot be read: %v", err)
	}
	if root.Dev != rec.Dev {
		return "the save folder is on another volume than before"
	}
	return ""
}
