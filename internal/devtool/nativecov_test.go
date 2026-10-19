package devtool

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

const nativeRoot = "/work/repo"

// The command lines (nc*) and the raw-profile path NativeCoverage uses for internal/platform.
const (
	ncFind   = "xcrun --find llvm-cov"
	ncBuild  = "env CGO_CFLAGS=-fprofile-instr-generate -fcoverage-mapping CGO_LDFLAGS=-fprofile-instr-generate go test -c -o /work/repo/build/coverage/native/internal_platform.test ./internal/platform"
	ncRaw    = "/work/repo/build/coverage/native/internal_platform.profraw"
	ncRun    = "env LLVM_PROFILE_FILE=" + ncRaw + " /work/repo/build/coverage/native/internal_platform.test -test.count=1"
	ncMerge  = "xcrun llvm-profdata merge -sparse " + ncRaw + " -o /work/repo/build/coverage/native/internal_platform.profdata"
	ncExport = "xcrun llvm-cov export -summary-only /work/repo/build/coverage/native/internal_platform.test -instr-profile=/work/repo/build/coverage/native/internal_platform.profdata /work/repo/internal/platform/trash_darwin.m"
)

// exportJSON is an llvm-cov export for trash_darwin.m with the given region counts.
func exportJSON(covered, count int) string {
	return `{"data":[{"files":[{"filename":"/work/repo/internal/platform/trash_darwin.m","summary":{"regions":{"count":` +
		strconv.Itoa(count) + `,"covered":` + strconv.Itoa(covered) + `}}}]}]}`
}

func platformFiles() []string {
	return []string{"internal/platform/trash.go", "internal/platform/trash_darwin.m", "internal/platform/trash_darwin.h"}
}

// passingSys answers the export (via Output) with full coverage and a non-empty profile file, so the
// happy path runs end to end. A test overrides one entry to drive a failure.
func passingSys() *fakeSystem {
	return &fakeSystem{
		results: map[string][]fakeResult{ncExport: {{out: exportJSON(9, 9)}}},
		files:   fakeFiles{ncRaw: "PROFRAW"},
	}
}

func TestNativeCoveragePassesWhenEveryRegionIsCovered(t *testing.T) {
	sys := passingSys()
	got := NativeCoverage(sys, nativeRoot, platformFiles())
	if got.status != statusPass || !strings.Contains(got.detail, "trash_darwin.m") {
		t.Fatalf("NativeCoverage = %+v, want pass naming the source", got)
	}
	calls := strings.Join(sys.calls, "\n")
	for _, want := range []string{"capture " + ncFind, "mkdir /work/repo/build/coverage/native", "capture " + ncBuild, "remove " + ncRaw, "capture " + ncRun, "capture " + ncMerge, "output " + ncExport} {
		if !strings.Contains(calls, want) {
			t.Errorf("missing call %q:\n%s", want, calls)
		}
	}
}

func TestNativeCoverageFailsOnAnUncoveredRegion(t *testing.T) {
	sys := passingSys()
	sys.results[ncExport] = []fakeResult{{out: exportJSON(7, 9)}}
	got := NativeCoverage(sys, nativeRoot, platformFiles())
	if got.status != statusFail || !strings.Contains(got.output, "trash_darwin.m: 7 of 9 regions covered") {
		t.Fatalf("NativeCoverage = %+v, want fail naming the uncovered source", got)
	}
}

func TestNativeCoverageFailsWhenASourceIsNotReported(t *testing.T) {
	sys := passingSys()
	sys.results[ncExport] = []fakeResult{{out: `{"data":[{"files":[]}]}`}}
	got := NativeCoverage(sys, nativeRoot, platformFiles())
	if got.status != statusFail || !strings.Contains(got.output, "trash_darwin.m: no coverage was reported") {
		t.Fatalf("NativeCoverage = %+v, want fail for the unreported source", got)
	}
}

