package devtool

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// tsEscapeHatches hide code from coverage, lint or the compiler, or switch tests off.
	tsEscapeHatches = regexp.MustCompile(`\b(?:v8|c8|istanbul) ignore\b|\b(?:eslint|oxlint)-disable|@ts-(?:ignore|expect-error|nocheck)\b|` +
		`\b(?:it|test|describe)(?:\.\w+)*\.(?:skip|only|todo|skipIf|runIf|fails)\b|\bx(?:it|test|describe)\(`)
	goEscapeHatches     = regexp.MustCompile(`//\s*nolint\b`)
	goTestEscapeHatches = regexp.MustCompile(`//\s*nolint\b|\bt\.Skip(?:Now|f)?\(`)
)

// EscapeHatches lists every line of own frontend sources and tests, and of Go sources and tests, that
// hides code from coverage, lint or the compiler, or switches a test off (TDD_AND_QUALITY "fixed measurement boundaries").
func EscapeHatches(sources map[string]string) []string {
	var found []string
	for _, name := range sortedKeys(sources) {
		var re *regexp.Regexp
		switch {
		case strings.HasPrefix(name, "frontend/src/"):
			re = tsEscapeHatches
		case strings.HasSuffix(name, "_test.go"):
			re = goTestEscapeHatches
		case strings.HasSuffix(name, ".go"):
			re = goEscapeHatches
		default:
			continue
		}
		for i, line := range strings.Split(sources[name], "\n") {
			if m := re.FindString(line); m != "" {
				found = append(found, fmt.Sprintf("%s:%d: %s", name, i+1, m))
			}
		}
	}
	return found
}
