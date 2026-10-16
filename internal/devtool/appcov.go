package devtool

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	appPoll = 250 * time.Millisecond
	// appPolls bounds every wait for an .app (start, quit) to 20 s; a quit request is repeated
	// every appQuitEvery polls in case the first one arrived before the window was ready.
	appPolls     = 80
	appQuitEvery = 8
)

// wailsApp is one Wails project of the repository. The gate builds, launches and quits each one
// with coverage instrumentation (only the real app executes its func main) and regenerates its
// bindings.
type wailsApp struct {
	Name     string // for messages
	Dir      string // the project directory (its wails.json), relative to the repository root
	Bundle   string // the built .app, relative to Dir
	BundleID string // CFBundleIdentifier in Dir/build/darwin/Info.plist
	Main     string // the file with func main, as the coverage profile names it
	Bindings string // the generated bindings directory, relative to the repository root
}

// wailsApps lists every Wails project; the product comes first.
var wailsApps = []wailsApp{
	{Name: "OctopusTorrent", Dir: ".", Bundle: "build/bin/OctopusTorrent.app", BundleID: "dev.tsykvas.octopustorrent",
		Main: "main.go", Bindings: "frontend/wailsjs/"},
}

func appMains() []string {
	var mains []string
	for _, a := range wailsApps {
		mains = append(mains, a.Main)
	}
	return mains
}

func (a wailsApp) running(sys System) bool {
	running, _ := sys.Output("osascript", "-e", `application id "`+a.BundleID+`" is running`)
	return strings.TrimSpace(running) == "true"
}

func (a wailsApp) alreadyRunning() stepResult {
	return notRun(a.Name + " is already running (another workspace?); quit it and run the check again")
}

// appCoverageRun covers what only the real applications execute (func main): for every Wails app
// in turn it builds the .app with coverage instrumentation of the whole module, launches it with
// GOCOVERDIR=dir, quits it with an Apple Event — a normal exit, so Go writes the counters — and
// checks that this process wrote counters. SIGTERM is not used to quit: it would end the process
// without writing them.
func appCoverageRun(sys System, root, dir, module string) stepResult {
	if err := sys.MkdirAll(dir); err != nil {
		return fail("create "+dir+": "+err.Error(), "")
	}
	var ran []string
	for _, a := range wailsApps {
		r := a.coverageRun(sys, root, dir, module)
		if r.status != statusPass {
			return r
		}
		ran = append(ran, r.detail)
	}
	return pass("instrumented .app launched and quit: " + strings.Join(ran, ", "))
}

// coverageRun builds, launches and quits one app; a passing result's detail is "Name (pid N)".
func (a wailsApp) coverageRun(sys System, root, dir, module string) stepResult {
	if a.running(sys) {
		return a.alreadyRunning()
	}
	coverFlags := "GOFLAGS=-cover -covermode=atomic -coverpkg=" + module + "/..."
	if out, err := sys.Capture("env", "-C", a.Dir, coverFlags, "go", "tool", "wails", "build", "-s", "-trimpath"); err != nil {
		return fail(a.Name+": instrumented build failed: "+err.Error(), out)
	}
	app := filepath.Join(root, a.Dir, a.Bundle)
	// Checked again after the build: the instance found below must be the one this run opened.
	if a.running(sys) {
		return a.alreadyRunning()
	}
	if out, err := sys.Capture("open", "-n", "--env", "GOCOVERDIR="+dir, app); err != nil {
		return fail(a.Name+": cannot open "+app+": "+err.Error(), out)
	}
	pid := ""
	for i := 0; i < appPolls && pid == ""; i++ {
		out, _ := sys.Output("pgrep", "-f", app+"/Contents/MacOS/")
		if pid, _, _ = strings.Cut(strings.TrimSpace(out), "\n"); pid == "" {
			sys.Sleep(appPoll)
		}
	}
	if pid == "" {
		return fail(a.Name+": the .app did not start within 20 s", "")
	}
	for i := 0; i < appPolls; i++ {
		if i%appQuitEvery == 0 {
			// osascript reports -128 when the app quits; only a refused permission matters.
			if out, err := sys.Capture("osascript", "-e", `tell application id "`+a.BundleID+`" to quit`); err != nil && strings.Contains(out, "-1743") {
				return notRun("macOS does not let this terminal control " + a.Name + ": allow it in System Settings → Privacy & Security → Automation; " + kill(sys, pid))
			}
		}
		if _, err := sys.Output("ps", "-p", pid); err != nil {
			// Counter files are named covcounters.<hash>.<pid>.<time>: only this process's count.
			names, _ := sys.ReadDir(dir)
			if !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "covcounters.") && strings.Contains(n, "."+pid+".") }) {
				return fail(a.Name+": the .app wrote no coverage counters to "+dir, "")
			}
			return pass(fmt.Sprintf("%s (pid %s)", a.Name, pid))
		}
		sys.Sleep(appPoll)
	}
	return fail(a.Name+": the .app did not quit within 20 s; "+kill(sys, pid), "")
}

// kill ends the .app that could not be quit and says whether it is gone.
func kill(sys System, pid string) string {
	if out, err := sys.Capture("kill", pid); err != nil {
		return "pid " + pid + " could not be killed: " + err.Error() + ": " + strings.TrimSpace(out)
	}
	return "pid " + pid + " was killed"
}
