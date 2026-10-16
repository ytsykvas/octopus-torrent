package devtool

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// handOverLine is every line a shell script may hold besides comments and blank lines: strict mode,
// building devtool, and handing over to it. Logic belongs in Go, where it is tested and measured.
var handOverLine = regexp.MustCompile(`^(set -eu|go build -o build/bin/devtool \./cmd/devtool|exec build/bin/devtool( [a-z0-9-]+)+)$`)

// CheckScripts lists every own script (a *.sh file, or any file starting with #!) that does more than
// hand over to devtool: shell has no coverage inventory, so a script may only set strict mode first,
// build devtool, and exec it last.
func CheckScripts(sources map[string]string) []string {
	var problems []string
	for _, name := range sortedKeys(sources) {
		if !strings.HasSuffix(name, ".sh") && !strings.HasPrefix(sources[name], "#!") {
			continue
		}
		lines := strings.Split(strings.TrimRight(sources[name], "\n"), "\n")
		if lines[0] != "#!/bin/sh" {
			problems = append(problems, name+": missing #!/bin/sh")
		}
		var commands []string
		for i, line := range lines[1:] {
			switch line = strings.TrimSpace(line); {
			case line == "" || strings.HasPrefix(line, "#"):
			case handOverLine.MatchString(line):
				commands = append(commands, line)
			default:
				problems = append(problems, fmt.Sprintf("%s:%d: %s", name, i+2, line))
			}
		}
		if len(commands) == 0 || commands[0] != "set -eu" {
			problems = append(problems, name+": missing set -eu as the first command")
		}
		if !slices.Contains(commands, "go build -o build/bin/devtool ./cmd/devtool") {
			problems = append(problems, name+": missing go build -o build/bin/devtool ./cmd/devtool")
		}
		if len(commands) == 0 || !strings.HasPrefix(commands[len(commands)-1], "exec ") {
			problems = append(problems, name+": missing exec build/bin/devtool as the last command")
		}
	}
	return problems
}
