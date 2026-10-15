package devtool

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeSystem records every process it is asked to start and answers from canned values.
type fakeSystem struct {
	files     fakeFiles
	runErr    map[string]error
	output    string
	outputErr error
	execErr   error
	writeErr  error
	written   map[string]string
	calls     []string
	// results answers Output and Capture per joined command line, in order; the last answer repeats.
	// Output falls back to output/outputErr, Capture to an empty success.
	results   map[string][]fakeResult
	dirs      map[string][]string
	dirErr    error
	mkdirErr  error
	removeErr map[string]error
}

type fakeResult struct {
	out string
	err error
}

func (s *fakeSystem) answer(call string) (fakeResult, bool) {
	queue := s.results[call]
	if len(queue) == 0 {
		return fakeResult{}, false
	}
	if len(queue) > 1 {
		s.results[call] = queue[1:]
	}
	return queue[0], true
}

func (s *fakeSystem) WriteFile(name string, data []byte) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.written == nil {
		s.written = map[string]string{}
	}
	s.written[name] = string(data)
	return nil
}

func (s *fakeSystem) ReadFile(name string) ([]byte, error) { return s.files.read(name) }

func (s *fakeSystem) Run(name string, args ...string) error {
	call := strings.Join(append([]string{name}, args...), " ")
	s.calls = append(s.calls, "run "+call)
	return s.runErr[call]
}

func (s *fakeSystem) Output(name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	s.calls = append(s.calls, "output "+call)
	if r, ok := s.answer(call); ok {
		return r.out, r.err
	}
	return s.output, s.outputErr
}

func (s *fakeSystem) Capture(name string, args ...string) (string, error) {
	call := strings.Join(append([]string{name}, args...), " ")
	s.calls = append(s.calls, "capture "+call)
	r, _ := s.answer(call)
	return r.out, r.err
}

func (s *fakeSystem) RemoveAll(path string) error {
	s.calls = append(s.calls, "remove "+path)
	return s.removeErr[path]
}

func (s *fakeSystem) MkdirAll(path string) error {
	s.calls = append(s.calls, "mkdir "+path)
	return s.mkdirErr
}

func (s *fakeSystem) ReadDir(path string) ([]string, error) {
	s.calls = append(s.calls, "readdir "+path)
	if s.dirErr != nil {
		return nil, s.dirErr
	}
	return s.dirs[path], nil
}

func (s *fakeSystem) Sleep(d time.Duration) { s.calls = append(s.calls, "sleep "+d.String()) }

func (s *fakeSystem) Exec(path string, argv []string) error {
	s.calls = append(s.calls, "exec "+path+" "+strings.Join(argv, " "))
	return s.execErr
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func runMain(t *testing.T, sys *fakeSystem, getenv func(string) string, stdin string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &stdout, &stderr, getenv, sys)
	if stdout.Len() != 0 {
		t.Errorf("unexpected stdout: %q", stdout.String())
	}
	return code, stderr.String()
}

func TestHookAllowsWithExitZeroAndNoOutput(t *testing.T) {
	sys := &fakeSystem{files: guardedRepo}
	code, stderr := runMain(t, sys, env(nil), `{"cwd":"/repo","tool_name":"Bash","tool_input":{"command":"npm --prefix frontend ci"}}`, "hook", "release-guard")
	if code != 0 || stderr != "" {
		t.Errorf("got code %d stderr %q, want 0 and nothing", code, stderr)
	}
}

func TestHookDeniesWithExitTwoAndReason(t *testing.T) {
	sys := &fakeSystem{files: guardedRepo}
	code, stderr := runMain(t, sys, env(nil), `{"cwd":"/repo","tool_name":"Bash","tool_input":{"command":"npx cowsay"}}`, "hook", "release-guard")
	if code != 2 || !strings.Contains(stderr, "npx") || !strings.HasPrefix(stderr, "release-guard: ") {
		t.Errorf("got code %d stderr %q, want 2 and a release-guard reason", code, stderr)
	}
}

