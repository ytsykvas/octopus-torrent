package devtool

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"
)

const usage = `usage: devtool workspace setup | workspace run | check-docs | codex-agents | check-plan | plan-nav | check [offline | fast | deps]`

// System is the boundary to the operating system, replaced by a fake in tests.
type System interface {
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte) error
	// Run starts a program with the caller's standard streams and waits for it.
	Run(name string, args ...string) error
	// Output starts a program and returns its standard output.
	Output(name string, args ...string) (string, error)
	// Exec replaces the current process with the program at path; it returns only on failure.
	Exec(path string, argv []string) error
	// Capture starts a program and returns its standard output and error combined.
	Capture(name string, args ...string) (string, error)
	RemoveAll(path string) error
	MkdirAll(path string) error
	// ReadDir returns the names of the entries in a directory.
	ReadDir(path string) ([]string, error)
	Sleep(d time.Duration)
	// Get downloads a URL; a status other than 200 is an error.
	Get(url string) ([]byte, error)
	Now() time.Time
}

// Main dispatches a devtool command and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer, getenv func(string) string, sys System) int {
	command := strings.Join(args, " ")
	if mode, ok := checkModes[command]; ok {
		return runCheck(mode, stdout, sys)
	}
	var err error
	switch command {
	case "workspace setup":
		err = workspaceSetup(sys)
	case "workspace run":
		err = workspaceRun(getenv, sys)
	case "check-docs":
		err = checkDocs(stdout, sys)
	case "codex-agents":
		err = codexAgents(sys)
	case "check-plan":
		err = checkPlan(stdout, sys)
	case "plan-nav":
		err = planNav(stdout, sys)
	default:
		fmt.Fprintln(stderr, usage)
		return 64
	}
	if err != nil {
		fmt.Fprintf(stderr, "devtool: %v\n", err)
		return 1
	}
	return 0
}

// repositoryFiles lists tracked and untracked, not ignored, files relative to the repository root.
// -z keeps every name as it is: the plain listing quotes names with unusual characters.
func repositoryFiles(sys System) ([]string, error) {
	out, err := sys.Output("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list repository files: %w", err)
	}
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 }), nil
}

// listedPath reports whether p is one of the listed files or a directory containing one.
func listedPath(files []string) func(string) bool {
	return func(p string) bool {
		for _, f := range files {
			if f == p || strings.HasPrefix(f, p+"/") {
				return true
			}
		}
		return false
	}
}

// sortedKeys returns the keys of m in order, so every report lists files the same way.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
