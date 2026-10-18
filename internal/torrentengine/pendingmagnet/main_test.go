package pendingmagnet

import (
	"os"
	"testing"

	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// The confirmed torrents use the engine's file storage, which reads its file IO from the environment
// before any test runs: restart under classic IO.
func TestMain(m *testing.M) { os.Exit(swarm.MainClassicIO(m)) }
