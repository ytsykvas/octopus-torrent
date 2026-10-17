package filestore

import (
	"os"
	"testing"

	"octopustorrent/internal/testutil/volume"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// childEnv marks the child processes of the crash tests; they run this binary too, and only the
// parent checks the disk images.
const childEnv = "OCTOPUS_FILESTORE_CHILD"

type mainRunner struct{ m *testing.M }

func (r mainRunner) Run() int { return volume.Main(r.m, os.Getenv(childEnv) == "") }

// The engine reads its file IO from the environment before any test runs: restart under classic IO.
func TestMain(m *testing.M) { os.Exit(swarm.MainClassicIO(mainRunner{m})) }
