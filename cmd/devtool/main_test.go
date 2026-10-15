package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"octopustorrent/internal/devtool"
)

// TestMain removes the shared test binary after every test has used it.
func TestMain(m *testing.M) {
	code := m.Run()
	if builtBinary != "" {
		os.RemoveAll(filepath.Dir(builtBinary))
	}
	os.Exit(code)
}

var (
	buildOnce   sync.Once
	builtBinary string
	builtEnv    []string
	buildErr    error
)

// buildDevtool compiles this command once per test run. Under `go test -cover` the binary is
// instrumented and writes its counters into the test's coverage directory, so main() is measured
// like the rest of the code.
func buildDevtool(t *testing.T) (binary string, env []string) {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "devtool-test")
		if err != nil {
			buildErr = err
			return
		}
		builtBinary = filepath.Join(dir, "devtool")
		args := []string{"build", "-o", builtBinary}
		builtEnv = os.Environ()
		if mode := testing.CoverMode(); mode != "" {
			args = append(args, "-cover", "-covermode="+mode, "-coverpkg=octopustorrent/...")
			if dir := flag.Lookup("test.gocoverdir"); dir != nil && dir.Value.String() != "" {
				builtEnv = append(builtEnv, "GOCOVERDIR="+dir.Value.String())
			}
		}
		if out, err := exec.Command("go", append(args, ".")...).CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return builtBinary, builtEnv
}

// writeScript creates an executable shell script.
func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRunReplacesTheProcessWithWails(t *testing.T) {
	binary, env := buildDevtool(t)
	bin, record := t.TempDir(), filepath.Join(t.TempDir(), "wails.args")
	writeScript(t, filepath.Join(bin, "wails"), `echo "$$ $*" > "`+record+`"`+"\n")
	writeScript(t, filepath.Join(bin, "go"), `echo "`+filepath.Join(bin, "wails")+`"`+"\n")

	cmd := exec.Command(binary, "workspace", "run")
	cmd.Env = append(env, "PATH="+bin+":"+os.Getenv("PATH"), "OCTOPUS_PORT=41000")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("workspace run: %v\n%s", err, out)
	}
	got, err := os.ReadFile(record)
	want := strconv.Itoa(cmd.Process.Pid) + " dev -devserver localhost:41000\n"
	if err != nil || string(got) != want {
		t.Errorf("wails recorded %q (%v), want %q: same pid proves the process was replaced", got, err, want)
	}
}

// The runner scripts are one-line wrappers; running them against a fake go proves every line.
func TestRunnerScriptsBuildDevtoolAndHandOverToIt(t *testing.T) {
	for _, command := range []string{"setup", "run"} {
		worktree, bin := t.TempDir(), t.TempDir()
		record := filepath.Join(worktree, "calls")
		writeScript(t, filepath.Join(bin, "go"), `echo "go $*" >> "`+record+`"
mkdir -p build/bin
printf '#!/bin/sh\necho "devtool $*" >> "%s"\n' "`+record+`" > build/bin/devtool
chmod +x build/bin/devtool
`)
		script, err := filepath.Abs(filepath.Join("..", "..", ".octopus", "scripts", command+".sh"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", script)
		cmd.Dir = worktree
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s.sh: %v\n%s", command, err, out)
		}
		got, _ := os.ReadFile(record)
		want := "go build -o build/bin/devtool ./cmd/devtool\ndevtool workspace " + command + "\n"
		if string(got) != want {
			t.Errorf("%s.sh calls = %q, want %q", command, got, want)
		}
	}
}

// The Codex agents are generated from the Claude Code agents; a hand edit on either side shows here.
func TestCodexAgentMirrorsAreUpToDate(t *testing.T) {
	agents, err := filepath.Glob(filepath.Join("..", "..", ".claude", "agents", "*.md"))
	if err != nil || len(agents) == 0 {
		t.Fatalf("no Claude agents found: %v", err)
	}
	for _, agent := range agents {
		source, err := os.ReadFile(agent)
		if err != nil {
			t.Fatal(err)
		}
		want, err := devtool.CodexAgentTOML(string(source))
		if err != nil {
			t.Fatalf("%s: %v", agent, err)
		}
		name := strings.TrimSuffix(filepath.Base(agent), ".md")
		got, err := os.ReadFile(filepath.Join("..", "..", ".codex", "agents", name+".toml"))
		if err != nil || string(got) != want {
			t.Errorf(".codex/agents/%s.toml is out of date (%v); run `go tool devtool codex-agents`", name, err)
		}
	}
}

// The real plan must stay consistent: the binary checks the repository it is built from.
func TestThePlanOfThisRepositoryIsConsistent(t *testing.T) {
	binary, env := buildDevtool(t)
	cmd := exec.Command(binary, "check-plan")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.HasPrefix(string(out), "pass   plan: ") {
		t.Errorf("check-plan: %v\n%s", err, out)
	}
}
