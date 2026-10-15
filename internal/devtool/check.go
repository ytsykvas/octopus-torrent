package devtool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Statuses of a check step, as printed in the report.
const (
	statusPass   = "pass"
	statusFail   = "FAIL"
	statusNotRun = "not_run"
)

// stepResult is the outcome of one check step. output is the tool output shown under a failure.
// optional marks a not_run that does not make the gate incomplete: a tier that does not exist yet.
type stepResult struct {
	status, detail, output string
	optional               bool
}

func pass(detail string) stepResult { return stepResult{status: statusPass, detail: detail} }
func fail(detail, output string) stepResult {
	return stepResult{status: statusFail, detail: detail, output: output}
}
func notRun(detail string) stepResult { return stepResult{status: statusNotRun, detail: detail} }
func notBuiltYet(detail string) stepResult {
	return stepResult{status: statusNotRun, detail: detail, optional: true}
}

// checkMode selects the steps: the full gate runs everything; offline skips the network steps and
// fast also skips the .app run. Skipped steps are reported as not_run, so only the full gate passes.
type checkMode struct {
	app, network bool
}

var checkModes = map[string]checkMode{
	"check":         {app: true, network: true},
	"check offline": {app: true},
	"check fast":    {},
}

const (
	unitCoverDir   = "build/coverage/go-unit"
	appCoverDir    = "build/coverage/go-app"
	frontendReport = "frontend/coverage/coverage-summary.json"
	reportTail     = 30
)

type checkStep struct {
	name string
	run  func() stepResult
}

type checker struct {
	sys    System
	root   string
	mode   checkMode
	files  []string
	appRan bool
}

// runCheck runs every step of the mode in order, never stopping early, prints one line per step and
// returns the exit code: 1 if a step failed, or if a mandatory step did not run in the full gate.
func runCheck(full bool, mode checkMode, stdout io.Writer, sys System) int {
	c := &checker{sys: sys, mode: mode}
	before, err := c.start()
	if err != nil {
		report(stdout, "repository", fail(err.Error(), ""))
		fmt.Fprintln(stdout, "GATE FAILED")
		return 1
	}
	var failed, missing []string
	for _, s := range append(c.steps(), checkStep{"single code version", func() stepResult { return c.sameSources(before) }}) {
		r := s.run()
		report(stdout, s.name, r)
		switch {
		case r.status == statusFail:
			failed = append(failed, s.name)
		case r.status == statusNotRun && !r.optional:
			missing = append(missing, s.name)
		}
	}
	switch {
	case len(failed) > 0:
		fmt.Fprintln(stdout, "GATE FAILED")
		return 1
	case len(missing) > 0 && full:
		fmt.Fprintln(stdout, "GATE FAILED: not run: "+strings.Join(missing, ", "))
		return 1
	case len(missing) > 0:
		fmt.Fprintln(stdout, "GATE INCOMPLETE: not run: "+strings.Join(missing, ", ")+"; only go tool devtool check accepts a change")
		return 0
	}
	fmt.Fprintln(stdout, "GATE PASSED")
	return 0
}

func report(stdout io.Writer, name string, r stepResult) {
	fmt.Fprintf(stdout, "%-7s  %s  %s\n", r.status, name, r.detail)
	if r.status != statusFail || r.output == "" {
		return
	}
	lines := strings.Split(strings.TrimRight(r.output, "\n"), "\n")
	if len(lines) > reportTail {
		lines = lines[len(lines)-reportTail:]
	}
	for _, l := range lines {
		fmt.Fprintln(stdout, "    "+l)
	}
}

// start requires the repository root as the working directory (every path below is relative to it)
// and records the source hash.
func (c *checker) start() (string, error) {
	prefix, err := c.sys.Output("git", "rev-parse", "--show-prefix")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %w", err)
	}
	if strings.TrimSpace(prefix) != "" {
		return "", errors.New("run the check from the repository root, not " + strings.TrimSpace(prefix))
	}
	root, err := c.sys.Output("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find the repository root: %w", err)
	}
	c.root = strings.TrimSpace(root)
	if c.files, err = repositoryFiles(c.sys); err != nil {
		return "", err
	}
	return c.hash(c.files), nil
}

