package devtool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	npmRegistry  = "https://registry.npmjs.org/"
	goProxy      = "https://proxy.golang.org/"
	fetchWorkers = 8
	day          = 24 * time.Hour
)

// networkError recognizes a missing network in tool output and Go errors, as opposed to a finding.
var networkError = regexp.MustCompile(`ENOTFOUND|EAI_AGAIN|ECONNREFUSED|ECONNRESET|ETIMEDOUT|no such host|dial tcp|network is unreachable|i/o timeout|connection refused|Client\.Timeout exceeded|context deadline exceeded|TLS handshake timeout`)

// npmLockEntry is one installed package of an npm lockfile: where it is installed, its real name
// (an alias is listed under the package it installs), and the artifact npm ci downloads.
type npmLockEntry struct {
	path, name, version, resolved, integrity string
}

// npmLockEntries returns every installed package of an npm lockfile (v2/v3 "packages"), by path.
func npmLockEntries(lock []byte) ([]npmLockEntry, error) {
	var file struct {
		Packages map[string]struct {
			Name, Version, Resolved, Integrity string
			Link                               bool
		} `json:"packages"`
	}
	if err := json.Unmarshal(lock, &file); err != nil {
		return nil, err
	}
	var entries []npmLockEntry
	for _, path := range sortedKeys(file.Packages) {
		p := file.Packages[path]
		if path == "" || p.Link || p.Version == "" {
			continue
		}
		name := p.Name
		if i := strings.LastIndex(path, "node_modules/"); name == "" && i >= 0 {
			name = path[i+len("node_modules/"):]
		} else if name == "" {
			name = path
		}
		entries = append(entries, npmLockEntry{path, name, p.Version, p.Resolved, p.Integrity})
	}
	return entries, nil
}

// NpmLockPackages returns every installed package version of an npm lockfile as name@version,
// sorted and unique.
func NpmLockPackages(lock []byte) ([]string, error) {
	entries, err := npmLockEntries(lock)
	if err != nil {
		return nil, err
	}
	found := make([]string, len(entries))
	for i, e := range entries {
		found[i] = e.name + "@" + e.version
	}
	slices.Sort(found)
	return slices.Compact(found), nil
}

type goModule struct {
	Path, Version  string
	Main, Indirect bool
	Replace        *struct{ Path, Version string }
	Update         *struct{ Version, Time string }
}

func decodeGoModules(list string) ([]goModule, error) {
	var mods []goModule
	dec := json.NewDecoder(strings.NewReader(list))
	for {
		var m goModule
		if err := dec.Decode(&m); errors.Is(err, io.EOF) {
			return mods, nil
		} else if err != nil {
			return nil, err
		}
		mods = append(mods, m)
	}
}

// GoModuleVersions returns path@version of every module in the build list (`go list -m -json all`)
// as it is actually downloaded. The main module has no published version; a module replaced by a
// local directory has none either, so its age cannot be checked and it is an error.
func GoModuleVersions(list string) ([]string, error) {
	mods, err := decodeGoModules(list)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, m := range mods {
		path, version := m.Path, m.Version
		if m.Replace != nil {
			path, version = m.Replace.Path, m.Replace.Version
		}
		if m.Main {
			continue
		}
		if version == "" {
			return nil, fmt.Errorf("%s is replaced by the local directory %s, whose age cannot be checked", m.Path, path)
		}
		found = append(found, path+"@"+version)
	}
	slices.Sort(found)
	return found, nil
}

// EscapeModulePath escapes a module path or version for the module proxy protocol: every capital
// letter becomes "!" and its lower case.
func EscapeModulePath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ReleaseAgeProblem describes a version published less than minReleaseAgeDays before now, or "".
func ReleaseAgeProblem(version string, published, now time.Time) string {
	age := now.Sub(published)
	if age >= minReleaseAgeDays*day {
		return ""
	}
	return fmt.Sprintf("%s was published %s, %d days ago; the minimum is %d", version, published.Format(time.DateOnly), int(age/day), minReleaseAgeDays)
}

// splitVersion splits name@version at the last "@", so scoped npm names keep their leading "@".
func splitVersion(s string) (string, string) {
	i := strings.LastIndexByte(s, '@')
	return s[:i], s[i+1:]
}

