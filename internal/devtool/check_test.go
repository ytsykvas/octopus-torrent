package devtool

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

const (
	listFiles    = "git ls-files -z --cached --others --exclude-standard"
	npmConfig    = "npm --prefix frontend config get min-release-age before min-release-age-exclude"
	unitTextfmt  = "go tool covdata textfmt -i=/work/repo/build/coverage/go-unit -o build/coverage/go-unit.txt"
	appTextfmt   = "go tool covdata textfmt -i=/work/repo/build/coverage/go-app -o build/coverage/go-app.txt"
	goTestRace   = "go test -race -count=1 -covermode=atomic -coverpkg=./... ./... -args -test.gocoverdir=/work/repo/build/coverage/go-unit"
	typecheck    = "npm --prefix frontend run typecheck"
	releaseBuild = "go tool wails build -clean -trimpath"
	generate     = "go tool wails generate module"
	goEnv        = "go env -json GOFLAGS GOINSECURE GONOPROXY GONOSUMDB GOPRIVATE GOPROXY GOSUMDB"
	goEnvDefault = `{"GOFLAGS":"","GOINSECURE":"","GONOPROXY":"","GONOSUMDB":"","GOPRIVATE":"","GOPROXY":"https://proxy.golang.org,direct","GOSUMDB":"sum.golang.org"}`
)

// healthyRepo is a fake repository on which every check step passes, including the .app run.
func healthyRepo() *fakeSystem {
	sys := appRun()
	files := planFixture()
	for name, content := range map[string]string{
		"go.mod":                                  "module octopustorrent\n\ngo 1.27.2\n",
		"main.go":                                 goSources["main.go"],
		"internal/app/app.go":                     goSources["internal/app/app.go"],
		"internal/app/app_test.go":                "package app\n",
		"frontend/src/App.tsx":                    "export const App = () => null\n",
		"frontend/src/App.test.tsx":               "it('renders', () => {})\n",
		"frontend/wailsjs/go/app/App.js":          "// generated\n",
		"README.md":                               "# Repository\n",
		"frontend/coverage/coverage-summary.json": string(summary(entry("frontend/src/App.tsx", metrics(3, 3)))),
		"build/coverage/go-unit.txt":              unitProfile,
		"build/coverage/go-app.txt":               appProfile,
	} {
		files[name] = content
	}
	listing := map[string]string{}
	for name, content := range files {
		if !strings.HasPrefix(name, "build/") && !strings.HasPrefix(name, "frontend/coverage/") {
			listing[name] = content
		}
	}
	sys.files = fakeFiles(files)
	sys.output = strings.Join(sortedKeys(listing), "\x00") + "\x00"
	sys.results["git rev-parse --show-prefix"] = []fakeResult{{out: "\n"}}
	sys.results["git rev-parse --show-toplevel"] = []fakeResult{{out: "/work/repo\n"}}
	sys.results[npmConfig] = []fakeResult{{out: "min-release-age=14\nbefore=null\nmin-release-age-exclude=\n"}}
	sys.results[goEnv] = []fakeResult{{out: goEnvDefault}}
	return withDependencies(sys)
}

func runCheckOn(sys *fakeSystem, args ...string) (int, string) {
	code, stdout, stderr := runMainWithOutput(sys, args...)
	return code, stdout + stderr
}

// line returns the report line of a step, or "" if it is missing.
func line(report, step string) string {
	for _, l := range strings.Split(report, "\n") {
		if strings.Contains(l, "  "+step+"  ") {
			return l
		}
	}
	return ""
}

func TestCheckPassesTheGateWhenEveryStepPasses(t *testing.T) {
	sys := healthyRepo()
	code, report := runCheckOn(sys, "check")
	if code != 0 || !strings.HasSuffix(report, "GATE PASSED\n") {
		t.Fatalf("check exit %d, report:\n%s", code, report)
	}
	for _, step := range []string{"npm ci", "npm cooldown", "go env", "typecheck", "lint", "frontend tests", "frontend build", "gofmt",
		"go vet", "bindings", "go tests", ".app coverage run", "go coverage", "escape hatches", "release build", "docs", "plan",
		"release age", "npm audit", "govulncheck", "updates", "single code version"} {
		if l := line(report, step); !strings.HasPrefix(l, statusPass) {
			t.Errorf("step %q: %q, want pass", step, l)
		}
	}
	for _, tier := range []string{"engine integration", "test-bridge UI tests"} {
		if l := line(report, tier); !strings.HasPrefix(l, statusNotRun) || !strings.Contains(l, "not implemented yet") {
			t.Errorf("tier %q: %q, want a visible not_run", tier, l)
		}
	}
	calls := strings.Join(sys.calls, "\n")
	order := []string{"capture npm --prefix frontend ci", "remove frontend/coverage", "capture " + generate,
		"remove build/coverage", "capture " + goTestRace, "capture " + appBuild, "capture " + unitTextfmt, "capture " + releaseBuild}
	last := -1
	for _, want := range order {
		i := strings.Index(calls, want)
		if i < 0 || i < last {
			t.Fatalf("call %q missing or out of order in:\n%s", want, calls)
		}
		last = i
	}
}

