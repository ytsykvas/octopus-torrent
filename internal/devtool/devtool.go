package devtool

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const usage = `usage: devtool workspace setup | workspace run | check-docs | codex-agents | check-plan | plan-nav | check [offline | fast | deps]`

// System is the boundary to the operating system, replaced by a fake in tests.
type System interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	// Run starts a program with the caller's standard streams and waits for it.
	Run(name string, args ...string) error
	// Output starts a program and returns its standard output.
	Output(name string, args ...string) (string, error)
	// Exec replaces the current process with the program at path; it returns only on failure.
	Exec(path string, argv []string) error
	// Capture starts a program and returns its standard output and error combined.
	Capture(name string, args ...string) (string, error)
	RemoveAll(path string) error
	MkdirAll(path string) error
	// ReadDir returns the names of the entries in a directory.
	ReadDir(path string) ([]string, error)
	Sleep(d time.Duration)
	// Get downloads a URL; a status other than 200 is an error.
	Get(url string) ([]byte, error)
	Now() time.Time
}

// Main dispatches a devtool command and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer, getenv func(string) string, sys System) int {
	command := strings.Join(args, " ")
	if mode, ok := checkModes[command]; ok {
		return runCheck(mode, stdout, sys)
	}
	var err error
	switch command {
	case "workspace setup":
		err = workspaceSetup(sys)
	case "workspace run":
		err = workspaceRun(getenv, sys)
	case "check-docs":
		err = checkDocs(stdout, sys)
	case "codex-agents":
		err = codexAgents(sys)
	case "check-plan":
		err = checkPlan(stdout, sys)
	case "plan-nav":
		err = planNav(stdout, sys)
	default:
		fmt.Fprintln(stderr, usage)
		return 64
	}
	if err != nil {
		fmt.Fprintf(stderr, "devtool: %v\n", err)
		return 1
	}
	return 0
}

