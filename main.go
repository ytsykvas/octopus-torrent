// Command OctopusTorrent starts the macOS desktop application: a Wails v2
// window with the embedded React frontend bound to the Go facade.
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

	"octopustorrent/internal/app"
	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
)

// assets is the built frontend (frontend/dist) compiled into the binary, so the
// packaged .app never loads UI from the network or the developer's disk.
//
//go:embed all:frontend/dist
var assets embed.FS

// shutdownTimeout bounds how long quitting waits for running operations.
const shutdownTimeout = 5 * time.Second

// main has no branches: all logic lives in run, which is unit-tested; main
// itself is covered by the instrumented run of the real .app (see README).
func main() {
	os.Exit(run(wails.Run, os.Stderr, operations.NewLedger(contracts.NewRuntimeID())))
}

// run starts the application of the ledger's runtime through start
// (wails.Run in production) and returns the process exit code. A start
// failure is reported on stderr. Errors cross the bridge as AppError JSON,
// and quitting stops the ledger's operations.
func run(start func(*options.App) error, stderr io.Writer, ledger *operations.Ledger) int {
	err := start(&options.App{
		Title:          "OctopusTorrent",
		Width:          1024,
		Height:         768,
		AssetServer:    &assetserver.Options{Assets: assets},
		Bind:           []any{app.New(ledger)},
		ErrorFormatter: app.ErrorFormatter(stderr),
		OnShutdown:     func(ctx context.Context) { stopOperations(ctx, ledger, stderr) },
	})
	if err != nil {
		fmt.Fprintf(stderr, "OctopusTorrent: %v\n", err)
		return 1
	}
	return 0
}

// stopOperations cancels the running operations and waits for them, at most
// shutdownTimeout; a mutation that did not stop is reported on stderr.
func stopOperations(ctx context.Context, ledger *operations.Ledger, stderr io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	if err := ledger.Shutdown(ctx); err != nil {
		fmt.Fprintf(stderr, "OctopusTorrent: operations still running at quit: %v\n", err)
	}
}