func TestCheckFastSkipsTheAppRunAndReportsAnIncompleteGate(t *testing.T) {
	sys := healthyRepo()
	code, report := runCheckOn(sys, "check", "fast")
	if code != 0 || !strings.Contains(report, "GATE INCOMPLETE") || !strings.Contains(report, "go tool devtool check") {
		t.Fatalf("check fast exit %d, report:\n%s", code, report)
	}
	if l := line(report, ".app coverage run"); !strings.HasPrefix(l, statusNotRun) {
		t.Errorf(".app coverage run: %q, want not_run", l)
	}
	if l := line(report, "go coverage"); !strings.HasPrefix(l, statusNotRun) || !strings.Contains(l, "main.go:5.13,5.31") {
		t.Errorf("go coverage: %q, want not_run naming the main() block", l)
	}
	if strings.Contains(strings.Join(sys.calls, "\n"), appBuild) {
		t.Error("check fast built the instrumented .app")
	}
}

func TestCheckOfflineInstallsFromTheNpmCache(t *testing.T) {
	sys := healthyRepo()
	code, report := runCheckOn(sys, "check", "offline")
	if !strings.Contains(strings.Join(sys.calls, "\n"), "capture npm --prefix frontend ci --offline") || code != 0 {
		t.Errorf("check offline exit %d calls %q report:\n%s", code, sys.calls, report)
	}
}

func TestCheckNeverStopsEarlyAndFailsTheGate(t *testing.T) {
	sys := healthyRepo()
	sys.results[typecheck] = []fakeResult{{out: "src/App.tsx(1,1): error TS2322\n", err: errors.New("exit status 2")}}
	code, report := runCheckOn(sys, "check")
	if code != 1 || !strings.HasSuffix(report, "GATE FAILED\n") || !strings.Contains(report, "    src/App.tsx(1,1): error TS2322") {
		t.Errorf("exit %d, report:\n%s", code, report)
	}
	if !strings.HasPrefix(line(report, "release build"), statusPass) {
		t.Errorf("steps after the failure did not run:\n%s", report)
	}
}

func TestCheckFailsTheFullGateWhenAMandatoryStepCannotRun(t *testing.T) {
	sys := healthyRepo()
	sys.results[isRunning] = []fakeResult{{out: "true\n"}}
	code, report := runCheckOn(sys, "check")
	if code != 1 || !strings.Contains(report, "GATE FAILED: not run: .app coverage run") {
		t.Errorf("exit %d, report:\n%s", code, report)
	}
}