func (c *checker) steps() []checkStep {
	npmCI := []string{"npm", "--prefix", "frontend", "ci"}
	if !c.mode.network {
		npmCI = append(npmCI, "--offline")
	}
	return []checkStep{
		{"npm ci", c.tool(npmCI...)},
		{"npm cooldown", c.npmCooldown},
		{"typecheck", c.tool("npm", "--prefix", "frontend", "run", "typecheck")},
		{"lint", c.tool("npm", "--prefix", "frontend", "run", "lint")},
		{"frontend tests", c.frontendTests},
		{"frontend build", c.tool("npm", "--prefix", "frontend", "run", "build")},
		{"gofmt", c.gofmt},
		{"go vet", c.tool("go", "vet", "./...")},
		{"bindings", c.bindings},
		{"go tests", c.goTests},
		{".app coverage run", c.appRun},
		{"go coverage", c.goCoverage},
		{"escape hatches", c.escapeHatches},
		{"release build", c.tool("go", "tool", "wails", "build", "-clean", "-trimpath")},
		{"docs", c.captured(checkDocs)},
		{"plan", c.captured(checkPlan)},
		{"engine integration", func() stepResult { return notBuiltYet("not implemented yet: tasks 007, 018") }},
		{"test-bridge UI tests", func() stepResult { return notBuiltYet("not implemented yet: task 035") }},
	}
}

// tool runs a command; a non-zero exit fails the step with the command's output.
func (c *checker) tool(command ...string) func() stepResult {
	return func() stepResult {
		if out, err := c.sys.Capture(command[0], command[1:]...); err != nil {
			return fail(strings.Join(command, " ")+": "+err.Error(), out)
		}
		return pass(strings.Join(command, " "))
	}
}

// captured runs one of devtool's own checks and keeps its report.
func (c *checker) captured(check func(io.Writer, System) error) func() stepResult {
	return func() stepResult {
		var out bytes.Buffer
		if err := check(&out, c.sys); err != nil {
			return fail(err.Error(), out.String())
		}
		return pass(strings.TrimSpace(strings.TrimPrefix(out.String(), statusPass)))
	}
}

// CheckNpmCooldown verifies npm's effective configuration as printed by `npm config get
// min-release-age before min-release-age-exclude`: the cooldown is at least minReleaseAgeDays and
// nothing overrides it. This is the guarantee behind the release-guard hook.
func CheckNpmCooldown(config string) []string {
	values := map[string]string{}
	for _, l := range strings.Split(config, "\n") {
		if key, value, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			values[key] = value
		}
	}
	var problems []string
	age, set := values["min-release-age"]
	if days, err := strconv.Atoi(age); !set {
		problems = append(problems, "min-release-age is not set")
	} else if err != nil || days < minReleaseAgeDays {
		problems = append(problems, fmt.Sprintf("min-release-age is %s, want at least %d", age, minReleaseAgeDays))
	}
	for _, key := range []string{"before", "min-release-age-exclude"} {
		if v := values[key]; v != "" && v != "null" {
			problems = append(problems, key+" is "+v+", want it unset")
		}
	}
	return problems
}

func (c *checker) npmCooldown() stepResult {
	out, err := c.sys.Capture("npm", "--prefix", "frontend", "config", "get", "min-release-age", "before", "min-release-age-exclude")
	if err != nil {
		return fail("npm config get: "+err.Error(), out)
	}
	if problems := CheckNpmCooldown(out); len(problems) > 0 {
		return fail(strings.Join(problems, "; "), "")
	}
	return pass(fmt.Sprintf("min-release-age ≥ %d days, no override", minReleaseAgeDays))
}

func (c *checker) frontendTests() stepResult {
	if err := c.sys.RemoveAll("frontend/coverage"); err != nil {
		return fail("remove the previous coverage report: "+err.Error(), "")
	}
	if out, err := c.sys.Capture("npm", "--prefix", "frontend", "run", "test:coverage"); err != nil {
		return fail("tests or coverage thresholds failed: "+err.Error(), out)
	}
	report, _ := c.sys.ReadFile(frontendReport)
	inventory := FrontendInventory(c.files)
	if problems := CheckFrontendCoverage(inventory, report, c.root); len(problems) > 0 {
		return fail(fmt.Sprintf("%d coverage problems", len(problems)), strings.Join(problems, "\n"))
	}
	return pass(fmt.Sprintf("%d own files at 100%% lines, statements, functions and branches", len(inventory)))
}

func (c *checker) gofmt() stepResult {
	out, err := c.sys.Capture("gofmt", "-l", ".")
	switch {
	case err != nil:
		return fail("gofmt -l .: "+err.Error(), out)
	case strings.TrimSpace(out) != "":
		return fail("files are not gofmt-formatted", out)
	}
	return pass("every Go file is formatted")
}

