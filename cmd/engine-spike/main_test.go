package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"

	"octopustorrent/internal/enginespike"
	"octopustorrent/internal/torrentengine/testutil/swarm"
)

func TestMain(m *testing.M) { os.Exit(swarm.Main(m)) }

func newService() *enginespike.Service {
	return enginespike.New(func(context.Context, string, ...any) {}, swarm.NewPeer)
}

func TestRunBindsOnlyTheSpikeAPIAndWiresItsLifecycle(t *testing.T) {
	var app *options.App
	var stderr bytes.Buffer
	svc := newService()
	code := run(func(a *options.App) error { app = a; return nil }, &stderr, svc)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("run = %d, stderr %q", code, stderr.String())
	}
	if app.Title != "OctopusTorrent engine spike" {
		t.Errorf("Title = %q", app.Title)
	}
	if _, err := fs.Stat(app.AssetServer.Assets, "assets/index.html"); err != nil {
		t.Fatalf("the embedded spike page has no index.html: %v", err)
	}
	if len(app.Bind) != 1 {
		t.Fatalf("Bind = %v, want only the spike API", app.Bind)
	}
	if _, ok := app.Bind[0].(*enginespike.API); !ok {
		t.Errorf("Bind[0] = %T, want *enginespike.API", app.Bind[0])
	}
	if _, err := app.Bind[0].(*enginespike.API).Start(); err == nil || !strings.Contains(err.Error(), "has not started") {
		t.Errorf("Start before OnStartup = %v", err)
	}
	app.OnStartup(context.Background())
	if got := app.Bind[0].(*enginespike.API).Snapshot(); got.State != enginespike.Idle {
		t.Errorf("after OnStartup the spike is %q, want idle", got.State)
	}
	app.OnShutdown(context.Background())
	if _, err := app.Bind[0].(*enginespike.API).Start(); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Errorf("Start after OnShutdown = %v, want the stopped error", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("a clean shutdown wrote %q", stderr.String())
	}
}

func TestRunReportsAStartFailure(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(func(*options.App) error { return errors.New("no window") }, &stderr, newService()); code != 1 {
		t.Errorf("run = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "EngineSpike: no window") {
		t.Errorf("stderr %q", stderr.String())
	}
}

func TestStopReportsAShutdownFailureWithinItsLimit(t *testing.T) {
	var stderr bytes.Buffer
	var limit time.Duration
	stop(context.Background(), func(ctx context.Context) error {
		deadline, _ := ctx.Deadline()
		limit = time.Until(deadline)
		return errors.New("a client did not close")
	}, &stderr)
	if !strings.Contains(stderr.String(), "EngineSpike: shutdown: a client did not close") || limit <= 0 || limit > shutdownTimeout {
		t.Errorf("stderr %q, limit %v", stderr.String(), limit)
	}
}