func TestCheckStepsReportTheirOwnFailures(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeSystem)
		step   string
		want   string
	}{
		"npm cooldown removed": {func(s *fakeSystem) {
			s.results[npmConfig] = []fakeResult{{out: "min-release-age=0\nbefore=2026-01-01\nmin-release-age-exclude=vite\n"}}
		}, "npm cooldown", "min-release-age is 0"},
		"npm config fails": {func(s *fakeSystem) { s.results[npmConfig] = []fakeResult{{out: "npm ERR!", err: exitStatus1}} },
			"npm cooldown", "exit status 1"},
		"Go checksums off": {func(s *fakeSystem) {
			s.results[goEnv] = []fakeResult{{out: strings.Replace(goEnvDefault, `"GOSUMDB":"sum.golang.org"`, `"GOSUMDB":"off"`, 1)}}
		}, "go env", "GOSUMDB is off"},
		"go env fails":   {func(s *fakeSystem) { s.results[goEnv] = []fakeResult{{out: "go: unknown flag", err: exitStatus1}} }, "go env", "exit status 1"},
		"go env garbage": {func(s *fakeSystem) { s.results[goEnv] = []fakeResult{{out: "{"}} }, "go env", "unexpected end of JSON"},
		"frontend tests fail": {func(s *fakeSystem) {
			s.results["npm --prefix frontend run test:coverage"] = []fakeResult{{out: "1 failed", err: exitStatus1}}
		}, "frontend tests", "tests or coverage thresholds failed"},
		"frontend file uncovered": {func(s *fakeSystem) {
			s.files["frontend/coverage/coverage-summary.json"] = string(summary(entry("frontend/src/App.tsx", metrics(2, 3))))
		}, "frontend tests", "frontend/src/App.tsx: lines 2/3"},
		"unformatted Go": {func(s *fakeSystem) { s.results["gofmt -l ."] = []fakeResult{{out: "main.go\n"}} }, "gofmt", "main.go"},
		"gofmt fails":    {func(s *fakeSystem) { s.results["gofmt -l ."] = []fakeResult{{out: "x", err: exitStatus1}} }, "gofmt", "exit status 1"},
		"stale bindings": {func(s *fakeSystem) {
			s.results[generate] = []fakeResult{{out: "regenerated"}}
			s.results[listFiles] = []fakeResult{{out: s.output}, {out: s.output + "frontend/wailsjs/go/app/New.js\x00"}}
			s.files["frontend/wailsjs/go/app/New.js"] = "// new\n"
		}, "bindings", "frontend/wailsjs/go/app/New.js"},
		"generation fails": {func(s *fakeSystem) { s.results[generate] = []fakeResult{{out: "boom", err: exitStatus1}} }, "bindings", "exit status 1"},
		"go tests fail":    {func(s *fakeSystem) { s.results[goTestRace] = []fakeResult{{out: "--- FAIL", err: exitStatus1}} }, "go tests", "--- FAIL"},
		"stale frontend report": {func(s *fakeSystem) { s.removeErr = map[string]error{"frontend/coverage": errors.New("busy")} },
			"frontend tests", "remove the previous coverage report"},
		"stale go profiles": {func(s *fakeSystem) { s.removeErr = map[string]error{"build/coverage": errors.New("busy")} },
			"go tests", "remove the previous coverage profiles"},
		"no unit dir": {func(s *fakeSystem) { s.mkdirErr = errors.New("read-only") }, "go tests", "read-only"},
		"uncovered Go": {func(s *fakeSystem) {
			s.files["build/coverage/go-unit.txt"] = strings.Replace(unitProfile, "7.16,9.2 1 3", "7.16,9.2 1 0", 1)
			s.files["build/coverage/go-app.txt"] = strings.Replace(appProfile, "7.16,9.2 1 1", "7.16,9.2 1 0", 1)
		}, "go coverage", "main.go:7.16,9.2 is not covered"},
		"textfmt fails": {func(s *fakeSystem) { s.results[unitTextfmt] = []fakeResult{{out: "no data", err: exitStatus1}} },
			"go coverage", "unit coverage profile is missing"},
		"escape hatch": {func(s *fakeSystem) { s.files["frontend/src/App.tsx"] = "/* v8 ignore next */\n" }, "escape hatches",
			"frontend/src/App.tsx:1: v8 ignore"},
		"dead link": {func(s *fakeSystem) { s.files["README.md"] = "[x](gone.md)\n" }, "docs", "DEAD README.md: gone.md"},
		"stale plan": {func(s *fakeSystem) {
			s.files["product/tasks/003-check.md"] = strings.Replace(s.files["product/tasks/003-check.md"], "`planned`", "`blocked`", 1)
		}, "plan", "navigation table is out of date"},
		"file added": {func(s *fakeSystem) {
			s.results[listFiles] = []fakeResult{{out: s.output}, {out: s.output + "notes.txt\x00"}}
			s.files["notes.txt"] = "written during the check\n"
		}, "single code version", "    notes.txt"},
		"file removed": {func(s *fakeSystem) {
			s.results[listFiles] = []fakeResult{{out: s.output}, {out: strings.Replace(s.output, "README.md\x00", "", 1)}}
		}, "single code version", "    README.md"},
	}
	for name, c := range cases {
		sys := healthyRepo()
		c.change(sys)
		code, report := runCheckOn(sys, "check")
		if l := line(report, c.step); code != 1 || !strings.HasPrefix(l, statusFail) || !strings.Contains(report, c.want) {
			t.Errorf("%s: exit %d, step %q line %q, want FAIL with %q in:\n%s", name, code, c.step, l, c.want, report)
		}
	}
}

func TestCheckRefusesToRunOutsideTheRepositoryRoot(t *testing.T) {
	for name, change := range map[string]func(*fakeSystem){
		"subdirectory": func(s *fakeSystem) { s.results["git rev-parse --show-prefix"] = []fakeResult{{out: "frontend/\n"}} },
		"not git":      func(s *fakeSystem) { s.results["git rev-parse --show-prefix"] = []fakeResult{{err: exitStatus1}} },
		"no toplevel":  func(s *fakeSystem) { s.results["git rev-parse --show-toplevel"] = []fakeResult{{err: exitStatus1}} },
		"no listing":   func(s *fakeSystem) { s.outputErr = errors.New("not a git repository") },
	} {
		sys := healthyRepo()
		change(sys)
		code, report := runCheckOn(sys, "check")
		if code != 1 || !strings.Contains(report, "GATE FAILED") || strings.Contains(strings.Join(sys.calls, "\n"), "capture npm") {
			t.Errorf("%s: exit %d calls %q report:\n%s", name, code, sys.calls, report)
		}
	}
}

