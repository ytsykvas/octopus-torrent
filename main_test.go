package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"

	"octopustorrent/internal/app"
	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
)

const (
	runtimeID = contracts.RuntimeID("8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e")
	opID      = contracts.OperationID("c0ffee00-1234-4abc-9def-0123456789ab")
)

// sqliteVersion stands in for library.SQLiteVersion.
func sqliteVersion(context.Context) (string, error) { return "3.53.4", nil }

func TestRunStartsApplicationWithEmbeddedFrontendAndFacade(t *testing.T) {
	var got *options.App
	var stderr bytes.Buffer
	ledger := operations.NewLedger(runtimeID)

	code := run(func(opts *options.App) error {
		got = opts
		return nil
	}, &stderr, ledger, sqliteVersion)

	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
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
	if got.ErrorFormatter == nil || got.ErrorFormatter(errors.New("x")) != app.ErrorFormatter(io.Discard)(errors.New("x")) {
		t.Fatal("errors do not cross the bridge as AppError JSON")
	}
	if !strings.Contains(stderr.String(), `unexpected error: "x"`) {
		t.Fatalf("the cause of an unexpected error is not in stderr: %q", stderr.String())
	}
}

// Quitting stops the operations: no goroutine of the ledger outlives the application.
func TestRunStopsOperationsOnShutdown(t *testing.T) {
	var got *options.App
	ledger := operations.NewLedger(runtimeID)
	run(func(opts *options.App) error {
		got = opts
		return nil
	}, &bytes.Buffer{}, ledger, sqliteVersion)

	got.OnShutdown(context.Background())

	_, problem := ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError { return nil })
	if problem == nil || problem.Code != contracts.CodeOperationCancelled {
		t.Fatalf("Start after OnShutdown = %+v, want operation.cancelled", problem)
	}
}

func TestStopOperationsReportsMutationsThatDidNotStop(t *testing.T) {
	ledger := operations.NewLedger(runtimeID)
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError {
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

func TestRunReportsStartFailureWithNonZeroCode(t *testing.T) {
	var stderr bytes.Buffer

	code := run(func(*options.App) error { return errors.New("webview unavailable") }, &stderr, operations.NewLedger(runtimeID), sqliteVersion)

	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if want := "OctopusTorrent: webview unavailable\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunRefusesToStartWithoutAWorkingSQLite(t *testing.T) {
	var stderr bytes.Buffer
	started := false
	broken := func(context.Context) (string, error) { return "", errors.New("no driver") }

	code := run(func(*options.App) error { started = true; return nil }, &stderr, operations.NewLedger(runtimeID), broken)

	if code != 1 || started {
		t.Fatalf("run() = %d, started %v; want 1 without starting", code, started)
	}
	if want := "OctopusTorrent: read the SQLite version: no driver\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}
