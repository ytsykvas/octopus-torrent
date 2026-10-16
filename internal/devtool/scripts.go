package devtool

import (
	"fmt"
	"regexp"
	"strings"
)

// handOverLine is every line a shell script may hold besides comments and blank lines: strict mode,
// building devtool, and handing over to it. Logic belongs in Go, where it is tested and measured.
var handOverLine = regexp.MustCompile(`^(set -eu|go build -o build/bin/devtool \./cmd/devtool|exec build/bin/devtool( [a-z0-9-]+)+)$`)

// CheckScripts lists every own shell script (*.sh) that does more than hand over to devtool: shell
// has no coverage inventory, so a script may only build devtool and exec it.
func CheckScripts(sources map[string]string) []string {
	var problems []string
	for _, name := range sortedKeys(sources) {
		if !strings.HasSuffix(name, ".sh") {
			continue
		}
		lines := strings.Split(strings.TrimRight(sources[name], "\n"), "\n")
		if lines[0] != "#!/bin/sh" {
			problems = append(problems, name+": missing #!/bin/sh")
		}
		seen := map[string]bool{}
		for i, line := range lines[1:] {
			switch line = strings.TrimSpace(line); {
			case line == "" || strings.HasPrefix(line, "#"):
			case handOverLine.MatchString(line):
				seen[strings.Fields(line)[0]] = true
			default:
				problems = append(problems, fmt.Sprintf("%s:%d: %s", name, i+2, line))
			}
		}
		for _, required := range []struct{ word, text string }{{"set", "set -eu"}, {"exec", "exec build/bin/devtool"}} {
			if !seen[required.word] {
				problems = append(problems, name+": missing "+required.text)
			}
		}
	}
	return problems
}