// workspaceSetup prepares a fresh worktree: locked frontend dependencies, the frontend/dist that
// main.go embeds, and the Go module cache.
func workspaceSetup(sys System) error {
	steps := [][]string{
		{"npm", "--prefix", "frontend", "ci"},
		{"npm", "--prefix", "frontend", "run", "build"},
		{"go", "mod", "download"},
	}
	for _, step := range steps {
		if err := sys.Run(step[0], step[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

// workspaceRun becomes `wails dev` on the runner's port. The wails binary is resolved first and the
// process replaced, so the runner's PID is wails itself: its signals go straight to wails and no idle
// go process stays in between. (`go tool` would forward SIGINT/SIGTERM too; nothing survives SIGKILL
// either way — wails, Vite and the dev app are then left running.)
func workspaceRun(getenv func(string) string, sys System) error {
	port := getenv("OCTOPUS_PORT")
	if port == "" {
		port = getenv("CONDUCTOR_PORT")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("set OCTOPUS_PORT or CONDUCTOR_PORT to a TCP port (1-65535), got " + strconv.Quote(port))
	}
	out, err := sys.Output("go", "tool", "-n", "wails")
	if err != nil {
		return fmt.Errorf("locate wails: %w", err)
	}
	wails := strings.TrimSpace(out)
	return sys.Exec(wails, []string{wails, "dev", "-devserver", "localhost:" + port})
}

// repositoryFiles lists tracked and untracked, not ignored, files relative to the repository root.
// -z keeps every name as it is: the plain listing quotes names with unusual characters.
func repositoryFiles(sys System) ([]string, error) {
	out, err := sys.Output("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list repository files: %w", err)
	}
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 }), nil
}

// checkDocs verifies every relative Markdown link and every .claude/**.md pointer in the agent and
// runner configuration.
func checkDocs(stdout io.Writer, sys System) error {
	summary, dead, err := docsCheck(sys)
	if err != nil {
		return err
	}
	for _, line := range dead {
		fmt.Fprintln(stdout, line)
	}
	if len(dead) > 0 {
		return fmt.Errorf("%d dead links or pointers", len(dead))
	}
	fmt.Fprintln(stdout, "pass   "+summary)
	return nil
}

// docsCheck returns the dead links and pointers, and the summary printed when there are none.
func docsCheck(sys System) (string, []string, error) {
	files, err := repositoryFiles(sys)
	if err != nil {
		return "", nil, err
	}
	exists := listedPath(files)
	docs, sources := map[string]string{}, map[string]string{}
	for _, f := range files {
		content, err := sys.ReadFile(f)
		if err != nil {
			continue // listed but deleted from the working tree
		}
		if strings.HasSuffix(f, ".md") {
			docs[f] = string(content)
		}
		if f == "CLAUDE.md" || f == "AGENTS.md" || hasAnyPrefix(f, ".claude/", ".codex/", ".octopus/", ".conductor/", "docs/") {
			sources[f] = string(content)
		}
	}
	dead := append(CheckLinks(docs, exists), CheckPointers(sources, exists)...)
	return fmt.Sprintf("every relative link and .claude pointer resolves (%d Markdown files)", len(docs)), dead, nil
}

// listedPath reports whether p is one of the listed files or a directory containing one.
func listedPath(files []string) func(string) bool {
	return func(p string) bool {
		for _, f := range files {
			if f == p || strings.HasPrefix(f, p+"/") {
				return true
			}
		}
		return false
	}
}

// planFiles reads the task files, the plan README and the requirements.
func planFiles(sys System) (map[string]string, func(string) bool, error) {
	files, err := repositoryFiles(sys)
	if err != nil {
		return nil, nil, err
	}
	plan := map[string]string{}
	for _, f := range files {
		if strings.HasPrefix(f, tasksDir) || f == requirementsFile {
			if content, err := sys.ReadFile(f); err == nil {
				plan[f] = string(content)
			}
		}
	}
	return plan, listedPath(files), nil
}

func reportPlanProblems(stdout io.Writer, problems []string) error {
	for _, p := range problems {
		fmt.Fprintln(stdout, "PLAN "+p)
	}
	return fmt.Errorf("%d plan problems", len(problems))
}

// checkPlan verifies the task plan and that its navigation table matches the task files.
func checkPlan(stdout io.Writer, sys System) error {
	summary, problems, err := planCheck(sys)
	switch {
	case err != nil:
		return err
	case len(problems) > 0:
		return reportPlanProblems(stdout, problems)
	}
	fmt.Fprintln(stdout, "pass   "+summary)
	return nil
}

// planCheck returns the plan problems, and the summary printed when there are none.
func planCheck(sys System) (string, []string, error) {
	files, exists, err := planFiles(sys)
	if err != nil {
		return "", nil, err
	}
	if problems := CheckPlan(files, exists); len(problems) > 0 {
		return "", problems, nil
	}
	summary, _ := PlanSummary(files)
	return "plan: " + summary + "; navigation up to date", nil, nil
}

// planNav regenerates the navigation block of the plan README from the task files.
func planNav(stdout io.Writer, sys System) error {
	files, _, err := planFiles(sys)
	if err != nil {
		return err
	}
	readme, problems := RenderNavigation(files)
	if len(problems) > 0 {
		return reportPlanProblems(stdout, problems)
	}
	if readme == files[tasksReadme] {
		fmt.Fprintln(stdout, "plan-nav: navigation already up to date")
		return nil
	}
	if err := sys.WriteFile(tasksReadme, []byte(readme)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "plan-nav: navigation regenerated")
	return nil
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// codexAgents regenerates .codex/agents/*.toml from .claude/agents/*.md.
func codexAgents(sys System) error {
	files, err := repositoryFiles(sys)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !strings.HasPrefix(f, ".claude/agents/") || !strings.HasSuffix(f, ".md") {
			continue
		}
		content, err := sys.ReadFile(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		toml, err := CodexAgentTOML(string(content))
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(f, ".claude/agents/"), ".md")
		if err := sys.WriteFile(".codex/agents/"+name+".toml", []byte(toml)); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}
