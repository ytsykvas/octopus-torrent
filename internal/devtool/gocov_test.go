package devtool

import (
	"strings"
	"testing"
)

var goSources = map[string]string{
	"main.go": "package main\n\nimport \"os\"\n\nfunc main() { os.Exit(run()) }\n\nfunc run() int {\n\treturn 0\n}\n",
	"internal/app/app.go": "package app\n\n// App is the facade.\ntype App struct{}\n\n// New returns the facade.\n" +
		"func New() *App { return &App{} }\n",
	"internal/app/types.go":     "package app\n\n// Info has no executable code.\ntype Info struct{ Name string }\n",
	"internal/app/app_test.go":  "package app\n\nfunc helper() {}\n",
	"cmd/tool/main.go":          "package main\n\nfunc main() {\n\tprintln(\"x\")\n}\n",
	"frontend/wailsjs/x.go.txt": "not go",
}

const (
	unitProfile = "mode: atomic\n" +
		"octopustorrent/main.go:5.13,5.31 1 0\n" +
		"octopustorrent/main.go:7.16,9.2 1 3\n" +
		"octopustorrent/internal/app/app.go:7.18,7.33 1 2\n" +
		"octopustorrent/cmd/tool/main.go:3.13,5.2 1 1\n"
	appProfile = "mode: atomic\n" +
		"octopustorrent/main.go:5.13,5.31 1 1\n" +
		"octopustorrent/main.go:7.16,9.2 1 1\n" +
		"octopustorrent/internal/app/app.go:7.18,7.33 1 0\n"
)

func TestCheckGoCoverageMergesTheUnitAndAppProfiles(t *testing.T) {
	problems, deferred := CheckGoCoverage("octopustorrent", goSources, unitProfile, appProfile, true)
	if len(problems) != 0 || len(deferred) != 0 {
		t.Errorf("CheckGoCoverage = %q deferred %q, want nothing", problems, deferred)
	}
}

func TestCheckGoCoverageDefersOnlyMainToTheAppRun(t *testing.T) {
	problems, deferred := CheckGoCoverage("octopustorrent", goSources, unitProfile, "", false)
	if len(problems) != 0 || strings.Join(deferred, " ") != "main.go:5.13,5.31" {
		t.Errorf("CheckGoCoverage = %q deferred %q, want only main() deferred", problems, deferred)
	}
}

func TestCheckGoCoverageRejectsEveryWayAroundFullCoverage(t *testing.T) {
	sources := func(extra string, content string) map[string]string {
		s := map[string]string{}
		for k, v := range goSources {
			s[k] = v
		}
		s[extra] = content
		return s
	}
	cases := map[string]struct {
		sources   map[string]string
		unit, app string
		withApp   bool
		want      string
	}{
		"uncovered main": {goSources, unitProfile, strings.Replace(appProfile, "5.13,5.31 1 1", "5.13,5.31 1 0", 1), true,
			"main.go:5.13,5.31 is not covered"},
		"uncovered block, unit only": {goSources, strings.Replace(unitProfile, "7.16,9.2 1 3", "7.16,9.2 1 0", 1), "", false,
			"main.go:7.16,9.2 is not covered"},
		"missing unit profile":  {goSources, "", appProfile, true, "the unit coverage profile is missing or empty"},
		"empty unit profile":    {goSources, "mode: atomic\n", appProfile, true, "the unit coverage profile is missing or empty"},
		"missing app profile":   {goSources, unitProfile, "", true, "the .app coverage profile is missing or empty"},
		"app without main":      {goSources, unitProfile, "mode: atomic\noctopustorrent/internal/app/app.go:7.18,7.33 1 1\n", true, "the .app profile does not contain main.go"},
		"incompatible profiles": {goSources, unitProfile, strings.Replace(appProfile, "app.go:7.18,7.33", "app.go:7.18,7.40", 1), true, "incompatible profiles for internal/app/app.go"},
		"mixed modes":           {goSources, unitProfile, strings.Replace(appProfile, "mode: atomic", "mode: set", 1), true, "coverage modes differ: atomic and set"},
		"file not measured": {sources("internal/app/extra.go", "package app\n\nfunc Extra() int { return 1 }\n"), unitProfile, appProfile, true,
			"internal/app/extra.go has functions but is not in the coverage profile"},
		"unparsable source": {sources("internal/app/broken.go", "package app\nfunc {"), unitProfile, appProfile, true, "internal/app/broken.go: cannot parse"},
		"malformed line":    {goSources, unitProfile + "garbage\n", appProfile, true, "malformed coverage line \"garbage\""},
		"Go under testdata": {sources("internal/x/testdata/gen.go", "package main\n\nfunc main() {}\n"), unitProfile, appProfile, true,
			"internal/x/testdata/gen.go: Go source under testdata/ is never built or measured; put it in a package"},
		"Go test under testdata": {sources("internal/x/testdata/gen_test.go", "package x\n"), unitProfile, appProfile, true,
			"internal/x/testdata/gen_test.go: Go source under testdata/ is never built or measured; put it in a package"},
	}
	for name, c := range cases {
		problems, _ := CheckGoCoverage("octopustorrent", c.sources, c.unit, c.app, c.withApp)
		if got := strings.Join(problems, "\n"); !strings.Contains(got, c.want) {
			t.Errorf("%s: CheckGoCoverage =\n%s\nwant a problem containing %q", name, got, c.want)
		}
	}
}

func TestGoModulePathReadsTheModuleLine(t *testing.T) {
	if got := GoModulePath("// comment\nmodule octopustorrent\n\ngo 1.27.2\n"); got != "octopustorrent" {
		t.Errorf("GoModulePath = %q", got)
	}
	if got := GoModulePath("go 1.27.2\n"); got != "" {
		t.Errorf("GoModulePath without a module line = %q", got)
	}
}

func TestCheckGoCoverageTakesFilesOnlyTheAppMeasured(t *testing.T) {
	sources := map[string]string{"main.go": goSources["main.go"], "internal/platform/menu.go": "package platform\n\nfunc Menu() {}\n"}
	unit := "mode: atomic\noctopustorrent/main.go:5.13,5.31 1 0\noctopustorrent/main.go:7.16,9.2 1 1\n"
	app := "mode: atomic\noctopustorrent/main.go:5.13,5.31 1 1\noctopustorrent/main.go:7.16,9.2 1 1\n" +
		"octopustorrent/internal/platform/menu.go:3.13,3.15 0 1\n"
	if problems, _ := CheckGoCoverage("octopustorrent", sources, unit, app, true); len(problems) != 0 {
		t.Errorf("CheckGoCoverage = %q, want the .app-only file merged", problems)
	}
}

func TestGoToolsReadsBothDirectiveForms(t *testing.T) {
	gomod := "module m\n\ntool (\n\tgithub.com/wailsapp/wails/v2/cmd/wails\n\tm/cmd/devtool\n)\n\ntool golang.org/x/vuln/cmd/govulncheck\n"
	if got := strings.Join(GoTools(gomod), " "); got != "github.com/wailsapp/wails/v2/cmd/wails m/cmd/devtool golang.org/x/vuln/cmd/govulncheck" {
		t.Errorf("GoTools = %q", got)
	}
}
