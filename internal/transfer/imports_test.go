package transfer

import (
	"go/build"
	"maps"
	"slices"
	"strings"
	"testing"
)

const module = "octopustorrent/"

// pureStd is the standard library the policy packages may import: nothing that reaches files,
// sockets, processes or the clock.
var pureStd = []string{"cmp", "context", "errors", "maps", "slices", "sync", "sync/atomic"}

// reachedPackages lists every package of this module the policy may reach, with the standard
// library each may import. contracts is the facade's DTO package: it validates paths lexically
// (path/filepath, never a file system call), makes IDs (crypto/rand) and reads JSON. A new import
// anywhere here fails the test until it is reviewed and listed.
var reachedPackages = map[string][]string{
	module + "internal/transfer":     pureStd,
	module + "internal/commandorder": pureStd,
	module + "internal/app/contracts": {
		"cmp", "crypto/rand", "crypto/sha256", "encoding/hex", "encoding/json", "errors", "fmt", "io",
		"maps", "path/filepath", "reflect", "regexp", "slices", "strconv", "strings", "time", "unicode",
	},
}

// The policy is computed without Wails, disk or network (task 013): the policy packages and every
// package of this module they reach import only listed packages. Read from the sources with
// go/build, so the check needs no go command and no network.
func TestThePolicyReachesNoWailsDiskOrNetwork(t *testing.T) {
	seen := map[string]bool{}
	queue := []string{module + "internal/transfer", module + "internal/commandorder"}
	for len(queue) > 0 {
		pkgPath := queue[0]
		queue = queue[1:]
		if seen[pkgPath] {
			continue
		}
		seen[pkgPath] = true
		allowedStd, listed := reachedPackages[pkgPath]
		if !listed {
			t.Errorf("the policy reaches %s, outside the pure policy", pkgPath)
			continue
		}
		pkg, err := build.ImportDir("../../"+strings.TrimPrefix(pkgPath, module), 0)
		if err != nil {
			t.Fatalf("read %s: %v", pkgPath, err)
		}
		for _, imported := range pkg.Imports {
			switch {
			case strings.HasPrefix(imported, module):
				queue = append(queue, imported)
			case !slices.Contains(allowedStd, imported):
				t.Errorf("%s imports %s, which is not on its list", pkgPath, imported)
			}
		}
	}
	if !seen[module+"internal/app/contracts"] {
		t.Fatalf("the walk never reached contracts (%v): it reads the wrong folders", slices.Sorted(maps.Keys(seen)))
	}
}
