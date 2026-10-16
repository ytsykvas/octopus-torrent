package swarm

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// TestMain refuses an environment that changes the engine's behaviour (the tests record the
// default), and fails the package when any test left an engine goroutine behind.
func TestMain(m *testing.M) {
	// TORRENT_STORAGE_DEFAULT_FILE_IO changes the engine's file IO; GOPPROF makes a linked engine
	// package open a pprof listener and write profiles into the home folder.
	for _, name := range []string{"TORRENT_STORAGE_DEFAULT_FILE_IO", "GOPPROF"} {
		if v, set := os.LookupEnv(name); set {
			fmt.Fprintf(os.Stderr, "%s=%q changes what the engine does; unset it\n", name, v)
			os.Exit(1)
		}
	}
	code := m.Run()
	ctx, cancel := context.WithTimeout(context.Background(), closeLimit)
	defer cancel()
	if err := WaitEngineStopped(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "after all tests:", err)
		code = 1
	}
	os.Exit(code)
}
