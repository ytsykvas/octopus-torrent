package files

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
)

// FileState is what a run knew about one payload file when it left: whether the file existed, its
// fingerprint as the run itself last left it, and the pieces the file covers.
type FileState struct {
	Path        string
	Present     bool
	Fingerprint Fingerprint
	// FirstPiece and EndPiece bound the pieces the file covers: [FirstPiece, EndPiece).
	FirstPiece, EndPiece int
}

// Record is what a run leaves for the next one. Clean says it ended without a storage problem and
// after the payload was synced. Task 029 keeps it in the library.
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
// last clean run left them.
//
//  1. No record, or not a clean quit: verify everything.
//  2. A file system that cannot be trusted, another type than recorded, or another device: verify
//     everything. A replugged volume is not a missing file.
//  3. A file the record lists as present that is gone: Missing.
//  4. A file whose fingerprint differs, that appeared, that is no regular file, or that cannot be
//     read: verify its pieces.
//  5. Otherwise trust.
//
// pieces is the torrent's piece count; ranges are kept inside it.
func Decide(rec *Record, saveRoot string, pieces int, probe Probe) Decision {
	switch {
	case rec == nil:
		return Decision{VerifyAll: true, Reason: "there is no record of an earlier run"}
	case !rec.Clean:
		return Decision{VerifyAll: true, Reason: "the earlier run did not end cleanly"}
	}
	fsType, err := probe.FileSystem(saveRoot)
	if err != nil {
		return Decision{VerifyAll: true, Reason: fmt.Sprintf("the file system of the save folder cannot be determined: %v", err)}
	}
	if !slices.Contains(trustedFileSystems, fsType) {
		return Decision{VerifyAll: true, Reason: fmt.Sprintf("the file system %q does not keep reliable timestamps and inodes", fsType)}
	}
	if fsType != rec.FileSystem {
		return Decision{VerifyAll: true, Reason: fmt.Sprintf("the save folder is on %q, it was on %q", fsType, rec.FileSystem)}
	}
	root, err := probe.Stat(saveRoot)
	if err != nil {
		return Decision{VerifyAll: true, Reason: fmt.Sprintf("the save folder cannot be read: %v", err)}
	}
	if root.Dev != rec.Dev {
		return Decision{VerifyAll: true, Reason: "the save folder is on another volume than before"}
	}

	var d Decision
	verify := map[int]bool{}
	for _, f := range rec.Files {
		cur, err := probe.Stat(f.Path)
		switch {
		case errors.Is(err, fs.ErrNotExist) && f.Present:
			d.Missing = append(d.Missing, f.Path)
			continue
		case errors.Is(err, fs.ErrNotExist):
			continue // never written
		case err != nil, !f.Present, !cur.Regular, cur != f.Fingerprint:
		default:
			d.Trusted = append(d.Trusted, f.Path)
			continue
		}
		for p := max(f.FirstPiece, 0); p < min(f.EndPiece, pieces); p++ {
			verify[p] = true
		}
	}
	for p := range verify {
		d.VerifyPieces = append(d.VerifyPieces, p)
	}
	slices.Sort(d.VerifyPieces)
	return d
}
