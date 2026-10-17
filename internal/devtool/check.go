package devtool

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
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
// deps runs the network steps alone. A mode with network steps is a full mode: a mandatory step that
// did not run fails it.
type checkMode struct {
	app, network, depsOnly bool
	label                  string
}

var checkModes = map[string]checkMode{
	"check":         {app: true, network: true, label: "GATE"},
	"check offline": {app: true, label: "GATE"},
	"check fast":    {label: "GATE"},
	"check deps":    {network: true, depsOnly: true, label: "DEPENDENCIES"},
}

const (
	coverRoot        = "build/coverage"
	unitCoverDir     = coverRoot + "/go-unit"
	appCoverDir      = coverRoot + "/go-app"
	frontendCoverDir = "frontend/coverage"
	frontendReport   = frontendCoverDir + "/coverage-summary.json"
	npmLock          = "frontend/package-lock.json"
	reportTail       = 30
)

// npm is an npm command run in the frontend folder.
func npm(args ...string) []string {
	return append([]string{"npm", "--prefix", "frontend"}, args...)
}

type checkStep struct {
	name string
	run  func() stepResult
}

type checker struct {
	sys     System
	root    string
	mode    checkMode
	files   []string
	appRan  bool
	npmDocs map[string]npmDocument
	// ageExceptions are the versions the release-age step lets through younger than the minimum.
	ageExceptions map[string]string
}

func newChecker(sys System, mode checkMode) *checker {
	return &checker{sys: sys, mode: mode, npmDocs: map[string]npmDocument{}, ageExceptions: releaseAgeExceptions}
}

