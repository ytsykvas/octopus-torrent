//go:build darwin

package platform

import (
	"os"
	"testing"

	"octopustorrent/internal/testutil/nativecov"
	"octopustorrent/internal/testutil/volume"
)

// TestMain refuses to start while a disk image of an ended run is still attached, and fails the
// package if a test leaves one behind. It flushes the native coverage profile before exiting,
// because os.Exit skips the C runtime's atexit writer (task 069).
func TestMain(m *testing.M) {
	code := volume.Main(m, true)
	nativecov.Flush() // write the clang coverage profile; os.Exit would skip the C writer (task 069)
	os.Exit(code)
}
