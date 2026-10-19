package devtool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// nativeCoverDir holds the instrumented test binaries and clang profiles.
const nativeCoverDir = coverRoot + "/native"

// cgo flags that build a test binary with clang source-based coverage. They are scoped to this one
// build through the environment, never the product or release build.
const (
	cgoCoverCFlags  = "CGO_CFLAGS=-fprofile-instr-generate -fcoverage-mapping"
	cgoCoverLDFlags = "CGO_LDFLAGS=-fprofile-instr-generate"
)

// nativeExtensions are the own-source extensions clang compiles as native code.
var nativeExtensions = []string{".m", ".c", ".mm", ".cc", ".cpp", ".cxx"}

// nativeSources groups our own native sources by their package directory, and reports any that sit
// where the step cannot measure them. Executable native code belongs in a package directory under
// internal/ or cmd/, not at the repository root, in build/ or under testdata/; a `.go` file's inline
// C preamble is Go-built and is not a source here (the one allowed shim lives in test support). The
// listing is the inventory, so a native file cannot be dropped silently — an unexpected one fails
// the step, as the Go inventory does.
func nativeSources(files []string) (byPkg map[string][]string, misplaced []string) {
	byPkg = map[string][]string{}
	for _, f := range files {
		if !hasNativeExtension(f) {
			continue
		}
		dir := filepath.Dir(f)
		switch {
		case !(strings.HasPrefix(f, "internal/") || strings.HasPrefix(f, "cmd/")),
			strings.HasPrefix(f, "build/"),
			dir == ".",
			slices.Contains(strings.Split(f, "/"), "testdata"):
			misplaced = append(misplaced, f)
		default:
			byPkg[dir] = append(byPkg[dir], f)
		}
	}
	return byPkg, misplaced
}

func hasNativeExtension(f string) bool {
	ext := filepath.Ext(f)
	for _, e := range nativeExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

// llvmCovExport is the part of `llvm-cov export` we read: per file, the region counts.
type llvmCovExport struct {
	Data []struct {
		Files []struct {
			Filename string `json:"filename"`
			Summary  struct {
				Regions struct {
					Count   int `json:"count"`
					Covered int `json:"covered"`
				} `json:"regions"`
			} `json:"summary"`
		} `json:"files"`
	} `json:"data"`
}

// NativeCoverage measures the coverage of our own native sources and requires every region covered.
// For each package with native code it builds an instrumented test binary, runs it (the binary's
// TestMain flushes the profile, which os.Exit would otherwise drop), merges the profile and exports
// the region counts, failing if any region is uncovered or if a native source sits where it cannot
// be measured. It is skipped with no native code, and reported not_run when the clang coverage tools
// are absent, never passed silently.
func NativeCoverage(sys System, root string, files []string) stepResult {
	sources, misplaced := nativeSources(files)
	if len(misplaced) > 0 {
		return fail("native source outside a measurable package", "move into a package under internal/ or cmd/ (not the root, build/ or testdata/): "+strings.Join(misplaced, ", "))
	}
	if len(sources) == 0 {
		return pass("no native code to measure")
	}
	if out, err := sys.Capture("xcrun", "--find", "llvm-cov"); err != nil {
		return notRun("the clang coverage tools (xcrun llvm-cov) are not available: " + strings.TrimSpace(out))
	}
	dir := filepath.Join(root, nativeCoverDir)
	if err := sys.MkdirAll(dir); err != nil {
		return fail("create "+dir+": "+err.Error(), "")
	}
	var problems []string
	var measured []string
	for _, pkg := range sortedKeys(sources) {
		if p := measurePackage(sys, root, dir, pkg, sources[pkg]); len(p) > 0 {
			problems = append(problems, p...)
		}
		measured = append(measured, sources[pkg]...)
	}
	if len(problems) > 0 {
		return fail(fmt.Sprintf("%d native coverage problems", len(problems)), strings.Join(problems, "\n"))
	}
	sort.Strings(measured)
	return pass("every region of every own native source is covered: " + strings.Join(measured, ", "))
}

// measurePackage builds, runs, merges and exports one package's native coverage, returning the
// problems found (a build/run/tool failure, an empty profile, or an uncovered source).
func measurePackage(sys System, root, dir, pkg string, srcs []string) []string {
	name := strings.ReplaceAll(pkg, "/", "_")
	bin := filepath.Join(dir, name+".test")
	raw := filepath.Join(dir, name+".profraw")
	data := filepath.Join(dir, name+".profdata")

	if out, err := sys.Capture("env", cgoCoverCFlags, cgoCoverLDFlags, "go", "test", "-c", "-o", bin, "./"+pkg); err != nil {
		return []string{pkg + ": instrumented build failed: " + err.Error() + "\n" + out}
	}
	if err := sys.RemoveAll(raw); err != nil { // a stale profile from an earlier run must not be read
		return []string{pkg + ": could not clear the old profile: " + err.Error()}
	}
	if out, err := sys.Capture("env", "LLVM_PROFILE_FILE="+raw, bin, "-test.count=1"); err != nil {
		return []string{pkg + ": instrumented test run failed: " + err.Error() + "\n" + out}
	}
	if profile, err := sys.ReadFile(raw); err != nil || len(profile) == 0 {
		return []string{pkg + ": the coverage profile is empty; the package's TestMain must call nativecov.Flush before os.Exit"}
	}
	if out, err := sys.Capture("xcrun", "llvm-profdata", "merge", "-sparse", raw, "-o", data); err != nil {
		return []string{pkg + ": merging the profile failed: " + err.Error() + "\n" + out}
	}
	// Output (stdout only), not Capture: an llvm-cov warning on stderr would corrupt the JSON.
	args := append([]string{"llvm-cov", "export", "-summary-only", bin, "-instr-profile=" + data}, absSources(root, srcs)...)
	out, err := sys.Output("xcrun", args...)
	if err != nil {
		return []string{pkg + ": exporting coverage failed: " + err.Error() + "\n" + out}
	}
	var export llvmCovExport
	if err := json.Unmarshal([]byte(out), &export); err != nil || len(export.Data) == 0 {
		return []string{pkg + ": the coverage export could not be read: " + exportError(err, out)}
	}
	return uncovered(export, root, srcs)
}

// uncovered lists the sources llvm-cov reports with a missing region, and the sources it did not
// report at all. It is pure: the export and the sources in, the problems out.
func uncovered(export llvmCovExport, root string, srcs []string) []string {
	regions := map[string]struct{ covered, count int }{}
	for _, f := range export.Data[0].Files {
		regions[f.Filename] = struct{ covered, count int }{f.Summary.Regions.Covered, f.Summary.Regions.Count}
	}
	var problems []string
	for i, src := range srcs {
		switch r, ok := regions[absSources(root, srcs)[i]]; {
		case !ok:
			problems = append(problems, src+": no coverage was reported")
		case r.covered < r.count:
			problems = append(problems, fmt.Sprintf("%s: %d of %d regions covered", src, r.covered, r.count))
		}
	}
	return problems
}

func absSources(root string, srcs []string) []string {
	abs := make([]string, len(srcs))
	for i, s := range srcs {
		abs[i] = filepath.Join(root, s)
	}
	return abs
}

// exportError describes why an llvm-cov export could not be read: the parse error, or the output
// itself when it parsed to no data.
func exportError(err error, out string) string {
	if err != nil {
		return err.Error()
	}
	return "no data in " + out
}
