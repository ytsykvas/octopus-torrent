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
		"node_modules/react":{"version":"19.3.0","resolved":"https://registry.npmjs.org/react/-/react-19.3.0.tgz","integrity":"sha512-19.3.0"},
		"node_modules/@types/react":{"version":"19.3.0","dev":true,"resolved":"https://registry.npmjs.org/@types/react/-/react-19.3.0.tgz","integrity":"sha512-19.3.0"},
		"node_modules/string-width-cjs":{"name":"string-width","version":"4.2.3","resolved":"https://registry.npmjs.org/string-width/-/string-width-4.2.3.tgz","integrity":"sha512-4.2.3"},
		"node_modules/a/node_modules/react":{"version":"18.0.0","resolved":"https://registry.npmjs.org/react/-/react-18.0.0.tgz","integrity":"sha512-18.0.0"},
		"node_modules/local":{"link":true,"resolved":"../local"}}}`
	goModules = `{"Path":"octopustorrent","Main":true,"GoVersion":"1.27.2"}
{"Path":"github.com/BurntSushi/toml","Version":"v1.4.0","Indirect":true}
{"Path":"golang.org/x/sys","Version":"v0.48.0","Replace":{"Path":"golang.org/x/sys","Version":"v0.47.0"}}
`
	goListModules = "go list -m -json all"
	npmOutdated   = "npm --prefix frontend outdated --json"
	goListUpdates = "go list -m -u -json all"
	npmAudit      = "npm --prefix frontend audit"
	govulncheck   = "go tool govulncheck ./..."
)

// packument is a registry document whose versions were published at the given times; each version's
// integrity is "sha512-<version>", as in the lockfile fixture.
func packument(times map[string]string) string {
	var parts, versions []string
	for v, t := range times {
		parts = append(parts, `"`+v+`":"`+t+`"`)
		versions = append(versions, `"`+v+`":{"dist":{"integrity":"sha512-`+v+`"}}`)
	}
	return `{"name":"x","time":{` + strings.Join(parts, ",") + `},"versions":{` + strings.Join(versions, ",") + `}}`
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
	// A package outside node_modules (a workspace) is named by its path, not cut at a fixed offset.
	if got, err := NpmLockPackages([]byte(`{"packages":{"x":{"version":"1.0.0"}}}`)); err != nil || strings.Join(got, " ") != "x@1.0.0" {
		t.Errorf("NpmLockPackages(workspace) = %q, %v", got, err)
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
	// A local directory has no publication time: its age cannot be checked.
	local := goModules + `{"Path":"example.com/local","Version":"v1.0.0","Replace":{"Path":"../local"}}` + "\n"
	if _, err := GoModuleVersions(local); err == nil || !strings.Contains(err.Error(), "example.com/local is replaced by the local directory ../local") {
		t.Errorf("GoModuleVersions(local replace) error = %v", err)
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
	c := newChecker(healthyRepo(), checkModes["check"])
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
		"timed out": {func(s *fakeSystem) {
			s.gets["https://registry.npmjs.org/react"] = fakeResult{err: errors.New(`Get "https://registry.npmjs.org/react": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`)}
		}, statusNotRun, "network"},
		// The age is checked in the registry, so the lockfile must install exactly what the registry has.
		"resolved elsewhere": {func(s *fakeSystem) {
			s.files["frontend/package-lock.json"] = strings.Replace(lockfile, "https://registry.npmjs.org/react/-/react-19.3.0.tgz", "https://evil.example/react.tgz", 1)
		}, statusFail, "node_modules/react: resolved from https://evil.example/react.tgz, not from https://registry.npmjs.org/"},
		"other integrity": {func(s *fakeSystem) {
			s.files["frontend/package-lock.json"] = strings.Replace(lockfile, `"integrity":"sha512-19.3.0"`, `"integrity":"sha512-evil"`, 1)
		}, statusFail, "node_modules/react: integrity sha512-evil differs from the registry's sha512-19.3.0"},
		"no lockfile":     {func(s *fakeSystem) { delete(s.files, "frontend/package-lock.json") }, statusFail, "package-lock.json"},
		"bad lockfile":    {func(s *fakeSystem) { s.files["frontend/package-lock.json"] = "{" }, statusFail, "package-lock.json"},
		"go list fails":   {func(s *fakeSystem) { s.results[goListModules] = []fakeResult{{err: exitStatus1}} }, statusFail, "go list -m -json all"},
		"go list garbage": {func(s *fakeSystem) { s.results[goListModules] = []fakeResult{{out: "{"}} }, statusFail, "go list -m -json all"},
	}
	for name, tc := range cases {
		sys := healthyRepo()
		tc.change(sys)
		c := newChecker(sys, checkModes["check"])
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
	c := newChecker(sys, checkModes["check"])
	c.releaseAges()
	got := c.updates()
	want := "3 updates out of quarantine: react 19.3.0 → 19.4.0, github.com/BurntSushi/toml v1.4.0 → v1.5.0, golang.org/x/vuln v1.8.0 → v1.9.0"
	if got.status != statusPass || got.detail != want {
		t.Errorf("updates = %+v", got)
	}
	c = newChecker(healthyRepo(), checkModes["check"])
	if got := c.updates(); got.status != statusPass || got.detail != "no update has been out of quarantine for 14 days" {
		t.Errorf("no updates: %+v", got)
	}
}

// A query that could not be answered is not_run, never an empty report.
func TestUpdatesDoesNotRunWhenItCannotAsk(t *testing.T) {
	const offline = `{"error":{"code":"ENOTFOUND","summary":"request to https://registry.npmjs.org/react failed, reason: getaddrinfo ENOTFOUND registry.npmjs.org"}}`
	cases := map[string]struct {
		change func(*fakeSystem)
		status string
		want   string
	}{
		"npm outdated garbage": {func(s *fakeSystem) { s.results[npmOutdated] = []fakeResult{{out: "npm ERR!", err: exitStatus1}} }, statusFail, "npm outdated --json"},
		"npm outdated offline": {func(s *fakeSystem) { s.results[npmOutdated] = []fakeResult{{out: offline, err: exitStatus1}} }, statusNotRun, "ENOTFOUND"},
		"npm outdated error": {func(s *fakeSystem) {
			s.results[npmOutdated] = []fakeResult{{out: `{"error":{"code":"E403","summary":"Forbidden"}}`, err: exitStatus1}}
		}, statusFail, "E403 Forbidden"},
		"publication times unavailable": {func(s *fakeSystem) {
			s.results[npmOutdated] = []fakeResult{{out: `{"react":{"current":"19.3.0","latest":"19.4.0"}}`, err: exitStatus1}}
			s.gets["https://registry.npmjs.org/react"] = fakeResult{err: errors.New("dial tcp: no such host")}
		}, statusNotRun, "react: dial tcp: no such host"},
		"go list fails":   {func(s *fakeSystem) { s.results[goListUpdates] = []fakeResult{{err: exitStatus1}} }, statusNotRun, "go list -m -u -json all: exit status 1"},
		"go list garbage": {func(s *fakeSystem) { s.results[goListUpdates] = []fakeResult{{out: "{"}} }, statusFail, "go list -m -u -json all"},
	}
	for name, c := range cases {
		sys := healthyRepo()
		c.change(sys)
		if got := newChecker(sys, checkModes["check"]).updates(); got.status != c.status || !strings.Contains(got.detail+"\n"+got.output, c.want) {
			t.Errorf("%s: updates = %+v, want %s with %q", name, got, c.status, c.want)
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
			// A finding whose text mentions a timeout is still a finding.
			"finding about a timeout": {fakeResult{out: "1 high severity vulnerability: i/o timeout in the parser\nVulnerability #1: GO-2026-0001", err: exitStatus1}, statusFail},
		} {
			sys := healthyRepo()
			sys.results[command] = []fakeResult{tc.answer}
			c := newChecker(sys, checkModes["check"])
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
