package main

import (
	"bytes"
	"context"
	"errors"
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

func TestRunStartsApplicationWithEmbeddedFrontendAndFacade(t *testing.T) {
	var got *options.App
	var stderr bytes.Buffer
	ledger := operations.NewLedger(runtimeID)

	code := run(func(opts *options.App) error {
		got = opts
		return nil
	}, &stderr, runtimeID, ledger)

	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
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
	failure := errors.New("x")
	if got.ErrorFormatter == nil || got.ErrorFormatter(failure) != app.FormatError(failure) {
		t.Fatal("errors do not cross the bridge as AppError JSON")
	}
}

// Quitting stops the operations: no goroutine of the ledger outlives the application.
func TestRunStopsOperationsOnShutdown(t *testing.T) {
	var got *options.App
	ledger := operations.NewLedger(runtimeID)
	run(func(opts *options.App) error {
		got = opts
		return nil
	}, &bytes.Buffer{}, runtimeID, ledger)

	got.OnShutdown(context.Background())

	_, problem := ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError { return nil })
	if problem == nil || problem.Code != contracts.CodeOperationCancelled {
		t.Fatalf("Start after OnShutdown = %+v, want operation.cancelled", problem)
	}
}

func TestStopOperationsReportsMutationsThatDidNotStop(t *testing.T) {
	ledger := operations.NewLedger(runtimeID)
	release := make(chan struct{})
	defer close(release)
	ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError {
		<-release
		return nil
	})
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

	code := run(func(*options.App) error { return errors.New("webview unavailable") }, &stderr, runtimeID, operations.NewLedger(runtimeID))

	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if want := "OctopusTorrent: webview unavailable\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}
