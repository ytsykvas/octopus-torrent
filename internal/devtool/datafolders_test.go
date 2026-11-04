package devtool

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOwnerDataDescribesTheFolderAndEveryEntryButFinderMetadata(t *testing.T) {
	sys := healthyRepo()
	got, err := ownerData(sys)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		ownerFolder:                     "folder modified " + time.Unix(100, 0).UTC().Format(time.RFC3339Nano),
		ownerFolder + "/library.sqlite": "4096 bytes, modified " + time.Unix(90, 0).UTC().Format(time.RFC3339Nano),
	}
	if len(got) != len(want) || got[ownerFolder] != want[ownerFolder] || got[ownerFolder+"/library.sqlite"] != want[ownerFolder+"/library.sqlite"] {
		t.Fatalf("ownerData = %q, want %q", got, want)
	}
	sys.stats[ownerFolder] = nil
	if got, err := ownerData(sys); err != nil || len(got) != 0 {
		t.Fatalf("ownerData of a missing folder = %q, %v; want nothing", got, err)
	}
}

func TestOwnerDataReportsWhatItCannotRead(t *testing.T) {
	for name, c := range map[string]struct {
		change func(*fakeSystem)
		want   string
	}{
		"no home":           {func(s *fakeSystem) { s.homeErr = errors.New("$HOME is not defined") }, "$HOME is not defined"},
		"unlistable folder": {func(s *fakeSystem) { s.dirErr = errors.New("permission denied") }, "permission denied"},
		"an entry vanished": {func(s *fakeSystem) {
			s.stats[ownerFolder+"/library.sqlite"] = []fakeStat{{err: errors.New("no such file")}}
		},
			"no such file"},
	} {
		sys := healthyRepo()
		c.change(sys)
		if _, err := ownerData(sys); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: ownerData = %v, want %q", name, err, c.want)
		}
	}
}

func TestDataFoldersReportsAFolderItCannotReadAtTheEnd(t *testing.T) {
	sys := healthyRepo()
	c := &checker{sys: sys, root: "/work/repo"}
	c.ownerBefore, c.ownerErr = ownerData(sys)
	sys.homeErr = errors.New("$HOME is not defined")
	if got := c.dataFolders(); got.status != statusFail || !strings.Contains(got.output, "could not be read at the end: $HOME is not defined") {
		t.Fatalf("dataFolders = %+v, want FAIL naming the error", got)
	}
}
