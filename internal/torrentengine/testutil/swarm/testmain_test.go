package swarm

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"octopustorrent/internal/torrentengine/classicio"
)

type runner func() int

func (r runner) Run() int { return r() }

func TestMainRefusesAnEnvironmentThatChangesTheEngine(t *testing.T) {
	for _, name := range []string{"TORRENT_STORAGE_DEFAULT_FILE_IO", "GOPPROF"} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			replace[io.Writer](t, &mainOutput, &out)
			t.Setenv(name, "x")
			ran := false
			if code := Main(runner(func() int { ran = true; return 0 })); code != 1 || ran {
				t.Fatalf("Main = %d, ran tests %v; want 1 without running them", code, ran)
			}
			if !strings.Contains(out.String(), name+`="x" changes what the engine does`) {
				t.Errorf("output %q does not name %s", out.String(), name)
			}
		})
	}
}

func TestMainReturnsTheTestsResultWhenNoEngineIsLeft(t *testing.T) {
	for _, want := range []int{0, 3} {
		if code := Main(runner(func() int { return want })); code != want {
			t.Errorf("Main = %d, want the tests' %d", code, want)
		}
	}
}

func TestMainFailsWhenTheTestsLeftAnEngineRunning(t *testing.T) {
	var out bytes.Buffer
	replace[io.Writer](t, &mainOutput, &out)
	replace(t, &mainLimit, 100*time.Millisecond)
	code := Main(runner(func() int {
		peer(t, Options{}) // closed only by the cleanup, after Main has looked
		return 0
	}))
	if code != 1 || !strings.Contains(out.String(), "after all tests: engine goroutines still running") {
		t.Errorf("Main = %d with output %q; want 1 and the engine stacks", code, out.String())
	}
}

func fakeProcess(env map[string]string, execErr error, calls *int) classicio.Process {
	return classicio.Process{
		LookupEnv:  func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		Executable: func() (string, error) { return "/apps/test", nil },
		Exec:       func(string, []string, []string) error { *calls++; return execErr },
	}
}

func TestMainClassicIORunsTheTestsOnceClassicIOIsSet(t *testing.T) {
	for _, want := range []int{0, 3} {
		calls := 0
		code := mainClassicIO(runner(func() int { return want }), fakeProcess(map[string]string{classicio.Variable: "classic"}, nil, &calls))
		if code != want || calls != 0 {
			t.Errorf("mainClassicIO = %d with %d restarts, want the tests' %d and none", code, calls, want)
		}
	}
}

func TestMainClassicIOStillRefusesWhatElseChangesTheEngine(t *testing.T) {
	var out bytes.Buffer
	replace[io.Writer](t, &mainOutput, &out)
	t.Setenv("GOPPROF", "http")
	ran, calls := false, 0
	code := mainClassicIO(runner(func() int { ran = true; return 0 }), fakeProcess(map[string]string{classicio.Variable: "classic"}, nil, &calls))
	if code != 1 || ran || !strings.Contains(out.String(), `GOPPROF="http" changes what the engine does`) {
		t.Fatalf("mainClassicIO = %d, ran %v, output %q", code, ran, out.String())
	}
}

func TestMainClassicIOFailsWhenClassicIOCannotBeEnsured(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"another value":  {classicio.Variable: "mmap"},
		"a failed start": nil,
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			replace[io.Writer](t, &mainOutput, &out)
			ran, calls := false, 0
			code := mainClassicIO(runner(func() int { ran = true; return 0 }), fakeProcess(env, errors.New("exec failed"), &calls))
			if code != 1 || ran || !strings.Contains(out.String(), classicio.Variable) {
				t.Fatalf("mainClassicIO = %d, ran %v, output %q", code, ran, out.String())
			}
		})
	}
}
