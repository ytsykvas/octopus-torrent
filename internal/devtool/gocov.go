package devtool

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	moduleLine   = regexp.MustCompile(`(?m)^module\s+(\S+)`)
	profileBlock = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)
)

// GoModulePath returns the module path declared in go.mod, or "" if there is none.
func GoModulePath(gomod string) string {
	if m := moduleLine.FindStringSubmatch(gomod); m != nil {
		return m[1]
	}
	return ""
}

// goProfile is a parsed `go tool covdata textfmt` profile: per repository-relative file, the hit
// count of each block ("startLine.startCol,endLine.endCol").
type goProfile struct {
	mode   string
	blocks map[string]map[string]int
}

func parseGoProfile(module, text string) (goProfile, []string) {
	p := goProfile{blocks: map[string]map[string]int{}}
	var problems []string
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		switch m := profileBlock.FindStringSubmatch(line); {
		case strings.HasPrefix(line, "mode: "):
			p.mode = strings.TrimPrefix(line, "mode: ")
		case line == "":
		case m == nil:
			problems = append(problems, fmt.Sprintf("malformed coverage line %q", line))
		default:
			file := strings.TrimPrefix(m[1], module+"/")
			if p.blocks[file] == nil {
				p.blocks[file] = map[string]int{}
			}
			count, _ := strconv.Atoi(m[7])
			p.blocks[file][m[2]+"."+m[3]+","+m[4]+"."+m[5]] += count
		}
	}
	return p, problems
}

// goInventory reports, for every own non-test Go file, whether it has a function body that coverage
// must reach, and the line range of func main in package main files.
func goInventory(sources map[string]string) (measured map[string]bool, mainLines map[string][2]int, problems []string) {
	measured, mainLines = map[string]bool{}, map[string][2]int{}
	fset := token.NewFileSet()
	for _, name := range sortedKeys(sources) {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: cannot parse: %v", name, err))
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			measured[name] = true
			if file.Name.Name == "main" && fn.Name.Name == "main" && fn.Recv == nil {
				mainLines[name] = [2]int{fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line}
			}
		}
	}
	return measured, mainLines, problems
}

// CheckGoCoverage verifies 100% statement coverage by raw counters. The unit profile and, when
// withApp is set, the instrumented .app profile must be non-empty, in one coverage mode and built
// from the same sources (identical blocks for every file they share); they are merged by block. Every
// own non-test file with a function must be in the profile and no block may have a zero count.
// Without the .app run, zero blocks inside func main of package main are returned as deferred
// instead: only the real application executes them.
func CheckGoCoverage(module string, sources map[string]string, unitText, appText string, withApp bool) (problems, deferred []string) {
	measured, mainLines, problems := goInventory(sources)
	unit, bad := parseGoProfile(module, unitText)
	problems = append(problems, bad...)
	if len(unit.blocks) == 0 {
		return append(problems, "the unit coverage profile is missing or empty"), nil
	}
	merged := map[string]map[string]int{}
	for file, blocks := range unit.blocks {
		merged[file] = maps.Clone(blocks)
	}
	if withApp {
		app, bad := parseGoProfile(module, appText)
		problems = append(problems, bad...)
		switch {
		case len(app.blocks) == 0:
			return append(problems, "the .app coverage profile is missing or empty"), nil
		case app.mode != unit.mode:
			return append(problems, fmt.Sprintf("coverage modes differ: %s and %s", unit.mode, app.mode)), nil
		case app.blocks["main.go"] == nil:
			problems = append(problems, "the .app profile does not contain main.go")
		}
		for _, file := range sortedKeys(app.blocks) {
			blocks, shared := merged[file]
			if shared && !slices.Equal(sortedKeys(blocks), sortedKeys(app.blocks[file])) {
				problems = append(problems, "incompatible profiles for "+file+": the unit and .app builds were made from different sources")
				continue
			}
			if !shared {
				merged[file] = map[string]int{}
			}
			for block, count := range app.blocks[file] {
				merged[file][block] += count
			}
		}
	}
	for _, file := range sortedKeys(measured) {
		if merged[file] == nil {
			problems = append(problems, file+" has functions but is not in the coverage profile")
		}
	}
	for _, file := range sortedKeys(merged) {
		for _, block := range sortedKeys(merged[file]) {
			if merged[file][block] > 0 {
				continue
			}
			start, _ := strconv.Atoi(block[:strings.IndexByte(block, '.')])
			if lines, ok := mainLines[file]; ok && !withApp && start >= lines[0] && start <= lines[1] {
				deferred = append(deferred, file+":"+block)
				continue
			}
			problems = append(problems, file+":"+block+" is not covered")
		}
	}
	return problems, deferred
}
