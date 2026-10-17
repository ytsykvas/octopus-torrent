package swarm

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"octopustorrent/internal/torrentengine/classicio"
)

// Runner runs a package's tests; *testing.M is one.
type Runner interface{ Run() int }

// mainOutput and mainLimit are seams for the tests of Main.
var (
	mainOutput io.Writer = os.Stderr
	mainLimit            = 5 * time.Second
)

// Main is the TestMain body of every package that starts engine clients:
//
//	func TestMain(m *testing.M) { os.Exit(swarm.Main(m)) }
//
// It refuses an environment that changes what the engine does (the tests record its default
// behaviour), runs the tests, and fails the package when any test left an engine goroutine behind.
func Main(m Runner) int {
	// TORRENT_STORAGE_DEFAULT_FILE_IO changes the engine's file IO; GOPPROF makes a linked engine
	// package open a pprof listener and write profiles into the home folder.
	return run(m, classicio.Variable, "GOPPROF")
}

// MainClassicIO is Main for a package whose tests need the engine's classic file IO (the engine's
// default, mmap, fails a chunk over a zero-length file): it starts the test binary again with
// TORRENT_STORAGE_DEFAULT_FILE_IO=classic when the variable is not set, because the engine reads it
// before any test code runs. Any other value, and GOPPROF, are refused.
func MainClassicIO(m Runner) int {
	return mainClassicIO(m, classicio.Current())
}

func mainClassicIO(m Runner, p classicio.Process) int {
	if err := classicio.Require(p); err != nil {
		fmt.Fprintln(mainOutput, err)
		return 1
	}
	return run(m, "GOPPROF")
}

// run refuses an environment that sets one of the named variables, runs the tests, and fails the
// package when any test left an engine goroutine behind.
func run(m Runner, refused ...string) int {
	for _, name := range refused {
		if v, set := os.LookupEnv(name); set {
			fmt.Fprintf(mainOutput, "%s=%q changes what the engine does; unset it\n", name, v)
			return 1
		}
	}
	code := m.Run()
	ctx, cancel := context.WithTimeout(context.Background(), mainLimit)
	defer cancel()
	if err := WaitEngineStopped(ctx); err != nil {
		fmt.Fprintln(mainOutput, "after all tests:", err)
		return 1
	}
	return code
}
