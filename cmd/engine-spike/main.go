// Command engine-spike is the temporary engine spike app of task 007 (deleted by task 034): a
// separate Wails window in which React shows a real transfer between two loopback engine clients,
// read through the spike's own DTO, binding and event. It shares the frontend project with the
// product but has its own page, bundle ID and bindings, and the product never links it.
package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"octopustorrent/internal/enginespike"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

// assets is the spike's page, built by frontend/vite.spike.config.ts. The directory is not named
// dist: wails build would copy the product's frontend/dist over it.
//
//go:embed all:assets
var assets embed.FS

// shutdownTimeout bounds how long quitting waits for the engine clients to close.
const shutdownTimeout = 5 * time.Second

// main has no branches: all logic lives in run; main itself is covered by the gate's instrumented
// run of the real EngineSpike.app.
func main() {
	os.Exit(run(wails.Run, os.Stderr, enginespike.New(runtime.EventsEmit, swarm.NewPeer)))
}

// run starts the spike window through start (wails.Run in the app) and returns the exit code. Only
// the service's API is bound; Wails calls the lifecycle hooks, which the page cannot.
func run(start func(*options.App) error, stderr io.Writer, svc *enginespike.Service) int {
	err := start(&options.App{
		Title:       "OctopusTorrent engine spike",
		Width:       720,
		Height:      480,
		AssetServer: &assetserver.Options{Assets: assets},
		Bind:        []any{svc.API()},
		OnStartup:   svc.Startup,
		OnShutdown:  func(ctx context.Context) { stop(ctx, svc.Shutdown, stderr) },
	})
	if err != nil {
		fmt.Fprintf(stderr, "EngineSpike: %v\n", err)
		return 1
	}
	return 0
}

// stop shuts the service down within shutdownTimeout and reports a failure on stderr.
func stop(ctx context.Context, shutdown func(context.Context) error, stderr io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		fmt.Fprintf(stderr, "EngineSpike: shutdown: %v\n", err)
	}
}