func npmURL(name string) string { return npmRegistry + strings.Replace(name, "/", "%2f", 1) }

func goInfoURL(path, version string) string {
	return goProxy + EscapeModulePath(path) + "/@v/" + EscapeModulePath(version) + ".info"
}

type fetched struct {
	body []byte
	err  error
}

// fetch downloads the URLs with a bounded number of concurrent requests.
func (c *checker) fetch(urls []string) map[string]fetched {
	results := make([]fetched, len(urls))
	var wg sync.WaitGroup
	slots := make(chan struct{}, fetchWorkers)
	for i, url := range urls {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			body, err := c.sys.Get(url)
			results[i] = fetched{body, err}
		})
	}
	wg.Wait()
	byURL := map[string]fetched{}
	for i, url := range urls {
		byURL[url] = results[i]
	}
	return byURL
}

// network marks a step that needs the network; the offline and fast modes skip it.
func (c *checker) network(step func() stepResult) func() stepResult {
	return func() stepResult {
		if !c.mode.network {
			return notRun("skipped: needs the network")
		}
		return step()
	}
}

// releaseAges checks that every locked npm package version and every Go module in the build list
// was published at least minReleaseAgeDays ago, by the registry's publication time.
func (c *checker) releaseAges() stepResult {
	lock, err := c.sys.ReadFile("frontend/package-lock.json")
	if err != nil {
		return fail("read frontend/package-lock.json: "+err.Error(), "")
	}
	entries, err := npmLockEntries(lock)
	if err != nil {
		return fail("frontend/package-lock.json: "+err.Error(), "")
	}
	npm, _ := NpmLockPackages(lock)
	list, err := c.sys.Output("go", "list", "-m", "-json", "all")
	if err != nil {
		return fail("go list -m -json all: "+err.Error(), list)
	}
	mods, err := GoModuleVersions(list)
	if err != nil {
		return fail("go list -m -json all: "+err.Error(), "")
	}
	var urls []string
	for _, p := range npm {
		name, _ := splitVersion(p)
		urls = append(urls, npmURL(name))
	}
	for _, m := range mods {
		urls = append(urls, goInfoURL(splitVersion(m)))
	}
	slices.Sort(urls)
	answers := c.fetch(slices.Compact(urls))
	for _, url := range sortedKeys(answers) {
		if err := answers[url].err; err != nil && networkError.MatchString(err.Error()) {
			return notRun("network unavailable: " + err.Error())
		}
	}
	now := c.sys.Now()
	var problems []string
	check := func(version, published string, err error) {
		t, perr := time.Parse(time.RFC3339, published)
		switch {
		case err != nil:
			problems = append(problems, version+": "+err.Error())
		case published == "" || perr != nil:
			problems = append(problems, version+": no publication time in the registry")
		default:
			if p := ReleaseAgeProblem(version, t, now); p != "" {
				problems = append(problems, p)
			}
		}
	}
	for _, p := range npm {
		name, version := splitVersion(p)
		doc, err := c.parsePackument(name, answers[npmURL(name)])
		check(p, doc.Time[version], err)
	}
	// The age was checked in the registry, so npm ci must install exactly the registry's artifact.
	for _, e := range entries {
		doc := c.npmDocs[e.name]
		switch want := doc.Versions[e.version].Dist.Integrity; {
		case !strings.HasPrefix(e.resolved, npmRegistry):
			problems = append(problems, e.path+": resolved from "+e.resolved+", not from "+npmRegistry)
		case doc.Versions != nil && e.integrity != want:
			problems = append(problems, e.path+": integrity "+e.integrity+" differs from the registry's "+want)
		}
	}
	for _, m := range mods {
		var info struct{ Time string }
		a := answers[goInfoURL(splitVersion(m))]
		err := a.err
		if err == nil {
			err = json.Unmarshal(a.body, &info)
		}
		check(m, info.Time, err)
	}
	if len(problems) > 0 {
		return fail(fmt.Sprintf("%d versions are younger than %d days or have no publication time", len(problems), minReleaseAgeDays), strings.Join(problems, "\n"))
	}
	return pass(fmt.Sprintf("%d npm package versions and %d Go modules are at least %d days old", len(npm), len(mods), minReleaseAgeDays))
}

