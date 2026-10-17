package devtool

import (
	"encoding/json"
	"fmt"
	"strings"
)

// frontendMetrics are the four Vitest coverage metrics that must each be exactly 100% per file.
var frontendMetrics = []string{"lines", "statements", "functions", "branches"}

// FrontendInventory returns the own executable TypeScript sources: everything under frontend/src
// except tests, the test setup and type declarations. It is derived from the repository listing,
// not from the Vitest configuration, so narrowing `include` cannot shrink it.
func FrontendInventory(files []string) []string {
	var own []string
	for _, f := range files {
		if !strings.HasPrefix(f, "frontend/src/") || strings.HasPrefix(f, "frontend/src/test/") ||
			!(strings.HasSuffix(f, ".ts") || strings.HasSuffix(f, ".tsx")) ||
			strings.HasSuffix(f, ".d.ts") || strings.Contains(f, ".test.") {
			continue
		}
		own = append(own, f)
	}
	return own
}

type coverageMetric struct {
	Total   *int `json:"total"`
	Covered int  `json:"covered"`
	Skipped int  `json:"skipped"`
}

// CheckFrontendCoverage verifies the Vitest json-summary report against the inventory: every file
// is in the report and every metric is covered == total by raw counts (the rounded pct is ignored),
// with nothing skipped. Report keys are absolute paths under root.
func CheckFrontendCoverage(inventory []string, report []byte, root string) []string {
	if len(report) == 0 {
		return []string{"the frontend coverage report is missing or empty"}
	}
	var files map[string]map[string]coverageMetric
	if err := json.Unmarshal(report, &files); err != nil {
		return []string{"the frontend coverage report is not valid JSON: " + err.Error()}
	}
	delete(files, "total")
	if len(files) == 0 {
		return []string{"the frontend coverage report is missing or empty"}
	}
	byPath := map[string]map[string]coverageMetric{}
	for name, m := range files {
		byPath[strings.TrimPrefix(name, root+"/")] = m
	}
	var problems []string
	for _, f := range inventory {
		m, ok := byPath[f]
		if !ok {
			problems = append(problems, f+": not in the coverage report (unimported or excluded)")
			continue
		}
		for _, name := range frontendMetrics {
			metric, ok := m[name]
			switch {
			case !ok || metric.Total == nil:
				problems = append(problems, fmt.Sprintf("%s: no %s metric", f, name))
			case metric.Skipped > 0:
				problems = append(problems, fmt.Sprintf("%s: %s has %d skipped", f, name, metric.Skipped))
			case metric.Covered != *metric.Total:
				problems = append(problems, fmt.Sprintf("%s: %s %d/%d", f, name, metric.Covered, *metric.Total))
			}
		}
	}
	return problems
}
