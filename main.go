// Command OctopusTorrent starts the macOS desktop application: a Wails v2
// window with the embedded React frontend bound to the Go facade.
package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"octopustorrent/internal/app"
	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
	"octopustorrent/internal/commandorder"
	"octopustorrent/internal/datadir"
	"octopustorrent/internal/library"
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
// generatingBindings and resolveDataDir come from the build-mode files
// (buildmode_*.go): the bindings generator, a development build and the
// application each get their own.
func main() {
	// The runtime's one command order: the ledger's now, and every service's that changes a torrent
	// or the settings (025, 053) gets this same value, or their steps would not wait for each other.
	order := commandorder.New()
	os.Exit(run(system{
		start:         wails.Run,
		stderr:        os.Stderr,
		sqliteVersion: library.SQLiteVersion,
		bindingsOnly:  generatingBindings,
		dataDir:       resolveDataDir,
		openLibrary:   openLibrary,
	}, operations.NewLedger(contracts.NewRuntimeID(), order)))
}

// system is what run takes from the outside world; main passes the real one, the tests a fake.
type system struct {
	// start runs the window until the application quits (wails.Run).
	start  func(*options.App) error
	stderr io.Writer
	// sqliteVersion reads the bundled SQLite's version from an in-memory database.
	sqliteVersion func(context.Context) (string, error)
	// bindingsOnly is set in the build that generates the bindings: Wails runs main to read the
	// bound methods on every wails build, generate module and wails dev.
	bindingsOnly bool
	// dataDir resolves the data folder (internal/datadir).
	dataDir func() (string, error)
	// openLibrary opens the library in the data folder.
	openLibrary func(ctx context.Context, dir string) (io.Closer, error)
}

// run starts the application of the ledger's runtime and returns the process
// exit code. In order: the bundled SQLite's version (an application whose
// SQLite does not work does not start), the data folder, the library (created,
// or migrated to the current schema), then the window; the library is closed
// after the window, the last thing before the exit. The bindings generator gets
// the window's options only: no data folder, no library. Each failure is
// reported on stderr, and nothing after it runs. Errors cross the bridge as
// AppError JSON, and quitting stops the ledger's operations.
func run(sys system, ledger *operations.Ledger) int {
	version, err := sys.sqliteVersion(context.Background())
	if err != nil {
		return failed(sys.stderr, "read the SQLite version", err)
	}
	opts := &options.App{
		Title:          "OctopusTorrent",
		Width:          1024,
		Height:         768,
		AssetServer:    &assetserver.Options{Assets: assets},
		Bind:           []any{app.New(ledger, version)},
		ErrorFormatter: app.ErrorFormatter(sys.stderr),
		OnShutdown:     func(ctx context.Context) { stopOperations(ctx, ledger, sys.stderr) },
	}
	if sys.bindingsOnly {
		return exitCode(sys.stderr, sys.start(opts))
	}
	dir, err := sys.dataDir()
	if err != nil {
		return failed(sys.stderr, "find the data folder", err)
	}
	lib, err := sys.openLibrary(context.Background(), dir)
	if err != nil {
		return failed(sys.stderr, "open the library", err)
	}
	code := exitCode(sys.stderr, sys.start(opts))
	if err := lib.Close(); err != nil {
		// ErrBusy or ErrFull: the WAL stays beside the library, and the next start uses it.
		fmt.Fprintf(sys.stderr, "OctopusTorrent: close the library: %v\n", err)
	}
	return code
}

// failed reports a step of the start that failed and returns the exit code 1.
func failed(stderr io.Writer, step string, err error) int {
	fmt.Fprintf(stderr, "OctopusTorrent: %s: %v\n", step, err)
	return 1
}

// exitCode is 0 when the window ran and quit, or 1 with its error on stderr.
func exitCode(stderr io.Writer, err error) int {
	if err != nil {
		fmt.Fprintf(stderr, "OctopusTorrent: %v\n", err)
		return 1
	}
	return 0
}

// openLibrary opens the library file of the data folder dir. On an error the closer is nil, never a
// nil *library.Library inside the interface.
func openLibrary(ctx context.Context, dir string) (io.Closer, error) {
	lib, err := library.OpenLibrary(ctx, filepath.Join(dir, datadir.LibraryFile))
	if err != nil {
		return nil, err
	}
	return lib, nil
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
