package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"

	"octopustorrent/internal/app"
	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
	"octopustorrent/internal/commandorder"
	"octopustorrent/internal/datadir"
	"octopustorrent/internal/library"
)

const (
	runtimeID = contracts.RuntimeID("8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e")
	opID      = contracts.OperationID("c0ffee00-1234-4abc-9def-0123456789ab")
	dataDir   = "/Users/me/Library/Application Support/dev.tsykvas.octopustorrent"
)

// fake is a system that records what run asked of it, in order.
type fake struct {
	calls  []string
	opts   *options.App
	stderr bytes.Buffer

	versionErr, dirErr, openErr, startErr, closeErr error
	bindingsOnly                                    bool
}

func (f *fake) system() system {
	return system{
		start: func(opts *options.App) error {
			f.calls, f.opts = append(f.calls, "start"), opts
			return f.startErr
		},
		stderr: &f.stderr,
		sqliteVersion: func(context.Context) (string, error) {
			f.calls = append(f.calls, "version")
			return "3.53.4", f.versionErr
		},
		bindingsOnly: f.bindingsOnly,
		dataDir: func() (string, error) {
			f.calls = append(f.calls, "data folder")
			return dataDir, f.dirErr
		},
		openLibrary: func(_ context.Context, dir string) (io.Closer, error) {
			f.calls = append(f.calls, "open "+dir)
			if f.openErr != nil {
				return nil, f.openErr
			}
			return closer{f}, nil
		},
	}
}

type closer struct{ f *fake }

func (c closer) Close() error {
	c.f.calls = append(c.f.calls, "close")
	return c.f.closeErr
}

func newLedger() *operations.Ledger { return operations.NewLedger(runtimeID, commandorder.New()) }

// startedOptions runs the application with a fake that succeeds and returns the options it started
// the window with.
func startedOptions(t *testing.T) *options.App {
	t.Helper()
	f := &fake{}
	if code := run(f.system(), newLedger()); code != 0 {
		t.Fatalf("run() = %d, want 0; stderr %q", code, f.stderr.String())
	}
	return f.opts
}

func TestRunStartsTheWindowWithTheEmbeddedFrontendAndTheFacade(t *testing.T) {
	got := startedOptions(t)
	if got.Title != "OctopusTorrent" {
		t.Fatalf("Title = %q, want OctopusTorrent", got.Title)
	}
	if _, err := fs.Stat(got.AssetServer.Assets, "frontend/dist/index.html"); err != nil {
		t.Fatalf("embedded frontend has no index.html: %v", err)
	}
	if len(got.Bind) != 1 {
		t.Fatalf("Bind = %v, want exactly the facade", got.Bind)
	}
	facade, ok := got.Bind[0].(*app.App)
	if !ok {
		t.Fatalf("Bind[0] = %T, want *app.App", got.Bind[0])
	}
	if snapshot := facade.GetLibrarySnapshot(); snapshot.RuntimeID != runtimeID {
		t.Fatalf("the facade reports runtime %s, want %s", snapshot.RuntimeID, runtimeID)
	}
	if v := facade.GetRuntimeInfo().SQLiteVersion; v != "3.53.4" {
		t.Fatalf("the facade reports SQLite %q, want the version read at start", v)
	}
}

func TestErrorsCrossTheBridgeAsAppErrorJSONWithTheCauseOnStderr(t *testing.T) {
	f := &fake{}
	run(f.system(), newLedger())
	got := f.opts.ErrorFormatter(errors.New("x"))
	if want := `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}`; got != want {
		t.Fatalf("ErrorFormatter = %v, want %s", got, want)
	}
	if want := "OctopusTorrent: unexpected error: \"x\"\n"; f.stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", f.stderr.String(), want)
	}
}

func TestRunOpensTheLibraryBeforeTheWindowAndClosesItAfter(t *testing.T) {
	f := &fake{}
	if code := run(f.system(), newLedger()); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if want := []string{"version", "data folder", "open " + dataDir, "start", "close"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls %v, want %v", f.calls, want)
	}
	if f.stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want nothing", f.stderr.String())
	}
}

