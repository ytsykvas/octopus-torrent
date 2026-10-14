package devtool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const usage = `usage: devtool hook release-guard | workspace setup | workspace run | check-docs | codex-agents`

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
}

// Main dispatches a devtool command and returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string, sys System) int {
	var err error
	switch strings.Join(args, " ") {
	case "hook release-guard":
		return releaseGuardHook(stdin, stdout, stderr, sys)
	case "workspace setup":
		err = workspaceSetup(sys)
	case "workspace run":
		err = workspaceRun(getenv, sys)
	case "check-docs":
		err = checkDocs(stdout, sys)
	case "codex-agents":
		err = codexAgents(sys)
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

// releaseGuardHook follows the PreToolUse contract shared by Claude Code and Codex: exit 2 with the
// reason on stderr blocks the call, exit 0 lets it through. Input it cannot parse is let through with
// a warning, so a payload format change cannot lock the agent out of every tool.
func releaseGuardHook(stdin io.Reader, stdout, stderr io.Writer, sys System) int {
	ev, err := decodeHookEvent(stdin)
	if errors.Is(err, errUnreadableEvent) {
		fmt.Fprintf(stderr, "release-guard: could not parse the hook input, not checked: %v\n", err)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "release-guard: cannot read the %s input, blocked: %v\n", ev.ToolName, err)
		return 2
	}
	d := Decide(ev, sys.ReadFile)
	switch {
	case d.Deny:
		fmt.Fprintf(stderr, "release-guard: %s\n", d.Reason)
		return 2
	case d.Ask:
		// Claude Code reads "ask" from JSON on stdout; an agent that does not understand it lets the
		// call through, which is why weakening changes are denied outright and never only asked.
		var out struct {
			HookSpecificOutput struct {
				HookEventName            string `json:"hookEventName"`
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		out.HookSpecificOutput.HookEventName = "PreToolUse"
		out.HookSpecificOutput.PermissionDecision = "ask"
		out.HookSpecificOutput.PermissionDecisionReason = "release-guard: " + d.Reason
		if err := json.NewEncoder(stdout).Encode(out); err != nil {
			fmt.Fprintf(stderr, "release-guard: %v\n", err)
			return 2
		}
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
func repositoryFiles(sys System) ([]string, error) {
	out, err := sys.Output("git", "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list repository files: %w", err)
	}
	return strings.Fields(out), nil
}

// checkDocs verifies every relative Markdown link and every .claude/**.md pointer in the agent and
// runner configuration.
func checkDocs(stdout io.Writer, sys System) error {
	files, err := repositoryFiles(sys)
	if err != nil {
		return err
	}
	exists := func(p string) bool {
		for _, f := range files {
			if f == p || strings.HasPrefix(f, p+"/") {
				return true
			}
		}
		return false
	}
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
	for _, line := range dead {
		fmt.Fprintln(stdout, line)
	}
	if len(dead) > 0 {
		return fmt.Errorf("%d dead links or pointers", len(dead))
	}
	fmt.Fprintf(stdout, "pass   every relative link and .claude pointer resolves (%d Markdown files)\n", len(docs))
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

var errUnreadableEvent = errors.New("not a hook event")

// guardedTools are the tools whose input the guard inspects; their input must be readable.
var guardedTools = map[string]bool{"Bash": true, "Edit": true, "MultiEdit": true, "Write": true}

// decodeHookEvent reads the PreToolUse payload. A payload that is not JSON at all is
// errUnreadableEvent (let through with a warning, so a format change cannot lock agents out); a
// guarded tool whose input does not decode is an error that blocks the call. The command may be a
// string or a list of arguments.
func decodeHookEvent(r io.Reader) (HookEvent, error) {
	var raw struct {
		Cwd       string          `json:"cwd"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return HookEvent{}, fmt.Errorf("%w: %v", errUnreadableEvent, err)
	}
	ev := HookEvent{Cwd: raw.Cwd, ToolName: raw.ToolName}
	if !guardedTools[raw.ToolName] {
		return ev, nil
	}
	var input struct {
		ToolInput
		Command json.RawMessage `json:"command"`
	}
	if err := json.Unmarshal(raw.ToolInput, &input); err != nil {
		return ev, err
	}
	ev.ToolInput = input.ToolInput
	if len(input.Command) == 0 {
		return ev, nil
	}
	if err := json.Unmarshal(input.Command, &ev.ToolInput.Command); err == nil {
		return ev, nil
	}
	var argv []string
	if err := json.Unmarshal(input.Command, &argv); err != nil {
		return ev, errors.New("command is neither a string nor a list of strings")
	}
	ev.ToolInput.Command = strings.Join(argv, " ")
	return ev, nil
}
