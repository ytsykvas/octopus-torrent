package devtool

import (
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

// releaseTags are the build tags wails build gives the product's release binary on macOS; the
// isolation step lists the dependencies under them, so it sees what the shipped binary links.
const releaseTags = "desktop,wv2runtime.download,production"

// isolation proves that the product binary links no spike, no test support and no engine.
func (c *checker) isolation() stepResult {
	out, err := c.sys.Capture("go", "list", "-tags", releaseTags, "-deps", "-f", "{{.ImportPath}}", ".")
	if err != nil {
		return fail("go list -deps .: "+err.Error(), out)
	}
	gomod, _ := c.sys.ReadFile("go.mod")
	deps := strings.Fields(out)
	if found := CheckProductIsolation(GoModulePath(string(gomod)), deps); len(found) > 0 {
		return fail("the product links packages it must not (the spike, test support, or the engine before 018)", strings.Join(found, "\n"))
	}
	return pass("the product links none of the spike, test support or the engine (" + strconv.Itoa(len(deps)) + " packages)")
}
