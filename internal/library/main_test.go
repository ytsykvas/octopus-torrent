package library

import (
	"os"
	"testing"

	"octopustorrent/internal/testutil/volume"
)

func TestMain(m *testing.M) {
	// The child processes of process_test.go run this binary too; only the parent checks the images.
	parent := os.Getenv(lockChildEnv) == "" && os.Getenv(commitChildEnv) == ""
	os.Exit(volume.Main(m, parent))
}