// runCheck runs every step of the mode in order, never stopping early, prints one line per step and
// returns the exit code: 1 if a step failed, or if a mandatory step did not run in the full gate.
func runCheck(mode checkMode, stdout io.Writer, sys System) int {
	c := newChecker(sys, mode)
	before, err := c.start()
	if err != nil {
		report(stdout, "repository", fail(err.Error(), ""))
		fmt.Fprintln(stdout, mode.label+" FAILED")
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
		fmt.Fprintln(stdout, mode.label+" FAILED")
		return 1
	case len(missing) > 0 && mode.network:
		fmt.Fprintln(stdout, mode.label+" FAILED: not run: "+strings.Join(missing, ", "))
		return 1
	case len(missing) > 0:
		fmt.Fprintln(stdout, mode.label+" INCOMPLETE: not run: "+strings.Join(missing, ", ")+"; only go tool devtool check accepts a change")
		return 0
	}
	fmt.Fprintln(stdout, mode.label+" PASSED")
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
func (c *checker) start() (map[string]string, error) {
	prefix, err := c.sys.Output("git", "rev-parse", "--show-prefix")
	if err != nil {
		return nil, fmt.Errorf("not a git repository: %w", err)
	}
	if strings.TrimSpace(prefix) != "" {
		return nil, errors.New("run the check from the repository root, not " + strings.TrimSpace(prefix))
	}
	root, err := c.sys.Output("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("find the repository root: %w", err)
	}
	c.root = strings.TrimSpace(root)
	if c.files, err = repositoryFiles(c.sys); err != nil {
		return nil, err
	}
	return c.fingerprints(c.files), nil
}

func (c *checker) steps() []checkStep {
	npmCI := npm("ci")
	if !c.mode.network {
		npmCI = append(npmCI, "--offline")
	}
	network := []checkStep{
		{"release age", c.network(c.releaseAges)},
		{"npm audit", c.network(c.audit(npm("audit")...))},
		{"govulncheck", c.network(c.audit("go", "tool", "govulncheck", "./..."))},
		{"updates", c.network(c.updates)},
	}
	if c.mode.depsOnly {
		return network
	}
	local := []checkStep{
		{"npm ci", c.tool(npmCI...)},
		{"npm cooldown", c.npmCooldown},
		{"go env", c.goEnv},
		{"typecheck", c.tool(npm("run", "typecheck")...)},
		{"lint", c.tool(npm("run", "lint")...)},
		{"frontend tests", c.frontendTests},
		{"frontend build", c.tool(npm("run", "build")...)},
		{"gofmt", c.gofmt},
		{"go vet", c.tool("go", "vet", "./...")},
		{"bindings", c.bindings},
		{"go tests", c.goTests},
		{".app coverage run", c.appRun},
		{"go coverage", c.goCoverage},
		{"escape hatches", c.escapeHatches},
		{"scripts", c.scripts},
		{"product isolation", c.isolation},
		{"release build", c.tool("go", "tool", "wails", "build", "-clean", "-trimpath")},
		{"docs", c.own(docsCheck)},
		{"plan", c.own(planCheck)},
	}
	pending := []checkStep{
		{"engine integration", notBuiltYetStep("task 018")},
		{"test-bridge UI tests", notBuiltYetStep("task 035")},
	}
	return slices.Concat(local, network, pending)
}

// notBuiltYetStep is a tier of the gate that a numbered task has yet to build.
func notBuiltYetStep(task string) func() stepResult {
	return func() stepResult { return notBuiltYet("not implemented yet: " + task) }
}

// gomod is the text of go.mod.
func (c *checker) gomod() string {
	b, _ := c.sys.ReadFile("go.mod")
	return string(b)
}

// module is the module path of go.mod.
func (c *checker) module() string {
	return GoModulePath(c.gomod())
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

// own runs one of devtool's own checks, which returns its summary or its problems.
func (c *checker) own(check func(System) (string, []string, error)) func() stepResult {
	return func() stepResult {
		summary, problems, err := check(c.sys)
		switch {
		case err != nil:
			return fail(err.Error(), "")
		case len(problems) > 0:
			return fail(fmt.Sprintf("%d problems", len(problems)), strings.Join(problems, "\n"))
		}
		return pass(summary)
	}
}

func (c *checker) frontendTests() stepResult {
	if err := c.sys.RemoveAll(frontendCoverDir); err != nil {
		return fail("remove the previous coverage report: "+err.Error(), "")
	}
	tests := npm("run", "test:coverage")
	if out, err := c.sys.Capture(tests[0], tests[1:]...); err != nil {
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

// bindings regenerates the bindings of every Wails app and compares the contents (not the file
// modes, which Wails itself changes) with what was there before. Each app has its own bindings
// directory: generation deletes the go/ part of the directory it writes.
func (c *checker) bindings() stepResult {
	before := map[string]map[string]string{}
	for _, a := range wailsApps {
		before[a.Name] = c.contents(c.files, a.Bindings)
	}
	for _, a := range wailsApps {
		if out, err := c.sys.Capture("env", "-C", a.Dir, "go", "tool", "wails", "generate", "module"); err != nil {
			return fail(a.Name+": go tool wails generate module: "+err.Error(), out)
		}
	}
	files, err := repositoryFiles(c.sys)
	if err != nil {
		return fail(err.Error(), "")
	}
	var changed, matched []string
	for _, a := range wailsApps {
		after := c.contents(files, a.Bindings)
		changed = append(changed, changedNames(before[a.Name], after)...)
		matched = append(matched, fmt.Sprintf("%s: %s (%d files)", a.Name, a.Bindings, len(after)))
	}
	if len(changed) > 0 {
		return fail("the committed bindings differ from the Go facade; commit the regenerated files", strings.Join(changed, "\n"))
	}
	return pass("bindings match the Go facades: " + strings.Join(matched, ", "))
}

func (c *checker) goTests() stepResult {
	if err := c.sys.RemoveAll(coverRoot); err != nil {
		return fail("remove the previous coverage profiles: "+err.Error(), "")
	}
	dir := filepath.Join(c.root, unitCoverDir)
	if err := c.sys.MkdirAll(dir); err != nil {
		return fail("create "+dir+": "+err.Error(), "")
	}
	return c.tool("go", "test", "-race", "-count=1", "-covermode=atomic", "-coverpkg=./...", "./...", "-args", "-test.gocoverdir="+dir)()
}

func (c *checker) appRun() stepResult {
	if !c.mode.app {
		return notRun("skipped by check fast")
	}
	r := appCoverageRun(c.sys, c.root, filepath.Join(c.root, appCoverDir), c.module())
	c.appRan = r.status == statusPass
	return r
}

// profile converts raw counters to text; a failed conversion leaves the profile empty, which the
// coverage check reports.
func (c *checker) profile(dir string) string {
	if _, err := c.sys.Capture("go", "tool", "covdata", "textfmt", "-i="+filepath.Join(c.root, dir), "-o", dir+".txt"); err != nil {
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
	sources := c.contents(c.files, "")
	problems, deferred := CheckGoCoverage(c.module(), sources, unit, app, c.appRan, appMains())
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

func (c *checker) scripts() stepResult {
	if found := CheckScripts(c.contents(c.files, "")); len(found) > 0 {
		return fail("shell scripts may only hand over to devtool; write the logic in Go", strings.Join(found, "\n"))
	}
	return pass("every shell script only hands over to devtool")
}

func (c *checker) sameSources(before map[string]string) stepResult {
	files, err := repositoryFiles(c.sys)
	if err != nil {
		return fail(err.Error(), "")
	}
	after := c.fingerprints(files)
	if changed := changedNames(before, after); len(changed) > 0 {
		return fail("the repository files changed during the check; the results do not describe one version", strings.Join(changed, "\n"))
	}
	return pass("the repository files did not change during the check")
}

// changedNames lists the names added to or changed in after, then the names missing from it.
func changedNames(before, after map[string]string) []string {
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
	return changed
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

// fingerprints returns the SHA-256 of every listed file's content.
func (c *checker) fingerprints(files []string) map[string]string {
	sums := map[string]string{}
	for name, content := range c.contents(files, "") {
		sum := sha256.Sum256([]byte(content))
		sums[name] = hex.EncodeToString(sum[:])
	}
	return sums
}
