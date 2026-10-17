package swarm

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
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

func TestClientIsTheEngineClientOfThePeer(t *testing.T) {
	p := peer(t, Options{})
	if p.Client() == nil || p.Client() != p.client {
		t.Fatal("Client does not return the peer's engine client")
	}
}
