//go:build darwin

package platform

import (
	"os"
	"testing"

	"octopustorrent/internal/testutil/volume"
)

// TestMain refuses to start while a disk image of an ended run is still attached, and fails the
// package if a test leaves one behind.
func TestMain(m *testing.M) { os.Exit(volume.Main(m, true)) }
