package main

import (
	"bytes"
	"errors"
	"io/fs"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"

	"octopustorrent/internal/app"
)

func TestRunStartsApplicationWithEmbeddedFrontendAndFacade(t *testing.T) {
	var got *options.App
	var stderr bytes.Buffer

	code := run(func(opts *options.App) error {
		got = opts
		return nil
	}, &stderr)

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
	if _, ok := got.Bind[0].(*app.App); !ok {
		t.Fatalf("Bind[0] = %T, want *app.App", got.Bind[0])
	}
}

func TestRunReportsStartFailureWithNonZeroCode(t *testing.T) {
	var stderr bytes.Buffer

	code := run(func(*options.App) error { return errors.New("webview unavailable") }, &stderr)

	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if want := "OctopusTorrent: webview unavailable\n"; stderr.String() != want {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}
