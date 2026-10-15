package devtool

import (
	"strconv"
	"strings"
	"testing"
)

func metrics(covered, total int) string {
	m := `{"total":` + strconv.Itoa(total) + `,"covered":` + strconv.Itoa(covered) + `,"skipped":0,"pct":100}`
	return `{"lines":` + m + `,"statements":` + m + `,"functions":` + m + `,"branches":` + m + `}`
}

const frontendRoot = "/work/repo"

func summary(entries ...string) []byte {
	return []byte(`{"total":` + metrics(1, 1) + `,` + strings.Join(entries, ",") + `}`)
}

func entry(file, metrics string) string { return `"` + frontendRoot + `/` + file + `":` + metrics }

func TestFrontendInventoryListsOwnExecutableSources(t *testing.T) {
	files := []string{
		"frontend/src/App.tsx", "frontend/src/App.test.tsx", "frontend/src/bridge/runtimeInfo.ts",
		"frontend/src/bridge/runtimeInfo.test.ts", "frontend/src/test/setup.ts", "frontend/src/vite-env.d.ts",
		"frontend/src/styles/app.css", "frontend/wailsjs/go/app/App.js", "frontend/vite.config.ts", "main.go",
	}
	got := strings.Join(FrontendInventory(files), " ")
	if got != "frontend/src/App.tsx frontend/src/bridge/runtimeInfo.ts" {
		t.Errorf("FrontendInventory = %q", got)
	}
}

func TestCheckFrontendCoverageAcceptsEveryFileAtExactlyFullCoverage(t *testing.T) {
	inventory := []string{"frontend/src/App.tsx", "frontend/src/types.ts"}
	report := summary(entry("frontend/src/App.tsx", metrics(12, 12)), entry("frontend/src/types.ts", metrics(0, 0)),
		entry("frontend/wailsjs/go/app/App.js", metrics(0, 4)))
	if got := CheckFrontendCoverage(inventory, report, frontendRoot); len(got) != 0 {
		t.Errorf("CheckFrontendCoverage = %q, want no problems", got)
	}
}

func TestCheckFrontendCoverageRejectsEveryWayAroundFullCoverage(t *testing.T) {
	inventory := []string{"frontend/src/App.tsx", "frontend/src/Unimported.ts"}
	full := entry("frontend/src/Unimported.ts", metrics(3, 3))
	skipped := `{"lines":{"total":4,"covered":4,"skipped":1,"pct":100},"statements":{"total":4,"covered":4,"skipped":0,"pct":100},` +
		`"functions":{"total":1,"covered":1,"skipped":0,"pct":100},"branches":{"total":2,"covered":2,"skipped":0,"pct":100}}`
	rounded := `{"lines":{"total":10000,"covered":9999,"skipped":0,"pct":100},"statements":{"total":4,"covered":4,"skipped":0,"pct":100},` +
		`"functions":{"total":1,"covered":1,"skipped":0,"pct":100},"branches":{"total":2,"covered":2,"skipped":0,"pct":100}}`
	cases := map[string]struct {
		report []byte
		want   string
	}{
		"uncovered file":         {summary(entry("frontend/src/App.tsx", metrics(0, 5)), full), "frontend/src/App.tsx: lines 0/5"},
		"unimported or excluded": {summary(entry("frontend/src/App.tsx", metrics(5, 5))), "frontend/src/Unimported.ts: not in the coverage report"},
		"rounded to 100":         {summary(entry("frontend/src/App.tsx", rounded), full), "lines 9999/10000"},
		"skipped by a directive": {summary(entry("frontend/src/App.tsx", skipped), full), "lines has 1 skipped"},
		"missing report":         {nil, "coverage report is missing or empty"},
		"empty report":           {[]byte(`{}`), "coverage report is missing or empty"},
		"not json":               {[]byte(`<html>`), "coverage report is not valid JSON"},
		"missing metric":         {summary(entry("frontend/src/App.tsx", `{"lines":{"total":1,"covered":1}}`), full), "frontend/src/App.tsx: no statements metric"},
		"relative paths unknown": {[]byte(`{"total":` + metrics(1, 1) + `,"src/App.tsx":` + metrics(5, 5) + `,` + full + `}`), "frontend/src/App.tsx: not in the coverage report"},
	}
	for name, c := range cases {
		got := strings.Join(CheckFrontendCoverage(inventory, c.report, frontendRoot), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: CheckFrontendCoverage =\n%s\nwant a problem containing %q", name, got, c.want)
		}
	}
}

func TestEscapeHatchesFindsIgnoreDirectivesAndDisabledTests(t *testing.T) {
	sources := map[string]string{
		"frontend/src/App.tsx":                 "/* v8 ignore next */\nexport const a = 1\n",
		"frontend/src/b.ts":                    "// istanbul ignore else\n",
		"frontend/src/c.ts":                    "/* c8 ignore start */\n",
		"frontend/src/d.ts":                    "// oxlint-disable-next-line no-console\n",
		"frontend/src/e.ts":                    "/* eslint-disable */\n",
		"frontend/src/f.test.ts":               "it.skip('x', () => {})\ndescribe.only('y', () => {})\ntest.todo('z')\n",
		"frontend/src/g.ts":                    "// @ts-ignore\n// @ts-expect-error\n// @ts-nocheck\n",
		"frontend/src/h.test.ts":               "it.skipIf(ci)('a')\ndescribe.runIf(mac)('b')\ntest.fails('c')\nxit('d')\nxdescribe('e')\nxtest('f')\nit.concurrent.skip('g')\n",
		"internal/app/app.go":                  "_ = f() // no" + "lint:errcheck\n",
		"internal/app/app_test.go":             "func TestX(t *testing.T) { t." + "Skip(\"later\") }\n", // split so this file has no real call
		"frontend/src/clean.ts":                "export const ignore = 'v8 is fast'; // skipping nothing\n",
		"internal/app/clean.go":                "// t.Skip is only forbidden in tests\n",
		"internal/devtool/frontendcov_test.go": "// fixtures mention v8 ignore on purpose\n",
	}
	got := strings.Join(EscapeHatches(sources), "\n")
	for _, want := range []string{
		"frontend/src/App.tsx:1: v8 ignore", "frontend/src/b.ts:1: istanbul ignore", "frontend/src/c.ts:1: c8 ignore",
		"frontend/src/d.ts:1: oxlint-disable", "frontend/src/e.ts:1: eslint-disable", "frontend/src/f.test.ts:1: it.skip",
		"frontend/src/f.test.ts:2: describe.only", "frontend/src/f.test.ts:3: test.todo", "internal/app/app_test.go:1: t." + "Skip(",
		"frontend/src/g.ts:1: @ts-ignore", "frontend/src/g.ts:2: @ts-expect-error", "frontend/src/g.ts:3: @ts-nocheck",
		"frontend/src/h.test.ts:1: it.skipIf", "frontend/src/h.test.ts:2: describe.runIf", "frontend/src/h.test.ts:3: test.fails",
		"frontend/src/h.test.ts:4: xit(", "frontend/src/h.test.ts:5: xdescribe(", "frontend/src/h.test.ts:6: xtest(",
		"frontend/src/h.test.ts:7: it.concurrent.skip", "internal/app/app.go:1: //" + " no" + "lint",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("EscapeHatches missing %q in:\n%s", want, got)
		}
	}
	for _, clean := range []string{"clean.ts", "internal/app/clean.go", "frontendcov_test.go"} {
		if strings.Contains(got, clean) {
			t.Errorf("EscapeHatches reported %s:\n%s", clean, got)
		}
	}
}