// Claude Code reads an "ask" decision from JSON on stdout with exit status 0.
func TestHookAsksTheOwnerThroughJSONOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	input := `{"tool_name":"Write","tool_input":{"file_path":"/repo/.claude/settings.json","content":"{}"}}`
	code := Main([]string{"hook", "release-guard"}, strings.NewReader(input), &stdout, &stderr, env(nil), &fakeSystem{})
	var out struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}
	h := out.HookSpecificOutput
	if code != 0 || stderr.Len() != 0 || h.HookEventName != "PreToolUse" || h.PermissionDecision != "ask" ||
		!strings.HasPrefix(h.PermissionDecisionReason, "release-guard: ") {
		t.Errorf("got code %d stderr %q output %+v, want 0, nothing, a PreToolUse ask with a release-guard reason", code, stderr.String(), h)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// If the ask cannot be delivered, the call is blocked rather than silently allowed.
func TestHookBlocksWhenTheAskCannotBeWritten(t *testing.T) {
	var stderr bytes.Buffer
	input := `{"tool_name":"Write","tool_input":{"file_path":"/repo/.codex/hooks.json","content":"{}"}}`
	code := Main([]string{"hook", "release-guard"}, strings.NewReader(input), failingWriter{}, &stderr, env(nil), &fakeSystem{})
	if code != 2 || !strings.Contains(stderr.String(), "broken pipe") {
		t.Errorf("got code %d stderr %q, want 2 naming the write error", code, stderr.String())
	}
}

func TestHookReadsACommandGivenAsAnArgumentList(t *testing.T) {
	sys := &fakeSystem{files: guardedRepo}
	code, stderr := runMain(t, sys, env(nil), `{"cwd":"/repo","tool_name":"Bash","tool_input":{"command":["npx","cowsay"]}}`, "hook", "release-guard")
	if code != 2 || !strings.Contains(stderr, "npx") {
		t.Errorf("got code %d stderr %q, want 2 naming npx", code, stderr)
	}
}

func TestHookReadsThePermissionMode(t *testing.T) {
	input := `{"permission_mode":"auto","tool_name":"Write","tool_input":{"file_path":"/repo/.codex/hooks.json","content":"{}"}}`
	code, stderr := runMain(t, &fakeSystem{}, env(nil), input, "hook", "release-guard")
	if code != 2 || !strings.Contains(stderr, "permission mode") {
		t.Errorf("got code %d stderr %q, want 2 naming the permission mode", code, stderr)
	}
}

// Joining an argument list must keep each argument one word, so a -c script stays whole.
func TestHookKeepsEachArgumentOfAListWhole(t *testing.T) {
	sys := &fakeSystem{files: guardedRepo}
	code, stderr := runMain(t, sys, env(nil), `{"cwd":"/repo","tool_name":"Bash","tool_input":{"command":["bash","-c","npm i -g x"]}}`, "hook", "release-guard")
	if code != 2 || !strings.Contains(stderr, "global") {
		t.Errorf("got code %d stderr %q, want 2 naming the global install", code, stderr)
	}
}

// A tool the guard checks, with input it cannot read, is blocked rather than let through unchecked.
func TestHookBlocksAKnownToolWithUnreadableInput(t *testing.T) {
	for _, input := range []string{
		`{"tool_name":"Bash","tool_input":"npx cowsay"}`,
		`{"tool_name":"Bash","tool_input":{"command":42}}`,
	} {
		code, stderr := runMain(t, &fakeSystem{}, env(nil), input, "hook", "release-guard")
		if code != 2 || !strings.Contains(stderr, "cannot read") {
			t.Errorf("%s: got code %d stderr %q, want 2", input, code, stderr)
		}
	}
	code, stderr := runMain(t, &fakeSystem{}, env(nil), `{"tool_name":"Read","tool_input":"anything"}`, "hook", "release-guard")
	if code != 0 || stderr != "" {
		t.Errorf("other tool: got code %d stderr %q, want 0", code, stderr)
	}
}

func TestHookWithUnreadableInputWarnsButDoesNotBlock(t *testing.T) {
	code, stderr := runMain(t, &fakeSystem{}, env(nil), "not json", "hook", "release-guard")
	if code != 0 || !strings.Contains(stderr, "could not parse") {
		t.Errorf("got code %d stderr %q, want 0 and a parse warning", code, stderr)
	}
}

func TestWorkspaceSetupInstallsBuildsAndDownloadsInOrder(t *testing.T) {
	sys := &fakeSystem{}
	code, stderr := runMain(t, sys, env(nil), "", "workspace", "setup")
	want := []string{
		"run npm --prefix frontend ci",
		"run npm --prefix frontend run build",
		"run go mod download",
	}
	if code != 0 || stderr != "" || strings.Join(sys.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("got code %d stderr %q calls %q, want 0, nothing, %q", code, stderr, sys.calls, want)
	}
}

func TestWorkspaceSetupStopsAtTheFirstFailure(t *testing.T) {
	sys := &fakeSystem{runErr: map[string]error{"npm --prefix frontend run build": errors.New("exit status 1")}}
	code, stderr := runMain(t, sys, env(nil), "", "workspace", "setup")
	if code != 1 || len(sys.calls) != 2 || !strings.Contains(stderr, "npm --prefix frontend run build: exit status 1") {
		t.Errorf("got code %d calls %q stderr %q, want 1 after two calls with the failing step named", code, sys.calls, stderr)
	}
}

func TestWorkspaceRunReplacesItselfWithWailsDevOnTheGivenPort(t *testing.T) {
	for name, vars := range map[string]map[string]string{
		"octopus":   {"OCTOPUS_PORT": "41000", "CONDUCTOR_PORT": "42000"},
		"conductor": {"CONDUCTOR_PORT": "41000"},
	} {
		sys := &fakeSystem{output: "/cache/wails\n"}
		code, stderr := runMain(t, sys, env(vars), "", "workspace", "run")
		want := []string{"output go tool -n wails", "exec /cache/wails /cache/wails dev -devserver localhost:41000"}
		if code != 0 || stderr != "" || strings.Join(sys.calls, "\n") != strings.Join(want, "\n") {
			t.Errorf("%s: got code %d stderr %q calls %q, want %q", name, code, stderr, sys.calls, want)
		}
	}
}

func TestWorkspaceRunRejectsAMissingOrInvalidPort(t *testing.T) {
	for _, port := range []string{"", "http", "0", "65536"} {
		sys := &fakeSystem{}
		code, stderr := runMain(t, sys, env(map[string]string{"OCTOPUS_PORT": port}), "", "workspace", "run")
		if code != 1 || len(sys.calls) != 0 || !strings.Contains(stderr, "port") {
			t.Errorf("port %q: got code %d calls %q stderr %q, want 1, no calls, a port error", port, code, sys.calls, stderr)
		}
	}
}

func TestWorkspaceRunReportsWhyWailsCouldNotStart(t *testing.T) {
	ports := env(map[string]string{"OCTOPUS_PORT": "41000"})
	sys := &fakeSystem{outputErr: errors.New("no tool")}
	if code, stderr := runMain(t, sys, ports, "", "workspace", "run"); code != 1 || !strings.Contains(stderr, "no tool") {
		t.Errorf("tool lookup failure: got code %d stderr %q", code, stderr)
	}
	sys = &fakeSystem{output: "/cache/wails", execErr: errors.New("exec format error")}
	if code, stderr := runMain(t, sys, ports, "", "workspace", "run"); code != 1 || !strings.Contains(stderr, "exec format error") {
		t.Errorf("exec failure: got code %d stderr %q", code, stderr)
	}
}

func TestMainExplainsUsageForUnknownCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"hook"}, {"workspace", "archive"}} {
		code, stderr := runMain(t, &fakeSystem{}, env(nil), "", args...)
		if code != 64 || !strings.Contains(stderr, "usage: devtool") {
			t.Errorf("args %q: got code %d stderr %q, want 64 and usage", args, code, stderr)
		}
	}
}