// npmDocument is what the gate reads from an npm registry document: the publication time of every
// version and the integrity of its artifact.
type npmDocument struct {
	Time     map[string]string
	Versions map[string]struct{ Dist struct{ Integrity string } }
}

// parsePackument reads an npm registry document and remembers it for the other checks.
func (c *checker) parsePackument(name string, a fetched) (npmDocument, error) {
	if doc, ok := c.npmDocs[name]; ok {
		return doc, nil
	}
	if a.err != nil {
		return npmDocument{}, a.err
	}
	var doc npmDocument
	if err := json.Unmarshal(a.body, &doc); err != nil {
		return npmDocument{}, err
	}
	c.npmDocs[name] = doc
	return doc, nil
}

// updates reports the newer versions of direct dependencies and tools that have been out of
// quarantine for minReleaseAgeDays: the policy says they are taken, not postponed. Transitive Go
// modules are left to MVS. Found updates do not fail the gate; a query that could not be answered
// does not run, so an empty report always means that nothing is due.
func (c *checker) updates() stepResult {
	now := c.sys.Now()
	settled := func(published string) bool {
		t, err := time.Parse(time.RFC3339, published)
		return err == nil && now.Sub(t) >= minReleaseAgeDays*day
	}
	// npm outdated exits 1 when something is outdated; its JSON is the answer either way, and an
	// npm error is a JSON object with an error code.
	out, _ := c.sys.Output("npm", "--prefix", "frontend", "outdated", "--json")
	var outdated map[string]struct{ Current, Latest, Code, Summary string }
	if err := json.Unmarshal([]byte(out), &outdated); err != nil {
		return fail("npm outdated --json: "+err.Error(), out)
	}
	if e, ok := outdated["error"]; ok && e.Code != "" {
		if networkError.MatchString(e.Code + " " + e.Summary) {
			return notRun("network unavailable: npm outdated: " + e.Summary)
		}
		return fail("npm outdated: "+e.Code+" "+e.Summary, out)
	}
	var found []string
	for _, name := range sortedKeys(outdated) {
		o := outdated[name]
		doc, err := c.parsePackument(name, c.fetch([]string{npmURL(name)})[npmURL(name)])
		if err != nil {
			return notRun("cannot read the publication times of " + name + ": " + err.Error())
		}
		if settled(doc.Time[o.Latest]) {
			found = append(found, name+" "+o.Current+" → "+o.Latest)
		}
	}
	list, err := c.sys.Output("go", "list", "-m", "-u", "-json", "all")
	if err != nil {
		return notRun("cannot look up Go module updates: go list -m -u -json all: " + err.Error())
	}
	mods, err := decodeGoModules(list)
	if err != nil {
		return fail("go list -m -u -json all: "+err.Error(), list)
	}
	gomod, _ := c.sys.ReadFile("go.mod")
	tools := GoTools(string(gomod))
	isTool := func(module string) bool {
		return slices.ContainsFunc(tools, func(t string) bool { return t == module || strings.HasPrefix(t, module+"/") })
	}
	for _, m := range mods {
		if m.Update != nil && settled(m.Update.Time) && (!m.Indirect || isTool(m.Path)) {
			found = append(found, m.Path+" "+m.Version+" → "+m.Update.Version)
		}
	}
	if len(found) == 0 {
		return pass(fmt.Sprintf("no update has been out of quarantine for %d days", minReleaseAgeDays))
	}
	return pass(fmt.Sprintf("%d updates out of quarantine: %s", len(found), strings.Join(found, ", ")))
}

// auditFinding is how npm audit and govulncheck report a vulnerability; such output is a finding even
// if its text mentions a network error.
var auditFinding = regexp.MustCompile(`\d+ (?:low|moderate|high|critical) severity vulnerabilit|Vulnerability #\d+`)

// audit runs a vulnerability scanner: a finding fails the step, a missing network does not run it.
func (c *checker) audit(command ...string) func() stepResult {
	return func() stepResult {
		out, err := c.sys.Capture(command[0], command[1:]...)
		switch {
		case err == nil:
			return pass(strings.Join(command, " "))
		case networkError.MatchString(out) && !auditFinding.MatchString(out):
			return notRun("network unavailable: " + strings.Join(command, " "))
		}
		return fail(strings.Join(command, " ")+": "+err.Error(), out)
	}
}
