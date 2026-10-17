package files

import (
	"os"
	"testing"

	"octopustorrent/internal/testutil/volume"
)

func TestMain(m *testing.M) { os.Exit(volume.Main(m, true)) }
