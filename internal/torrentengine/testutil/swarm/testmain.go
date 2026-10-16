package swarm

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"
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
	for _, name := range []string{"TORRENT_STORAGE_DEFAULT_FILE_IO", "GOPPROF"} {
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