// Each stage's failure is reported, named for the stage, so a swapped branch cannot pass.
func TestNativeCoverageReportsEachStageFailure(t *testing.T) {
	boom := errors.New("exit status 1")
	for _, c := range []struct {
		name string
		set  func(*fakeSystem)
		says string
	}{
		{"build", func(s *fakeSystem) { s.results[ncBuild] = []fakeResult{{out: "link error", err: boom}} }, "instrumented build failed"},
		{"run", func(s *fakeSystem) { s.results[ncRun] = []fakeResult{{out: "panic", err: boom}} }, "instrumented test run failed"},
		{"empty profile", func(s *fakeSystem) { s.files[ncRaw] = "" }, "profile is empty"},
		{"merge", func(s *fakeSystem) { s.results[ncMerge] = []fakeResult{{out: "bad profile", err: boom}} }, "merging the profile failed"},
		{"export", func(s *fakeSystem) { s.results[ncExport] = []fakeResult{{out: "no instr", err: boom}} }, "exporting coverage failed"},
		{"bad json", func(s *fakeSystem) { s.results[ncExport] = []fakeResult{{out: "not json"}} }, "could not be read"},
		{"empty data", func(s *fakeSystem) { s.results[ncExport] = []fakeResult{{out: `{"data":[]}`}} }, "could not be read"},
		{"clear old profile", func(s *fakeSystem) { s.removeErr = map[string]error{ncRaw: boom} }, "could not clear the old profile"},
	} {
		sys := passingSys()
		c.set(sys)
		got := NativeCoverage(sys, nativeRoot, platformFiles())
		if got.status != statusFail || !strings.Contains(got.output, c.says) {
			t.Errorf("%s: NativeCoverage = %+v, want fail saying %q", c.name, got, c.says)
		}
	}
}

func TestNativeCoverageIsNotRunWithoutTheTools(t *testing.T) {
	sys := passingSys()
	sys.results[ncFind] = []fakeResult{{out: "unable to find utility \"llvm-cov\"", err: errors.New("exit status 72")}}
	got := NativeCoverage(sys, nativeRoot, platformFiles())
	if got.status != statusNotRun || !strings.Contains(got.detail, "not available") {
		t.Fatalf("NativeCoverage = %+v, want not_run", got)
	}
}

func TestNativeCoverageFailsWhenTheDirCannotBeMade(t *testing.T) {
	sys := passingSys()
	sys.mkdirErr = errors.New("read-only")
	if got := NativeCoverage(sys, nativeRoot, platformFiles()); got.status != statusFail {
		t.Fatalf("NativeCoverage = %+v, want fail", got)
	}
}

// A native source that sits where it cannot be measured fails the step rather than being dropped.
func TestNativeCoverageFailsOnAMisplacedNativeSource(t *testing.T) {
	for _, bad := range []string{"main.c", "internal/platform/testdata/fixture.m", "build/x.c"} {
		got := NativeCoverage(passingSys(), nativeRoot, append(platformFiles(), bad))
		if got.status != statusFail || !strings.Contains(got.output, bad) {
			t.Errorf("%s: NativeCoverage = %+v, want fail naming it", bad, got)
		}
	}
}

func TestNativeCoveragePassesWithNoNativeCode(t *testing.T) {
	got := NativeCoverage(&fakeSystem{}, nativeRoot, []string{"internal/app/app.go", "frontend/src/x.ts"})
	if got.status != statusPass || !strings.Contains(got.detail, "no native code") {
		t.Fatalf("NativeCoverage = %+v, want pass for no native code", got)
	}
}

// Two packages are each built, run and measured, in sorted order.
func TestNativeCoverageMeasuresEveryPackage(t *testing.T) {
	const otherExport = "xcrun llvm-cov export -summary-only /work/repo/build/coverage/native/internal_other.test -instr-profile=/work/repo/build/coverage/native/internal_other.profdata /work/repo/internal/other/x.c"
	sys := passingSys()
	sys.files["/work/repo/build/coverage/native/internal_other.profraw"] = "PROFRAW"
	sys.results[otherExport] = []fakeResult{{out: `{"data":[{"files":[{"filename":"/work/repo/internal/other/x.c","summary":{"regions":{"count":3,"covered":3}}}]}]}`}}
	got := NativeCoverage(sys, nativeRoot, append(platformFiles(), "internal/other/x.c"))
	if got.status != statusPass {
		t.Fatalf("NativeCoverage = %+v, want pass for both packages", got)
	}
	calls := strings.Join(sys.calls, "\n")
	if !strings.Contains(calls, "output "+otherExport) || !strings.Contains(calls, "output "+ncExport) {
		t.Errorf("both packages must be exported:\n%s", calls)
	}
	if strings.Index(calls, "internal_other") > strings.Index(calls, "internal_platform") {
		t.Error("packages must be measured in sorted order")
	}
}
