package devtool

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"octopustorrent/internal/datadir"
)

// workspaceSetup prepares a fresh worktree: locked frontend dependencies, the frontend/dist that
// main.go embeds, and the Go module cache.
func workspaceSetup(sys System) error {
	steps := [][]string{
		npm("ci"),
		npm("run", "build"),
		{"go", "mod", "download"},
	}
	for _, step := range steps {
		if err := sys.Run(step[0], step[1:]...); err != nil {
			return fmt.Errorf("%s: %w", strings.Join(step, " "), err)
		}
	}
	return nil
}

// workspaceRun becomes `wails dev` on the runner's port, with the worktree's own data folder
// (OCTOPUS_DATA_DIR=<worktree>/build/workspace-data, replacing an inherited one): a workspace never
// opens the owner's library or another workspace's. The wails binary is resolved first and the
// process replaced, so the runner's PID is wails itself: its signals go straight to wails and no idle
// go process stays in between. (`go tool` would forward SIGINT/SIGTERM too; nothing survives SIGKILL
// either way — wails, Vite and the dev app are then left running.)
func workspaceRun(getenv func(string) string, sys System) error {
	port := getenv("OCTOPUS_PORT")
	if port == "" {
		port = getenv("CONDUCTOR_PORT")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("set OCTOPUS_PORT or CONDUCTOR_PORT to a TCP port (1-65535), got " + strconv.Quote(port))
	}
	root, err := sys.Output("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("find the repository root: %w", err)
	}
	data := datadir.Override + "=" + filepath.Join(strings.TrimSpace(root), workspaceData)
	out, err := sys.Output("go", "tool", "-n", "wails")
	if err != nil {
		return fmt.Errorf("locate wails: %w", err)
	}
	wails := strings.TrimSpace(out)
	return sys.Exec(wails, []string{wails, "dev", "-devserver", "localhost:" + port}, data)
}
