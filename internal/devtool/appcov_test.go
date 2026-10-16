package devtool

import (
	"errors"
	"strings"
	"testing"
)

const (
	appDir     = "/work/repo/build/coverage/go-app"
	isRunning  = `osascript -e application id "dev.tsykvas.octopustorrent" is running`
	appBuild   = "env GOFLAGS=-cover -covermode=atomic -coverpkg=./... go tool wails build -s -trimpath"
	appOpen    = "open -n --env GOCOVERDIR=" + appDir + " /work/repo/build/bin/OctopusTorrent.app"
	appPgrep   = "pgrep -f /work/repo/build/bin/OctopusTorrent.app/Contents/MacOS/"
	appQuit    = `osascript -e tell application id "dev.tsykvas.octopustorrent" to quit`
	appAlive   = "ps -p 4242"
	cancelled  = "execution error: OctopusTorrent got an error: User canceled. (-128)"
	notAllowed = "execution error: Not authorized to send Apple events to OctopusTorrent. (-1743)"
)

var exitStatus1 = errors.New("exit status 1")

// appRun is a fake .app that starts on the second poll and quits after the first quit request.
func appRun() *fakeSystem {
	return &fakeSystem{
		results: map[string][]fakeResult{
			isRunning: {{out: "false\n"}},
			appPgrep:  {{err: exitStatus1}, {out: "4242\n"}},
			appQuit:   {{out: cancelled, err: exitStatus1}},
			appAlive:  {{out: "4242 OctopusTorrent\n"}, {err: exitStatus1}},
		},
		dirs: map[string][]string{appDir: {"covcounters.abc.4242.1", "covmeta.abc"}},
	}
}

func TestAppCoverageRunLaunchesAndQuitsTheInstrumentedApp(t *testing.T) {
	sys := appRun()
	got := appCoverageRun(sys, "/work/repo", appDir)
	if got.status != statusPass || !strings.Contains(got.detail, "4242") {
		t.Fatalf("appCoverageRun = %+v, want pass naming the pid", got)
	}
	calls := strings.Join(sys.calls, "\n")
	for _, want := range []string{"capture " + appBuild, "mkdir " + appDir, "capture " + appOpen, "capture " + appQuit, "readdir " + appDir} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls missing %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "kill") {
		t.Errorf("a clean quit must not kill the app:\n%s", calls)
	}
}

func TestAppCoverageRunReportsEveryFailure(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeSystem)
		status string
		want   string
	}{
		"already running": {func(s *fakeSystem) { s.results[isRunning] = []fakeResult{{out: "true\n"}} }, statusNotRun, "already running"},
		// Another OctopusTorrent started during the instrumented build: it must not be the one quit.
		"started during the build": {func(s *fakeSystem) { s.results[isRunning] = []fakeResult{{out: "false\n"}, {out: "true\n"}} },
			statusNotRun, "already running"},
		"build fails": {func(s *fakeSystem) { s.results[appBuild] = []fakeResult{{out: "compile error", err: exitStatus1}} },
			statusFail, "instrumented build failed"},
		"no coverage dir": {func(s *fakeSystem) { s.mkdirErr = errors.New("read-only") }, statusFail, "read-only"},
		"open fails": {func(s *fakeSystem) { s.results[appOpen] = []fakeResult{{out: "LSOpenURLs failed", err: exitStatus1}} },
			statusFail, "cannot open"},
		"never starts":  {func(s *fakeSystem) { s.results[appPgrep] = []fakeResult{{err: exitStatus1}} }, statusFail, "did not start"},
		"no automation": {func(s *fakeSystem) { s.results[appQuit] = []fakeResult{{out: notAllowed, err: exitStatus1}} }, statusNotRun, "Automation"},
		"never quits":   {func(s *fakeSystem) { s.results[appAlive] = []fakeResult{{out: "4242\n"}} }, statusFail, "did not quit within 20 s; pid 4242 was killed"},
		"cannot be killed": {func(s *fakeSystem) {
			s.results[appAlive] = []fakeResult{{out: "4242\n"}}
			s.results["kill 4242"] = []fakeResult{{out: "kill: 4242: Operation not permitted", err: exitStatus1}}
		}, statusFail, "pid 4242 could not be killed: exit status 1"},
		"no automation, cannot be killed": {func(s *fakeSystem) {
			s.results[appQuit] = []fakeResult{{out: notAllowed, err: exitStatus1}}
			s.results["kill 4242"] = []fakeResult{{out: "kill: 4242: Operation not permitted", err: exitStatus1}}
		}, statusNotRun, "pid 4242 could not be killed"},
		"no counters":    {func(s *fakeSystem) { s.dirs[appDir] = []string{"covmeta.abc"} }, statusFail, "no coverage counters"},
		"unreadable dir": {func(s *fakeSystem) { s.dirErr = errors.New("permission denied") }, statusFail, "no coverage counters"},
	}
	for name, c := range cases {
		sys := appRun()
		c.change(sys)
		got := appCoverageRun(sys, "/work/repo", appDir)
		if got.status != c.status || !strings.Contains(got.detail+got.output, c.want) {
			t.Errorf("%s: appCoverageRun = %+v, want %s containing %q", name, got, c.status, c.want)
		}
		if name == "never quits" && !strings.Contains(strings.Join(sys.calls, "\n"), "capture kill 4242") {
			t.Errorf("never quits: the app was left running:\n%s", strings.Join(sys.calls, "\n"))
		}
	}
}
