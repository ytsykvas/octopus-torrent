package devtool

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var checkNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

const (
	lockfile = `{"lockfileVersion":3,"packages":{
		"":{"name":"octopustorrent-frontend","version":"0.0.1"},
		"node_modules/react":{"version":"19.3.0"},
		"node_modules/@types/react":{"version":"19.3.0","dev":true},
		"node_modules/string-width-cjs":{"name":"string-width","version":"4.2.3"},
		"node_modules/a/node_modules/react":{"version":"18.0.0"},
		"node_modules/local":{"link":true,"resolved":"../local"}}}`
	goModules = `{"Path":"octopustorrent","Main":true,"GoVersion":"1.27.2"}
{"Path":"github.com/BurntSushi/toml","Version":"v1.4.0","Indirect":true}
{"Path":"golang.org/x/sys","Version":"v0.48.0","Replace":{"Path":"golang.org/x/sys","Version":"v0.47.0"}}
{"Path":"example.com/local","Version":"v1.0.0","Replace":{"Path":"../local"}}
`
	goListModules = "go list -m -json all"
	npmOutdated   = "npm --prefix frontend outdated --json"
	goListUpdates = "go list -m -u -json all"
	npmAudit      = "npm --prefix frontend audit"
	govulncheck   = "go tool govulncheck ./..."
)

func packument(times map[string]string) string {
	var parts []string
	for v, t := range times {
		parts = append(parts, `"`+v+`":"`+t+`"`)
	}
	return `{"name":"x","time":{` + strings.Join(parts, ",") + `}}`
}

// withDependencies adds a healthy dependency state to a fake repository: every locked version is
// older than 14 days and no update has left the quarantine.
func withDependencies(sys *fakeSystem) *fakeSystem {
	sys.now = checkNow
	sys.files["frontend/package-lock.json"] = lockfile
	sys.results[goListModules] = []fakeResult{{out: goModules}}
	sys.results[npmOutdated] = []fakeResult{{out: "{}"}}
	sys.results[goListUpdates] = []fakeResult{{out: goModules}}
	old := "2026-09-01T00:00:00Z"
	sys.gets = map[string]fakeResult{
		"https://registry.npmjs.org/react":                                     {out: packument(map[string]string{"19.3.0": old, "18.0.0": old})},
		"https://registry.npmjs.org/@types%2freact":                            {out: packument(map[string]string{"19.3.0": old})},
		"https://registry.npmjs.org/string-width":                              {out: packument(map[string]string{"4.2.3": old})},
		"https://proxy.golang.org/github.com/!burnt!sushi/toml/@v/v1.4.0.info": {out: `{"Version":"v1.4.0","Time":"` + old + `"}`},
		"https://proxy.golang.org/golang.org/x/sys/@v/v0.47.0.info":            {out: `{"Version":"v0.47.0","Time":"` + old + `"}`},
	}
	return sys
}

func TestNpmLockPackagesListsEveryInstalledVersion(t *testing.T) {
	got, err := NpmLockPackages([]byte(lockfile))
	want := "@types/react@19.3.0 react@18.0.0 react@19.3.0 string-width@4.2.3"
	if err != nil || strings.Join(got, " ") != want {
		t.Errorf("NpmLockPackages = %q, %v; want %q", got, err, want)
	}
	if _, err := NpmLockPackages([]byte("{")); err == nil {
		t.Error("NpmLockPackages accepted invalid JSON")
	}
}

func TestGoModuleVersionsUsesTheSelectedVersions(t *testing.T) {
	got, err := GoModuleVersions(goModules)
	want := "github.com/BurntSushi/toml@v1.4.0 golang.org/x/sys@v0.47.0"
	if err != nil || strings.Join(got, " ") != want {
		t.Errorf("GoModuleVersions = %q, %v; want %q", got, err, want)
	}
	if _, err := GoModuleVersions("{"); err == nil {
		t.Error("GoModuleVersions accepted invalid JSON")
	}
}

func TestEscapeModulePathFollowsTheProxyProtocol(t *testing.T) {
	if got := EscapeModulePath("github.com/BurntSushi/toml"); got != "github.com/!burnt!sushi/toml" {
		t.Errorf("EscapeModulePath = %q", got)
	}
}

func TestReleaseAgeProblemNeedsFourteenFullDays(t *testing.T) {
	if p := ReleaseAgeProblem("vite@8.3.4", checkNow.Add(-14*24*time.Hour), checkNow); p != "" {
		t.Errorf("14 days old: %q", p)
	}
	p := ReleaseAgeProblem("vite@8.3.4", checkNow.Add(-13*24*time.Hour), checkNow)
	if !strings.Contains(p, "vite@8.3.4 was published 2026-10-02, 13 days ago") {
		t.Errorf("13 days old: %q", p)
	}
}

