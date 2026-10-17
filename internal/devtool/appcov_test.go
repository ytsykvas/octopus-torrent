package devtool

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	appDir     = "/work/repo/build/coverage/go-app"
	isRunning  = `osascript -e application id "dev.tsykvas.octopustorrent" is running`
	appBuild   = "env -C . GOFLAGS=-cover -covermode=atomic -coverpkg=octopustorrent/... go tool wails build -s -trimpath"
	appOpen    = "open -n --env GOCOVERDIR=" + appDir + " /work/repo/build/bin/OctopusTorrent.app"
	appPgrep   = "pgrep -f ^/work/repo/build/bin/OctopusTorrent\\.app/Contents/MacOS/"
	appQuit    = `osascript -e tell application id "dev.tsykvas.octopustorrent" to quit`
	appAlive   = "ps -p 4242"
	cancelled  = "execution error: OctopusTorrent got an error: User canceled. (-128)"
	notAllowed = "execution error: Not authorized to send Apple events to OctopusTorrent. (-1743)"
)

var exitStatus1 = errors.New("exit status 1")

// The committed engine spike, the second row of wailsApps.
const (
	engineSpikeRunning = `osascript -e application id "dev.tsykvas.octopustorrent.enginespike" is running`
	engineSpikePgrep   = "pgrep -f ^/work/repo/cmd/engine-spike/build/bin/EngineSpike\\.app/Contents/MacOS/"
	engineSpikeQuit    = `osascript -e tell application id "dev.tsykvas.octopustorrent.enginespike" to quit`
	engineSpikeAlive   = "ps -p 6363"
)

// appRun is a fake run of the committed apps: the product starts on the second poll and quits after
// the first quit request; the engine spike starts at once and quits on request.
func appRun() *fakeSystem {
	return &fakeSystem{
		results: map[string][]fakeResult{
			isRunning:          {{out: "false\n"}},
			appPgrep:           {{err: exitStatus1}, {out: "4242\n"}},
			appQuit:            {{out: cancelled, err: exitStatus1}},
			appAlive:           {{out: "4242 OctopusTorrent\n"}, {err: exitStatus1}},
			engineSpikeRunning: {{out: "false\n"}},
			engineSpikePgrep:   {{out: "6363\n"}},
			engineSpikeQuit:    {{out: cancelled, err: exitStatus1}},
			engineSpikeAlive:   {{err: exitStatus1}},
		},
		dirs: map[string][]string{appDir: {"covcounters.abc.4242.1", "covmeta.abc", "covcounters.ghi.6363.1"}},
	}
}

func TestAppCoverageRunLaunchesAndQuitsTheInstrumentedApp(t *testing.T) {
	sys := appRun()
	got := appCoverageRun(sys, "/work/repo", appDir, "octopustorrent")
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
		"no counters": {func(s *fakeSystem) { s.dirs[appDir] = []string{"covmeta.abc"} }, statusFail, "no coverage counters"},
		// Counters of another process (an earlier app of the run) do not count for this one.
		"counters of another pid": {func(s *fakeSystem) { s.dirs[appDir] = []string{"covcounters.abc.4243.1"} }, statusFail, "no coverage counters"},
		"unreadable dir":          {func(s *fakeSystem) { s.dirErr = errors.New("permission denied") }, statusFail, "no coverage counters"},
	}
	for name, c := range cases {
		sys := appRun()
		c.change(sys)
		got := appCoverageRun(sys, "/work/repo", appDir, "octopustorrent")
		if got.status != c.status || !strings.Contains(got.detail+got.output, c.want) {
			t.Errorf("%s: appCoverageRun = %+v, want %s containing %q", name, got, c.status, c.want)
		}
		if name == "never quits" && !strings.Contains(strings.Join(sys.calls, "\n"), "capture kill 4242") {
			t.Errorf("never quits: the app was left running:\n%s", strings.Join(sys.calls, "\n"))
		}
	}
}

const (
	spikeRunning = `osascript -e application id "dev.example.spike" is running`
	spikeBuild   = "env -C cmd/spike GOFLAGS=-cover -covermode=atomic -coverpkg=octopustorrent/... go tool wails build -s -trimpath"
	spikeOpen    = "open -n --env GOCOVERDIR=" + appDir + " /work/repo/cmd/spike/build/bin/Spike.app"
	spikePgrep   = "pgrep -f ^/work/repo/cmd/spike/build/bin/Spike\\.app/Contents/MacOS/"
	spikeQuit    = `osascript -e tell application id "dev.example.spike" to quit`
	spikeAlive   = "ps -p 5151"
)