func TestEveryStartupFailureExitsWithItsReasonAndRunsNothingAfter(t *testing.T) {
	broken := errors.New("broken")
	for name, c := range map[string]struct {
		fake  fake
		calls []string
		line  string
	}{
		"the SQLite version": {fake{versionErr: broken}, []string{"version"}, "OctopusTorrent: read the SQLite version: broken\n"},
		"the data folder":    {fake{dirErr: broken}, []string{"version", "data folder"}, "OctopusTorrent: find the data folder: broken\n"},
		"the library": {fake{openErr: broken}, []string{"version", "data folder", "open " + dataDir},
			"OctopusTorrent: open the library: broken\n"},
		// The window failed after the library opened: the library is still closed.
		"the window": {fake{startErr: broken}, []string{"version", "data folder", "open " + dataDir, "start", "close"},
			"OctopusTorrent: broken\n"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &c.fake
			if code := run(f.system(), newLedger()); code != 1 {
				t.Fatalf("run() = %d, want 1", code)
			}
			if !slices.Equal(f.calls, c.calls) {
				t.Fatalf("calls %v, want %v", f.calls, c.calls)
			}
			if f.stderr.String() != c.line {
				t.Fatalf("stderr = %q, want %q", f.stderr.String(), c.line)
			}
		})
	}
}

func TestALibraryThatDoesNotCloseIsReportedAndKeepsTheExitCode(t *testing.T) {
	f := &fake{closeErr: library.ErrBusy}
	if code := run(f.system(), newLedger()); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if want := "OctopusTorrent: close the library: " + library.ErrBusy.Error() + "\n"; f.stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", f.stderr.String(), want)
	}
}

func TestGeneratingBindingsTouchesNoDataFolderAndNoLibrary(t *testing.T) {
	f := &fake{bindingsOnly: true}
	if code := run(f.system(), newLedger()); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if want := []string{"version", "start"}; !slices.Equal(f.calls, want) {
		t.Fatalf("calls %v, want %v: the bindings generator runs main on every build", f.calls, want)
	}
	if len(f.opts.Bind) != 1 {
		t.Fatalf("Bind = %v, want the facade, whose methods the bindings describe", f.opts.Bind)
	}
}

func TestOpenLibraryOpensTheLibraryFileOfTheDataFolder(t *testing.T) {
	dir := t.TempDir()
	lib, err := openLibrary(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, datadir.LibraryFile)); err != nil {
		t.Fatalf("no library in the data folder: %v", err)
	}
}

func TestOpenLibraryOfAFolderThatCannotHoldOneIsAnErrorWithoutACloser(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, datadir.LibraryFile), []byte("not a database, but long enough to be read as one"), 0o600); err != nil {
		t.Fatal(err)
	}
	lib, err := openLibrary(context.Background(), dir)
	if err == nil || lib != nil {
		t.Fatalf("openLibrary = %v, %v; want an error and a nil closer (not a typed nil)", lib, err)
	}
	if !errors.Is(err, library.ErrCorrupt) {
		t.Fatalf("openLibrary = %v, want the file refused as not a database", err)
	}
}

// Quitting stops the operations: no goroutine of the ledger outlives the application.
func TestRunStopsOperationsOnShutdown(t *testing.T) {
	ledger := newLedger()
	f := &fake{}
	run(f.system(), ledger)

	f.opts.OnShutdown(context.Background())

	_, problem := ledger.Start(opID, contracts.OperationPauseTorrents, "fp", nil, func(context.Context) *contracts.AppError { return nil })
	if problem == nil || problem.Code != contracts.CodeOperationCancelled {
		t.Fatalf("Start after OnShutdown = %+v, want operation.cancelled", problem)
	}
}

func TestStopOperationsReportsMutationsThatDidNotStop(t *testing.T) {
	ledger := newLedger()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	ledger.Start(opID, contracts.OperationPauseTorrents, "fp", nil, func(context.Context) *contracts.AppError {
		close(started)
		<-release
		return nil
	})
	<-started
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer

	stopOperations(ended, ledger, &stderr)

	if !strings.HasPrefix(stderr.String(), "OctopusTorrent: operations still running at quit: ") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
