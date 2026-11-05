package devtool

import (
	"slices"
	"strconv"
	"strings"
)

// CheckProductIsolation returns the packages of the product's build that must not be in it: the
// engine spike (a separate app, task 007), test support (testutil), and the torrent engine, which
// enters the product with its adapter in task 018. deps is `go list -deps` of the product's main.
func CheckProductIsolation(module string, deps []string) []string {
	var found []string
	for _, p := range deps {
		if p == module+"/internal/enginespike" || strings.HasPrefix(p, module+"/internal/enginespike/") ||
			strings.Contains(p+"/", "/testutil/") || strings.HasPrefix(p, "github.com/anacrolix/") {
			found = append(found, p)
		}
	}
	return found
}

// sqlImportsFormat lists an own package with every import of its files, its tests included, on one
// line.
const sqlImportsFormat = "{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}{{range .XTestImports}} {{.}}{{end}}"

// buildModes are the tag sets the own packages are built with: no tags, wails dev, the bindings
// generator (task 014's build-mode files), and the release build, whose tags a file may require.
var buildModes = []string{"", "dev", "bindings", releaseTags}

// CheckSQLIsolation returns what breaks "SQLite only through the repository" (STACK-04, task 015):
// an own package other than internal/library that imports database/sql or any SQLite package (in
// listing, lines of sqlImportsFormat), and a frontend package whose name names SQL. Sorted, each
// once.
func CheckSQLIsolation(module, listing string, npmPackages []string) []string {
	var found []string
	for _, line := range strings.Split(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == module+"/internal/library" {
			continue
		}
		for _, p := range fields[1:] {
			if p == "database/sql" || strings.HasPrefix(p, "database/sql/") || strings.Contains(strings.ToLower(p), "sqlite") {
				found = append(found, fields[0]+" imports "+p)
			}
		}
	}
	for _, name := range npmPackages {
		if strings.Contains(strings.ToLower(name), "sql") {
			found = append(found, "frontend/package-lock.json: "+name)
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

// releaseTags are the build tags wails build gives the product's release binary on macOS; the
// isolation step lists the dependencies under them, so it sees what the shipped binary links.
const releaseTags = "desktop,wv2runtime.download,production"

// isolation proves that the product binary links no spike, no test support and no engine, and that
// SQL lives only in internal/library: no other own package, in any build mode, and no frontend
// package, can reach SQLite.
func (c *checker) isolation() stepResult {
	out, err := c.sys.Capture("go", "list", "-tags", releaseTags, "-deps", "-f", "{{.ImportPath}}", ".")
	if err != nil {
		return fail("go list -deps .: "+err.Error(), out)
	}
	deps := strings.Fields(out)
	if found := CheckProductIsolation(c.module(), deps); len(found) > 0 {
		return fail("the product links packages it must not (the spike, test support, or the engine before 018)", strings.Join(found, "\n"))
	}
	var listing strings.Builder
	for _, tags := range buildModes {
		out, err := c.sys.Capture("go", "list", "-tags="+tags, "-f", sqlImportsFormat, "./...")
		if err != nil {
			return fail("go list -tags="+tags+" ./...: "+err.Error(), out)
		}
		listing.WriteString(out)
	}
	lock, err := c.sys.ReadFile("frontend/package-lock.json")
	if err != nil {
		return fail("read frontend/package-lock.json: "+err.Error(), "")
	}
	entries, err := npmLockEntries(lock)
	if err != nil {
		return fail("parse frontend/package-lock.json: "+err.Error(), "")
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	if found := CheckSQLIsolation(c.module(), listing.String(), names); len(found) > 0 {
		return fail("SQL outside internal/library (STACK-04)", strings.Join(found, "\n"))
	}
	return pass("the product links none of the spike, test support or the engine (" + strconv.Itoa(len(deps)) +
		" packages); SQL only in internal/library")
}