func TestCheckNpmCooldownAcceptsOnlyTheFullCooldown(t *testing.T) {
	if got := CheckNpmCooldown("min-release-age=21\nbefore=null\nmin-release-age-exclude=\n"); len(got) != 0 {
		t.Errorf("CheckNpmCooldown(21 days) = %q", got)
	}
	got := strings.Join(CheckNpmCooldown("before=2026-01-01\nmin-release-age-exclude=vite\n"), "\n")
	for _, want := range []string{"min-release-age is not set", "before is 2026-01-01", "min-release-age-exclude is vite"} {
		if !strings.Contains(got, want) {
			t.Errorf("CheckNpmCooldown missing %q in %q", want, got)
		}
	}
	if got := strings.Join(CheckNpmCooldown("min-release-age=soon\n"), "\n"); !strings.Contains(got, "min-release-age is soon") {
		t.Errorf("CheckNpmCooldown(soon) = %q", got)
	}
}

// Go reads these settings from the environment and from go env -w; each can switch off checksum
// verification or fetch modules from somewhere other than the proxy whose dates the gate checks.
func TestCheckGoEnvAcceptsOnlyTheDefaults(t *testing.T) {
	if got := CheckGoEnv(map[string]string{"GOPROXY": "https://proxy.golang.org,direct", "GOSUMDB": "sum.golang.org"}); len(got) != 0 {
		t.Errorf("CheckGoEnv(defaults) = %q", got)
	}
	got := strings.Join(CheckGoEnv(map[string]string{"GOFLAGS": "-mod=mod", "GOINSECURE": "example.com", "GONOPROXY": "x",
		"GONOSUMDB": "x", "GOPRIVATE": "x", "GOPROXY": "direct", "GOSUMDB": "off"}), "\n")
	for _, want := range []string{"GOFLAGS is -mod=mod, want it unset", "GOINSECURE is example.com", "GONOPROXY is x", "GONOSUMDB is x",
		"GOPRIVATE is x", "GOPROXY is direct, want https://proxy.golang.org,direct", "GOSUMDB is off, want sum.golang.org"} {
		if !strings.Contains(got, want) {
			t.Errorf("CheckGoEnv missing %q in:\n%s", want, got)
		}
	}
}

func TestReportShowsOnlyTheTailOfALongFailure(t *testing.T) {
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	var out strings.Builder
	report(&out, "go tests", fail("go test failed", strings.Join(lines, "\n")))
	if got := out.String(); !strings.Contains(got, "    line 40\n") || !strings.Contains(got, "    line 11\n") || strings.Contains(got, "line 10\n") {
		t.Errorf("report kept the wrong lines:\n%s", got)
	}
}

func TestBindingsReportARemovedFileAndAnUnreadableListing(t *testing.T) {
	sys := &fakeSystem{
		output: "frontend/wailsjs/a.js\x00",
		files:  fakeFiles{"frontend/wailsjs/a.js": "a", "frontend/wailsjs/b.js": "b"},
	}
	c := &checker{sys: sys, files: []string{"frontend/wailsjs/a.js", "frontend/wailsjs/b.js"}}
	if got := c.bindings(); got.status != statusFail || got.output != "frontend/wailsjs/b.js" {
		t.Errorf("removed binding: %+v", got)
	}
	sys.outputErr = errors.New("not a git repository")
	if got := c.bindings(); got.status != statusFail || !strings.Contains(got.detail, "not a git repository") {
		t.Errorf("unreadable listing: %+v", got)
	}
	if got := c.sameSources(nil); got.status != statusFail || !strings.Contains(got.detail, "not a git repository") {
		t.Errorf("unreadable listing at the end: %+v", got)
	}
	for name, check := range map[string]func(System) (string, []string, error){"docs": docsCheck, "plan": planCheck} {
		if got := c.own(check)(); got.status != statusFail || !strings.Contains(got.detail, "not a git repository") {
			t.Errorf("%s with an unreadable listing: %+v", name, got)
		}
	}
}

func TestSameSourcesNamesAFileWhoseContentChanged(t *testing.T) {
	sys := &fakeSystem{output: "a.go\x00b.go\x00", files: fakeFiles{"a.go": "package a\n", "b.go": "package b\n"}}
	c := &checker{sys: sys, files: []string{"a.go", "b.go"}}
	before := c.fingerprints(c.files)
	sys.files["b.go"] = "package b // edited\n"
	if got := c.sameSources(before); got.status != statusFail || got.output != "b.go" {
		t.Errorf("sameSources = %+v, want FAIL naming b.go", got)
	}
}