func TestReleaseAgeStepChecksEveryLockedVersion(t *testing.T) {
	c := &checker{sys: healthyRepo(), mode: checkModes["check"]}
	if got := c.releaseAges(); got.status != statusPass || !strings.Contains(got.detail, "4 npm package versions and 2 Go modules") {
		t.Errorf("releaseAges = %+v", got)
	}
	cases := map[string]struct {
		change func(*fakeSystem)
		status string
		want   string
	}{
		"young npm package": {func(s *fakeSystem) {
			s.gets["https://registry.npmjs.org/react"] = fakeResult{out: packument(map[string]string{"19.3.0": "2026-10-10T00:00:00Z", "18.0.0": "2026-01-01T00:00:00Z"})}
		}, statusFail, "react@19.3.0 was published 2026-10-10, 5 days ago"},
		"young Go module": {func(s *fakeSystem) {
			s.gets["https://proxy.golang.org/golang.org/x/sys/@v/v0.47.0.info"] = fakeResult{out: `{"Time":"2026-10-14T00:00:00Z"}`}
		}, statusFail, "golang.org/x/sys@v0.47.0 was published 2026-10-14, 1 days ago"},
		"unknown version": {func(s *fakeSystem) {
			s.gets["https://registry.npmjs.org/string-width"] = fakeResult{out: packument(nil)}
		}, statusFail, "string-width@4.2.3: no publication time"},
		"bad answer": {func(s *fakeSystem) { s.gets["https://registry.npmjs.org/react"] = fakeResult{out: "<html>"} }, statusFail, "react@19.3.0: invalid character"},
		"bad Go answer": {func(s *fakeSystem) {
			s.gets["https://proxy.golang.org/golang.org/x/sys/@v/v0.47.0.info"] = fakeResult{out: "{}"}
		}, statusFail, "golang.org/x/sys@v0.47.0: no publication time"},
		"not in the registry": {func(s *fakeSystem) { delete(s.gets, "https://registry.npmjs.org/string-width") }, statusFail, "string-width@4.2.3: 404 Not Found"},
		"offline": {func(s *fakeSystem) {
			s.gets["https://registry.npmjs.org/react"] = fakeResult{err: errors.New("dial tcp: no such host")}
		}, statusNotRun, "network"},
		"no lockfile":     {func(s *fakeSystem) { delete(s.files, "frontend/package-lock.json") }, statusFail, "package-lock.json"},
		"bad lockfile":    {func(s *fakeSystem) { s.files["frontend/package-lock.json"] = "{" }, statusFail, "package-lock.json"},
		"go list fails":   {func(s *fakeSystem) { s.results[goListModules] = []fakeResult{{err: exitStatus1}} }, statusFail, "go list -m -json all"},
		"go list garbage": {func(s *fakeSystem) { s.results[goListModules] = []fakeResult{{out: "{"}} }, statusFail, "go list -m -json all"},
	}
	for name, tc := range cases {
		sys := healthyRepo()
		tc.change(sys)
		c := &checker{sys: sys, mode: checkModes["check"]}
		if got := c.releaseAges(); got.status != tc.status || !strings.Contains(got.detail+"\n"+got.output, tc.want) {
			t.Errorf("%s: releaseAges = %+v, want %s with %q", name, got, tc.status, tc.want)
		}
	}
}