func runMainWithOutput(sys *fakeSystem, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Main(args, strings.NewReader(""), &stdout, &stderr, env(nil), sys)
	return code, stdout.String(), stderr.String()
}

func TestCheckDocsPassesWhenEverythingResolves(t *testing.T) {
	sys := &fakeSystem{
		output: "README.md\ndocs/a.md\n.conductor/settings.toml\n.claude/docs/actions/review.md\ndocs/deleted.md\n",
		files: fakeFiles{
			"README.md":                      "# R\n[a](docs/a.md#a)\n",
			"docs/a.md":                      "# A\n",
			".conductor/settings.toml":       "Rules: .claude/docs/actions/review.md",
			".claude/docs/actions/review.md": "# Review\n",
		},
	}
	code, stdout, stderr := runMainWithOutput(sys, "check-docs")
	if code != 0 || stderr != "" || !strings.HasPrefix(stdout, "pass") || sys.calls[0] != "output git ls-files --cached --others --exclude-standard" {
		t.Errorf("got code %d stdout %q stderr %q calls %q", code, stdout, stderr, sys.calls)
	}
}

func TestCheckDocsFailsAndListsEveryDeadReference(t *testing.T) {
	sys := &fakeSystem{
		output: "README.md\n.octopus/instructions/review.md\n",
		files: fakeFiles{
			"README.md":                       "# R\n[gone](docs/gone.md)\n",
			".octopus/instructions/review.md": "Read .claude/docs/actions/gone.md",
		},
	}
	code, stdout, stderr := runMainWithOutput(sys, "check-docs")
	want := "DEAD README.md: docs/gone.md\nDEAD .octopus/instructions/review.md: .claude/docs/actions/gone.md\n"
	if code != 1 || stdout != want || !strings.Contains(stderr, "2 dead") {
		t.Errorf("got code %d stdout %q stderr %q, want 1, %q and a count", code, stdout, stderr, want)
	}
}

func TestDocToolsReportAFailingFileListing(t *testing.T) {
	for _, command := range []string{"check-docs", "codex-agents", "check-plan", "plan-nav"} {
		code, _, stderr := runMainWithOutput(&fakeSystem{outputErr: errors.New("not a git repository")}, command)
		if code != 1 || !strings.Contains(stderr, "not a git repository") {
			t.Errorf("%s: got code %d stderr %q", command, code, stderr)
		}
	}
}

