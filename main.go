// Command OctopusTorrent starts the macOS desktop application: a Wails v2
// window with the embedded React frontend bound to the Go facade.
package main

import (
	"embed"
	"fmt"
	"io"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"octopustorrent/internal/app"
)

// assets is the built frontend (frontend/dist) compiled into the binary, so the
// packaged .app never loads UI from the network or the developer's disk.
//
//go:embed all:frontend/dist
var assets embed.FS

// main has no branches: all logic lives in run, which is unit-tested; main
// itself is covered by the instrumented run of the real .app (see README).
func main() { os.Exit(run(wails.Run, os.Stderr)) }

// run starts the application through start (wails.Run in production) and
// returns the process exit code. A start failure is reported on stderr.
func run(start func(*options.App) error, stderr io.Writer) int {
	err := start(&options.App{
		Title:       "OctopusTorrent",
		Width:       1024,
		Height:      768,
		AssetServer: &assetserver.Options{Assets: assets},
		Bind:        []any{app.New()},
	})
	if err != nil {
		fmt.Fprintf(stderr, "OctopusTorrent: %v\n", err)
		return 1
	}
	return 0
}
