package devtool

import (
	"slices"
	"strings"
	"time"
)

const (
	bundleID  = "dev.tsykvas.octopustorrent"
	appBundle = "build/bin/OctopusTorrent.app"
	appPoll   = 250 * time.Millisecond
	// appPolls bounds every wait for the .app (start, quit) to 20 s; a quit request is repeated
	// every appQuitEvery polls in case the first one arrived before the window was ready.
	appPolls     = 80
	appQuitEvery = 8
)

// appCoverageRun covers what only the real application executes (func main): it builds the .app
// with coverage instrumentation, launches it with GOCOVERDIR=dir, quits it with an Apple Event —
// a normal exit, so Go writes the counters — and checks that counters were written. SIGTERM is not
// used to quit: it would end the process without writing them.
func appCoverageRun(sys System, root, dir string) stepResult {
	if running, _ := sys.Output("osascript", "-e", `application id "`+bundleID+`" is running`); strings.TrimSpace(running) == "true" {
		return notRun("OctopusTorrent is already running (another workspace?); quit it and run the check again")
	}
	if out, err := sys.Capture("env", "GOFLAGS=-cover -covermode=atomic -coverpkg=./...", "go", "tool", "wails", "build", "-s", "-trimpath"); err != nil {
		return fail("instrumented build failed: "+err.Error(), out)
	}
	if err := sys.MkdirAll(dir); err != nil {
		return fail("create "+dir+": "+err.Error(), "")
	}
	app := root + "/" + appBundle
	if out, err := sys.Capture("open", "-n", "--env", "GOCOVERDIR="+dir, app); err != nil {
		return fail("cannot open "+app+": "+err.Error(), out)
	}
	pid := ""
	for i := 0; i < appPolls && pid == ""; i++ {
		out, _ := sys.Output("pgrep", "-f", app+"/Contents/MacOS/")
		if pid, _, _ = strings.Cut(strings.TrimSpace(out), "\n"); pid == "" {
			sys.Sleep(appPoll)
		}
	}
	if pid == "" {
		return fail("the .app did not start within 20 s", "")
	}
	for i := 0; i < appPolls; i++ {
		if i%appQuitEvery == 0 {
			// osascript reports -128 when the app quits; only a refused permission matters.
			if out, err := sys.Capture("osascript", "-e", `tell application id "`+bundleID+`" to quit`); err != nil && strings.Contains(out, "-1743") {
				sys.Capture("kill", pid)
				return notRun("macOS does not let this terminal control OctopusTorrent: allow it in System Settings → Privacy & Security → Automation")
			}
		}
		if _, err := sys.Output("ps", "-p", pid); err != nil {
			names, _ := sys.ReadDir(dir)
			if !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, "covcounters.") }) {
				return fail("the .app wrote no coverage counters to "+dir, "")
			}
			return pass("instrumented .app launched and quit (pid " + pid + ")")
		}
		sys.Sleep(appPoll)
	}
	sys.Capture("kill", pid)
	return fail("the .app did not quit within 20 s; pid "+pid+" was killed", "")
}