var spikeApp = wailsApp{Name: "Spike", Dir: "cmd/spike", Bundle: "build/bin/Spike.app", BundleID: "dev.example.spike",
	Main: "cmd/spike/main.go", Bindings: "frontend/spike/wailsjs/"}

// twoApps is appRun with a second Wails project in the table: it starts at once and quits on request.
func twoApps(t *testing.T) *fakeSystem {
	replace(t, &wailsApps, []wailsApp{wailsApps[0], spikeApp})
	sys := appRun()
	sys.results[spikeRunning] = []fakeResult{{out: "false\n"}}
	sys.results[spikePgrep] = []fakeResult{{out: "5151\n"}}
	sys.results[spikeQuit] = []fakeResult{{out: cancelled, err: exitStatus1}}
	sys.results[spikeAlive] = []fakeResult{{err: exitStatus1}}
	sys.dirs[appDir] = append(sys.dirs[appDir], "covcounters.def.5151.1")
	return sys
}

func TestAppCoverageRunBuildsLaunchesAndQuitsEveryWailsApp(t *testing.T) {
	sys := twoApps(t)
	got := appCoverageRun(sys, "/work/repo", appDir, "octopustorrent")
	if got.status != statusPass || !strings.Contains(got.detail, "OctopusTorrent (pid 4242)") || !strings.Contains(got.detail, "Spike (pid 5151)") {
		t.Fatalf("appCoverageRun = %+v, want pass naming both apps", got)
	}
	calls := strings.Join(sys.calls, "\n")
	last := -1
	for _, want := range []string{"capture " + appBuild, "capture " + appOpen, "capture " + appQuit, "capture " + spikeBuild, "capture " + spikeOpen, "capture " + spikeQuit} {
		i := strings.Index(calls, want)
		if i < 0 || i < last {
			t.Fatalf("call %q missing or out of order in:\n%s", want, calls)
		}
		last = i
	}
}

func TestAppCoverageRunNamesTheAppThatFailed(t *testing.T) {
	cases := map[string]struct {
		change func(*fakeSystem)
		status string
		want   string
	}{
		"spike already running": {func(s *fakeSystem) { s.results[spikeRunning] = []fakeResult{{out: "true\n"}} }, statusNotRun, "Spike is already running"},
		"spike build fails": {func(s *fakeSystem) { s.results[spikeBuild] = []fakeResult{{out: "boom", err: exitStatus1}} },
			statusFail, "Spike: instrumented build failed"},
		"spike never starts":       {func(s *fakeSystem) { s.results[spikePgrep] = []fakeResult{{err: exitStatus1}} }, statusFail, "Spike: the .app did not start"},
		"spike writes no counters": {func(s *fakeSystem) { s.dirs[appDir] = s.dirs[appDir][:2] }, statusFail, "Spike: the .app wrote no coverage counters"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			sys := twoApps(t)
			c.change(sys)
			got := appCoverageRun(sys, "/work/repo", appDir, "octopustorrent")
			if got.status != c.status || !strings.Contains(got.detail+got.output, c.want) {
				t.Errorf("appCoverageRun = %+v, want %s containing %q", got, c.status, c.want)
			}
		})
	}
}

// replace sets a package variable for the rest of the test and restores it afterwards.
func replace[T any](t *testing.T, v *T, value T) {
	saved := *v
	*v = value
	t.Cleanup(func() { *v = saved })
}

// The table must describe the committed projects: a wrong bundle ID would quit (or miss) another
// app, a wrong main file would hide that app's func main from the coverage check.
func TestWailsAppsMatchTheCommittedProjects(t *testing.T) {
	for _, a := range wailsApps {
		dir := filepath.Join("..", "..", a.Dir)
		if _, err := os.Stat(filepath.Join(dir, "wails.json")); err != nil {
			t.Errorf("%s: %v", a.Name, err)
		}
		for _, plist := range []string{"Info.plist", "Info.dev.plist"} {
			data, err := os.ReadFile(filepath.Join(dir, "build", "darwin", plist))
			if err != nil || !strings.Contains(string(data), "<key>CFBundleIdentifier</key>\n        <string>"+a.BundleID+"</string>") {
				t.Errorf("%s: %s does not name the bundle ID %s (%v)", a.Name, plist, a.BundleID, err)
			}
		}
		if data, err := os.ReadFile(filepath.Join("..", "..", a.Main)); err != nil || !strings.Contains(string(data), "\nfunc main() {") {
			t.Errorf("%s: %s has no func main (%v)", a.Name, a.Main, err)
		}
		if _, err := os.Stat(filepath.Join("..", "..", a.Bindings)); err != nil {
			t.Errorf("%s: %v", a.Name, err)
		}
	}
	if wailsApps[0].Dir != "." {
		t.Error("the product must be the first Wails app")
	}
}
