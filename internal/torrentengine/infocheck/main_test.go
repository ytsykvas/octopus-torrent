package infocheck

import (
	"fmt"
	"os"
	"testing"

	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// TestMain runs the fuzz target's engine under classic IO, like every engine test, and closes the
// shared client before the check that no engine goroutine is left.
func TestMain(m *testing.M) { os.Exit(swarm.MainClassicIO(closing{m})) }

// closing runs the tests and then closes the fuzz target's shared client.
type closing struct{ m *testing.M }

func (c closing) Run() int {
	code := c.m.Run()
	if err := closeEngine(); err != nil {
		fmt.Fprintln(os.Stderr, "close the fuzz client:", err)
		return 1
	}
	return code
}