func TestCodexAgentsWritesOneMirrorPerClaudeAgent(t *testing.T) {
	sys := &fakeSystem{
		output: ".claude/agents/code-reviewer.md\n.claude/docs/README.md\nREADME.md\n",
		files:  fakeFiles{".claude/agents/code-reviewer.md": reviewerAgent},
	}
	code, stdout, stderr := runMainWithOutput(sys, "codex-agents")
	want, err := CodexAgentTOML(reviewerAgent)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || stdout != "" || stderr != "" || len(sys.written) != 1 || sys.written[".codex/agents/code-reviewer.toml"] != want {
		t.Errorf("got code %d stdout %q stderr %q written %q", code, stdout, stderr, sys.written)
	}
}

func TestCodexAgentsReportsTheAgentItCannotMirror(t *testing.T) {
	listing := ".claude/agents/broken.md\n"
	cases := map[string]*fakeSystem{
		"unreadable": {output: listing},
		"invalid":    {output: listing, files: fakeFiles{".claude/agents/broken.md": "no front matter"}},
		"unwritable": {output: listing, files: fakeFiles{".claude/agents/broken.md": reviewerAgent}, writeErr: errors.New("read-only file system")},
	}
	for name, sys := range cases {
		code, _, stderr := runMainWithOutput(sys, "codex-agents")
		if code != 1 || !strings.Contains(stderr, "broken") {
			t.Errorf("%s: got code %d stderr %q, want 1 naming the agent", name, code, stderr)
		}
	}
}

// planSystem serves a plan fixture as the repository.
func planSystem(files map[string]string) *fakeSystem {
	return &fakeSystem{output: strings.Join(sortedKeys(files), "\n") + "\n", files: fakeFiles(files)}
}

func TestCheckPlanPassesOnAConsistentPlan(t *testing.T) {
	code, stdout, stderr := runMainWithOutput(planSystem(planFixture()), "check-plan")
	want := "pass   plan: 3 tasks, 3 dependencies, 3 requirements, 2 scenarios; navigation up to date\n"
	if code != 0 || stdout != want || stderr != "" {
		t.Errorf("got code %d stdout %q stderr %q, want 0 and %q", code, stdout, stderr, want)
	}
}

func TestCheckPlanListsEveryProblem(t *testing.T) {
	files := edit(t, "product/tasks/003-check.md", "`planned`", "`started`")
	code, stdout, stderr := runMainWithOutput(planSystem(files), "check-plan")
	if code != 1 || !strings.Contains(stdout, "PLAN 003-check.md: unknown status started\n") || !strings.Contains(stderr, "plan problems") {
		t.Errorf("got code %d stdout %q stderr %q", code, stdout, stderr)
	}
}

func TestPlanNavRewritesOnlyAStaleBlock(t *testing.T) {
	stale := edit(t, "product/tasks/003-check.md", "Check everything", "Check every gate")
	sys := planSystem(stale)
	code, stdout, stderr := runMainWithOutput(sys, "plan-nav")
	if code != 0 || stdout != "plan-nav: navigation regenerated\n" || stderr != "" || !strings.Contains(sys.written["product/tasks/README.md"], "| Check every gate |") {
		t.Errorf("got code %d stdout %q stderr %q written %q", code, stdout, stderr, sys.written)
	}
	sys = planSystem(planFixture())
	code, stdout, _ = runMainWithOutput(sys, "plan-nav")
	if code != 0 || stdout != "plan-nav: navigation already up to date\n" || len(sys.written) != 0 {
		t.Errorf("up to date: got code %d stdout %q written %q", code, stdout, sys.written)
	}
}

func TestPlanNavRefusesAnUnreadablePlanOrAFailedWrite(t *testing.T) {
	broken := planSystem(edit(t, "product/tasks/003-check.md", "**Status:** `planned`  \n", ""))
	code, stdout, stderr := runMainWithOutput(broken, "plan-nav")
	if code != 1 || !strings.Contains(stdout, "PLAN 003-check.md") || len(broken.written) != 0 || !strings.Contains(stderr, "plan problems") {
		t.Errorf("unreadable: got code %d stdout %q stderr %q written %q", code, stdout, stderr, broken.written)
	}
	readOnly := planSystem(edit(t, "product/tasks/003-check.md", "Check everything", "Check every gate"))
	readOnly.writeErr = errors.New("read-only file system")
	if code, _, stderr := runMainWithOutput(readOnly, "plan-nav"); code != 1 || !strings.Contains(stderr, "read-only") {
		t.Errorf("unwritable: got code %d stderr %q", code, stderr)
	}
}