// Transitive Go modules follow MVS (toolchain-versions ADR), so only direct requirements and tools
// are reported.
func TestUpdatesReportsOnlyVersionsOutOfQuarantine(t *testing.T) {
	sys := healthyRepo()
	sys.gets["https://registry.npmjs.org/react"] = fakeResult{out: packument(map[string]string{
		"19.3.0": "2026-09-01T00:00:00Z", "18.0.0": "2026-01-01T00:00:00Z", "19.4.0": "2026-09-20T00:00:00Z", "19.5.0": "2026-10-12T00:00:00Z"})}
	sys.gets["https://registry.npmjs.org/@types%2freact"] = fakeResult{out: packument(map[string]string{"19.3.0": "2026-09-01T00:00:00Z", "19.3.1": "2026-10-12T00:00:00Z"})}
	sys.results[npmOutdated] = []fakeResult{{out: `{"react":{"current":"19.3.0","latest":"19.4.0"},"@types/react":{"current":"19.3.0","latest":"19.3.1"}}`, err: exitStatus1}}
	sys.results[goListUpdates] = []fakeResult{{out: `{"Path":"github.com/BurntSushi/toml","Version":"v1.4.0","Update":{"Version":"v1.5.0","Time":"2026-09-01T00:00:00Z"}}
{"Path":"golang.org/x/sys","Version":"v0.47.0","Update":{"Version":"v0.49.0","Time":"2026-10-13T00:00:00Z"}}
{"Path":"golang.org/x/vuln","Version":"v1.8.0","Indirect":true,"Update":{"Version":"v1.9.0","Time":"2026-09-20T00:00:00Z"}}
{"Path":"golang.org/x/text","Version":"v0.42.0","Indirect":true,"Update":{"Version":"v0.43.0","Time":"2026-09-20T00:00:00Z"}}
`}}
	sys.files["go.mod"] = "module octopustorrent\n\ntool (\n\tgolang.org/x/vuln/cmd/govulncheck\n\toctopustorrent/cmd/devtool\n)\n"
	c := &checker{sys: sys, mode: checkModes["check"]}
	c.releaseAges()
	got := c.updates()
	want := "3 updates out of quarantine: react 19.3.0 → 19.4.0, github.com/BurntSushi/toml v1.4.0 → v1.5.0, golang.org/x/vuln v1.8.0 → v1.9.0"
	if got.status != statusPass || got.detail != want {
		t.Errorf("updates = %+v", got)
	}
	c = &checker{sys: healthyRepo(), mode: checkModes["check"]}
	if got := c.updates(); got.status != statusPass || got.detail != "no update has been out of quarantine for 14 days" {
		t.Errorf("no updates: %+v", got)
	}
	for name, change := range map[string]func(*fakeSystem){
		"npm outdated garbage": func(s *fakeSystem) { s.results[npmOutdated] = []fakeResult{{out: "npm ERR!", err: exitStatus1}} },
		"go list garbage":      func(s *fakeSystem) { s.results[goListUpdates] = []fakeResult{{out: "{", err: exitStatus1}} },
	} {
		sys := healthyRepo()
		change(sys)
		if got := (&checker{sys: sys, mode: checkModes["check"]}).updates(); got.status != statusFail {
			t.Errorf("%s: updates = %+v, want FAIL", name, got)
		}
	}
}

func TestAuditsFailOnFindingsAndDoNotRunOffline(t *testing.T) {
	for _, command := range []string{npmAudit, govulncheck} {
		for name, tc := range map[string]struct {
			answer fakeResult
			status string
		}{
			"clean":         {fakeResult{out: "found 0 vulnerabilities"}, statusPass},
			"vulnerable":    {fakeResult{out: "1 high severity vulnerability", err: exitStatus1}, statusFail},
			"network error": {fakeResult{out: "request to https://registry.npmjs.org failed, reason: getaddrinfo ENOTFOUND", err: exitStatus1}, statusNotRun},
		} {
			sys := healthyRepo()
			sys.results[command] = []fakeResult{tc.answer}
			c := &checker{sys: sys, mode: checkModes["check"]}
			step := c.audit(strings.Fields(command)...)
			if got := step(); got.status != tc.status {
				t.Errorf("%s %s: %+v, want %s", command, name, got, tc.status)
			}
		}
	}
}

func TestNetworkStepsAreSkippedOfflineAndAloneInCheckDeps(t *testing.T) {
	sys := healthyRepo()
	code, report := runCheckOn(sys, "check", "offline")
	if code != 0 || !strings.HasPrefix(line(report, "release age"), statusNotRun) || !strings.Contains(report, "GATE INCOMPLETE: not run: release age, npm audit, govulncheck, updates") {
		t.Errorf("check offline: exit %d report:\n%s", code, report)
	}
	sys = healthyRepo()
	code, report = runCheckOn(sys, "check", "deps")
	if code != 0 || line(report, "typecheck") != "" || !strings.HasPrefix(line(report, "release age"), statusPass) || !strings.HasSuffix(report, "DEPENDENCIES PASSED\n") {
		t.Errorf("check deps: exit %d report:\n%s", code, report)
	}
	sys = healthyRepo()
	sys.results[npmAudit] = []fakeResult{{out: "getaddrinfo ENOTFOUND registry.npmjs.org", err: exitStatus1}}
	if code, report = runCheckOn(sys, "check", "deps"); code != 1 || !strings.Contains(report, "DEPENDENCIES FAILED: not run: npm audit") {
		t.Errorf("check deps offline: exit %d report:\n%s", code, report)
	}
}