// bindings regenerates the Wails bindings and compares the contents (not the file modes, which
// Wails itself changes) with what was there before.
func (c *checker) bindings() stepResult {
	before := c.contents(c.files, "frontend/wailsjs/")
	if out, err := c.sys.Capture("go", "tool", "wails", "generate", "module"); err != nil {
		return fail("go tool wails generate module: "+err.Error(), out)
	}
	files, err := repositoryFiles(c.sys)
	if err != nil {
		return fail(err.Error(), "")
	}
	after := c.contents(files, "frontend/wailsjs/")
	var changed []string
	for _, name := range sortedKeys(after) {
		if before[name] != after[name] {
			changed = append(changed, name)
		}
	}
	for _, name := range sortedKeys(before) {
		if _, ok := after[name]; !ok {
			changed = append(changed, name)
		}
	}
	if len(changed) > 0 {
		return fail("the committed bindings differ from the Go facade; commit the regenerated files", strings.Join(changed, "\n"))
	}
	return pass(fmt.Sprintf("frontend/wailsjs matches the Go facade (%d files)", len(after)))
}

func (c *checker) goTests() stepResult {
	if err := c.sys.RemoveAll("build/coverage"); err != nil {
		return fail("remove the previous coverage profiles: "+err.Error(), "")
	}
	dir := c.root + "/" + unitCoverDir
	if err := c.sys.MkdirAll(dir); err != nil {
		return fail("create "+dir+": "+err.Error(), "")
	}
	return c.tool("go", "test", "-race", "-count=1", "-covermode=atomic", "-coverpkg=./...", "./...", "-args", "-test.gocoverdir="+dir)()
}

func (c *checker) appRun() stepResult {
	if !c.mode.app {
		return notRun("skipped by check fast")
	}
	r := appCoverageRun(c.sys, c.root, c.root+"/"+appCoverDir)
	c.appRan = r.status == statusPass
	return r
}

// profile converts raw counters to text; a failed conversion leaves the profile empty, which the
// coverage check reports.
func (c *checker) profile(dir string) string {
	if _, err := c.sys.Capture("go", "tool", "covdata", "textfmt", "-i="+c.root+"/"+dir, "-o", dir+".txt"); err != nil {
		return ""
	}
	text, _ := c.sys.ReadFile(dir + ".txt")
	return string(text)
}

func (c *checker) goCoverage() stepResult {
	unit, app := c.profile(unitCoverDir), ""
	if c.appRan {
		app = c.profile(appCoverDir)
	}
	gomod, _ := c.sys.ReadFile("go.mod")
	sources := c.contents(c.files, "")
	problems, deferred := CheckGoCoverage(GoModulePath(string(gomod)), sources, unit, app, c.appRan)
	switch {
	case len(problems) > 0:
		return fail(fmt.Sprintf("%d coverage problems", len(problems)), strings.Join(problems, "\n"))
	case len(deferred) > 0:
		return notRun("func main is covered only by the .app run: " + strings.Join(deferred, ", "))
	}
	return pass("every block of every own Go file is covered (unit tests + .app run)")
}

func (c *checker) escapeHatches() stepResult {
	if found := EscapeHatches(c.contents(c.files, "")); len(found) > 0 {
		return fail("ignore directives, lint disables or switched-off tests", strings.Join(found, "\n"))
	}
	return pass("no ignore directives, lint disables or switched-off tests")
}

func (c *checker) sameSources(before string) stepResult {
	files, err := repositoryFiles(c.sys)
	if err != nil {
		return fail(err.Error(), "")
	}
	if c.hash(files) != before {
		return fail("the repository files changed during the check; the results do not describe one version", "")
	}
	return pass("the repository files did not change during the check")
}

// contents reads the listed files under prefix; a listed file deleted from the working tree is skipped.
func (c *checker) contents(files []string, prefix string) map[string]string {
	out := map[string]string{}
	for _, f := range files {
		if !strings.HasPrefix(f, prefix) {
			continue
		}
		if data, err := c.sys.ReadFile(f); err == nil {
			out[f] = string(data)
		}
	}
	return out
}

func (c *checker) hash(files []string) string {
	h := sha256.New()
	contents := c.contents(files, "")
	for _, name := range sortedKeys(contents) {
		fmt.Fprintf(h, "%s\x00%s\x00", name, contents[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}
