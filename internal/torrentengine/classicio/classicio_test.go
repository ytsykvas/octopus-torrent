package classicio

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// execCall is one recorded call of a fake process's Exec.
type execCall struct {
	path string
	argv []string
	env  []string
}

// process builds a fake process: the variable (or none), the path of its executable, and an Exec
// that records its call instead of replacing the test process.
func process(env map[string]string, calls *[]execCall, execErr error) Process {
	return Process{
		LookupEnv:  func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Executable: func() (string, error) { return "/apps/OctopusTorrent", nil },
		Exec: func(path string, argv, envv []string) error {
			*calls = append(*calls, execCall{path, argv, envv})
			return execErr
		},
		Args:    []string{"OctopusTorrent", "-flag", "value"},
		Environ: []string{"HOME=/home/x", "PATH=/usr/bin"},
	}
}

func TestClassicIOAlreadySetNeedsNoRestart(t *testing.T) {
	var calls []execCall
	if err := Require(process(map[string]string{Variable: "classic"}, &calls, nil)); err != nil || len(calls) != 0 {
		t.Fatalf("Require = %v with %d exec calls, want nil and none", err, len(calls))
	}
}

func TestAnyOtherValueIsRefused(t *testing.T) {
	for _, value := range []string{"mmap", "", "Classic", "classic "} {
		var calls []execCall
		err := Require(process(map[string]string{Variable: value}, &calls, nil))
		if err == nil || !strings.Contains(err.Error(), Variable) || len(calls) != 0 {
			t.Fatalf("value %q: Require = %v with %d exec calls, want an error naming %s and no restart", value, err, len(calls), Variable)
		}
	}
}

func TestWhenUnsetTheProcessRestartsItselfWithTheVariableAndTheSameArguments(t *testing.T) {
	var calls []execCall
	err := Require(process(nil, &calls, errors.New("exec failed")))
	if len(calls) != 1 {
		t.Fatalf("%d exec calls, want 1", len(calls))
	}
	c := calls[0]
	if c.path != "/apps/OctopusTorrent" || !slices.Equal(c.argv, []string{"OctopusTorrent", "-flag", "value"}) {
		t.Fatalf("exec %q %q, want the same executable and arguments", c.path, c.argv)
	}
	want := []string{"HOME=/home/x", "PATH=/usr/bin", Variable + "=classic", Restarted + "=1"}
	if !slices.Equal(c.env, want) {
		t.Fatalf("environment %q, want %q", c.env, want)
	}
	if err == nil || !strings.Contains(err.Error(), "exec failed") {
		t.Fatalf("Require = %v, want the exec failure returned (exec returns only on failure)", err)
	}
}

func TestAnExecutableThatCannotBeFoundIsAnErrorAndNothingIsExecuted(t *testing.T) {
	var calls []execCall
	p := process(nil, &calls, nil)
	p.Executable = func() (string, error) { return "", errors.New("no executable") }
	if err := Require(p); err == nil || len(calls) != 0 {
		t.Fatalf("Require = %v with %d exec calls, want an error and none", err, len(calls))
	}
}

func TestCurrentIsTheRealProcess(t *testing.T) {
	p := Current()
	if p.LookupEnv == nil || p.Executable == nil || p.Exec == nil {
		t.Fatal("Current leaves a function unset")
	}
	if !slices.Equal(p.Args, os.Args) || !slices.Equal(p.Environ, os.Environ()) {
		t.Fatal("Current does not carry the process's arguments and environment")
	}
	if path, err := p.Executable(); err != nil || path == "" {
		t.Fatalf("Executable = %q, %v", path, err)
	}
}

func TestAProcessRestartedOnceDoesNotRestartAgainWhenTheVariableWasDropped(t *testing.T) {
	var calls []execCall
	err := Require(process(map[string]string{Restarted: "1"}, &calls, nil))
	if err == nil || len(calls) != 0 {
		t.Fatalf("Require = %v with %d exec calls, want an error and no second restart", err, len(calls))
	}
}

func TestAnExecThatReturnsWithoutAnErrorIsStillAnError(t *testing.T) {
	var calls []execCall
	err := Require(process(nil, &calls, nil))
	if err == nil || len(calls) != 1 {
		t.Fatalf("Require = %v with %d exec calls; a process that was not replaced must not carry on", err, len(calls))
	}
}
