package devtool

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"
)

// The task plan: one Markdown file per task in product/tasks, the plan README with the generated
// navigation table and the manual coverage matrices, and the requirements they refer to.
const (
	tasksDir          = "product/tasks/"
	tasksReadme       = tasksDir + "README.md"
	requirementsFile  = "product/PROJECT_REQUIREMENTS.md"
	navBegin          = "<!-- TASKS-NAV:BEGIN -->\n"
	navEnd            = "<!-- TASKS-NAV:END -->"
	navHeader         = "| ID / file | Task | Phase | Mandatory dependencies | Requirements | Status |\n|---|---|---|---|---|---|\n"
	matrixHeading     = "## Full coverage of the"
	scenariosHeading  = "## Coverage of the"
	reqScenarioHeader = "## 6. End-to-end acceptance scenarios"
)

var (
	taskFile       = regexp.MustCompile(`^product/tasks/(\d{3})-[a-z0-9-]+\.md$`)
	taskTitle      = regexp.MustCompile(`\A# (\d{3}) — (.+)\n`)
	taskStatus     = regexp.MustCompile("(?m)^\\*\\*Status:\\*\\* `([^`]*)`")
	taskPhase      = regexp.MustCompile(`(?m)^\*\*Phase:\*\* (P\d+) — `)
	taskDeps       = regexp.MustCompile(`(?m)^\*\*Hard dependencies:\*\* (.*?)[ \t]*$`)
	taskReqs       = regexp.MustCompile(`(?m)^\*\*Requirements:\*\* (.*?)[ \t]*$`)
	taskLink       = regexp.MustCompile(`\[(\d{3})\]\((\d{3})-[a-z0-9-]+\.md\)`)
	reqInTask      = regexp.MustCompile("`([A-Z]+-\\d{2})`")
	reqRow         = regexp.MustCompile(`(?m)^\| ([A-Z]+-\d{2}) \|`)
	matrixRow      = regexp.MustCompile(`(?m)^\| ([A-Z]+-\d{2}) \| .+ \| ([^|]*) \| [^|]* \|$`)
	scenarioRow    = regexp.MustCompile(`(?m)^\| (AS-\d{2}) \|`)
	scenarioItem   = regexp.MustCompile(`(?m)^\d+\. \*\*`)
	openItem       = regexp.MustCompile(`(?m)^[ \t]*- \[ \] .*$`)
	taskStatuses   = []string{"planned", "in_progress", "blocked", "done"}
	firstTaskInRow = regexp.MustCompile(`\[(\d{3})[^\]]*\]\(\d{3}-[a-z0-9-]+\.md\)`)
)

type planTask struct {
	id, file, title, phase, status string
	deps, reqs                     []string
}

// parseTasks reads every task file; problems name what could not be read.
func parseTasks(files map[string]string) (map[string]planTask, []string) {
	tasks := map[string]planTask{}
	var problems []string
	for _, name := range sortedKeys(files) {
		m := taskFile.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		task, bad := parseTask(m[1], path.Base(name), files[name])
		problems = append(problems, bad...)
		if _, dup := tasks[task.id]; dup {
			problems = append(problems, fmt.Sprintf("two task files with ID %s", task.id))
			continue
		}
		tasks[task.id] = task
	}
	return tasks, problems
}

func parseTask(id, file, content string) (planTask, []string) {
	task := planTask{id: id, file: file}
	var problems []string
	bad := func(format string, args ...any) {
		problems = append(problems, file+": "+fmt.Sprintf(format, args...))
	}
	if m := taskTitle.FindStringSubmatch(content); m == nil || m[1] != id {
		bad("first line must be %q", "# "+id+" — <title>")
	} else {
		task.title = m[2]
	}
	one := func(re *regexp.Regexp, line string) string {
		all := re.FindAllStringSubmatch(content, -1)
		if len(all) != 1 {
			bad("needs exactly one %q line", line)
			return ""
		}
		return all[0][1]
	}
	task.status = one(taskStatus, "**Status:** `…`")
	if task.status != "" && !slices.Contains(taskStatuses, task.status) {
		bad("unknown status %s", task.status)
	}
	if task.status == "done" {
		for _, item := range openItem.FindAllString(content, -1) {
			bad("is done but has an unchecked item: %s", strings.TrimSpace(item))
		}
	}
	task.phase = one(taskPhase, "**Phase:** P<n> — …")
	if deps := one(taskDeps, "**Hard dependencies:** …"); deps != "" {
		links := taskLink.FindAllStringSubmatch(deps, -1)
		if len(links) == 0 && deps != "—" && !strings.HasPrefix(deps, "None") {
			bad("**Hard dependencies:** must be task links, \"—\" or \"None …\"")
		}
		for _, l := range links {
			if l[1] != l[2] {
				bad("dependency link [%s] names task %s", l[1], l[2])
			}
			task.deps = append(task.deps, l[1])
		}
	}
	if reqs := one(taskReqs, "**Requirements:** …"); reqs != "" {
		for _, r := range reqInTask.FindAllStringSubmatch(reqs, -1) {
			task.reqs = append(task.reqs, r[1])
		}
		if len(task.reqs) == 0 {
			bad("**Requirements:** names no requirement")
		}
	}
	return task, problems
}

// section returns the text from the first line starting with heading up to the next "## " heading.
func section(content, heading string) (string, bool) {
	start := strings.Index(content, "\n"+heading)
	if start < 0 {
		return "", false
	}
	rest := content[start+1:]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end+1]
	}
	return rest, true
}

const navMarkersProblem = tasksReadme + ": needs one pair of TASKS-NAV markers"

// navBounds locates the generated block: begin is the start of the begin marker, end the start of
// the end marker.
func navBounds(readme string) (begin, end int, ok bool) {
	begin, end = strings.Index(readme, navBegin), strings.Index(readme, navEnd)
	ok = begin >= 0 && end > begin && strings.Count(readme, navBegin) == 1 && strings.Count(readme, navEnd) == 1
	return begin, end, ok
}

// RenderNavigation returns the plan README with its marked navigation block regenerated from the
// task files; everything outside the markers is kept as it is.
func RenderNavigation(files map[string]string) (string, []string) {
	tasks, problems := parseTasks(files)
	readme := files[tasksReadme]
	begin, end, ok := navBounds(readme)
	if !ok {
		problems = append(problems, navMarkersProblem)
	}
	if len(problems) > 0 {
		return "", problems
	}
	var b strings.Builder
	b.WriteString(navHeader)
	for _, id := range sortedKeys(tasks) {
		t := tasks[id]
		deps := []string{}
		for _, d := range t.deps {
			deps = append(deps, "["+d+"]("+tasks[d].file+")")
		}
		dependencies := strings.Join(deps, ", ")
		if dependencies == "" {
			dependencies = "—"
		}
		fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s | %s | `%s` |\n", id, t.file, t.title, t.phase, dependencies, strings.Join(t.reqs, ", "), t.status)
	}
	return readme[:begin+len(navBegin)] + b.String() + readme[end:], nil
}

// PlanSummary counts what the plan contains, for the success line.
func PlanSummary(files map[string]string) string {
	tasks, _ := parseTasks(files)
	deps := 0
	for _, t := range tasks {
		deps += len(t.deps)
	}
	reqs := len(reqRow.FindAllString(files[requirementsFile], -1))
	scenarios := 0
	if s, ok := section(files[requirementsFile], reqScenarioHeader); ok {
		scenarios = len(scenarioItem.FindAllString(s, -1))
	}
	return fmt.Sprintf("%d tasks, %d dependencies, %d requirements, %d scenarios", len(tasks), deps, reqs, scenarios)
}

// CheckPlan verifies the plan: readable task files with unique IDs, existing dependencies and links,
// an acyclic dependency graph (by topological sort, not by number), statuses that respect
// dependencies, every requirement covered by a task and by one matrix row whose primary task lists
// it, every scenario in the table, and a navigation block that matches the task files.
func CheckPlan(files map[string]string, exists func(string) bool) []string {
	tasks, problems := parseTasks(files)
	problems = append(problems, checkDependencies(tasks)...)
	problems = append(problems, checkRequirements(files, tasks)...)
	problems = append(problems, checkScenarios(files)...)
	docs := map[string]string{}
	for name, content := range files {
		if strings.HasPrefix(name, tasksDir) {
			docs[name] = content
		}
	}
	problems = append(problems, CheckLinks(docs, exists)...)
	if _, _, ok := navBounds(files[tasksReadme]); !ok {
		problems = append(problems, navMarkersProblem)
	} else if nav, bad := RenderNavigation(files); len(bad) == 0 && nav != files[tasksReadme] {
		problems = append(problems, tasksReadme+": navigation table is out of date with the task files; run go tool devtool plan-nav")
	}
	return problems
}

func checkDependencies(tasks map[string]planTask) []string {
	var problems []string
	waiting := map[string]int{}
	dependents := map[string][]string{}
	for _, id := range sortedKeys(tasks) {
		t := tasks[id]
		for _, d := range t.deps {
			dep, ok := tasks[d]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: dependency %s does not exist", t.file, d))
				continue
			}
			waiting[id]++
			dependents[d] = append(dependents[d], id)
			if (t.status == "done" || t.status == "in_progress") && dep.status != "done" {
				problems = append(problems, fmt.Sprintf("%s is %s but its dependency %s is %s", id, t.status, d, dep.status))
			}
		}
	}
	var ready []string
	for id := range tasks {
		if waiting[id] == 0 {
			ready = append(ready, id)
		}
	}
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		for _, next := range dependents[id] {
			if waiting[next]--; waiting[next] == 0 {
				ready = append(ready, next)
			}
		}
	}
	var cycle []string
	for id, n := range waiting {
		if n > 0 {
			cycle = append(cycle, id)
		}
	}
	if len(cycle) > 0 {
		slices.Sort(cycle)
		problems = append(problems, "dependency cycle among "+strings.Join(cycle, ", "))
	}
	return problems
}

func checkRequirements(files map[string]string, tasks map[string]planTask) []string {
	var problems []string
	defined := map[string]bool{}
	var order []string
	for _, m := range reqRow.FindAllStringSubmatch(files[requirementsFile], -1) {
		if defined[m[1]] {
			problems = append(problems, fmt.Sprintf("%s: requirement %s is defined twice", requirementsFile, m[1]))
			continue
		}
		defined[m[1]] = true
		order = append(order, m[1])
	}
	if len(order) == 0 {
		return append(problems, "no requirement rows in "+requirementsFile)
	}
	inTask := map[string]bool{}
	for _, id := range sortedKeys(tasks) {
		for _, r := range tasks[id].reqs {
			inTask[r] = true
			if !defined[r] {
				problems = append(problems, fmt.Sprintf("%s: unknown requirement %s", tasks[id].file, r))
			}
		}
	}
	matrix, ok := section(files[tasksReadme], matrixHeading)
	if !ok {
		return append(problems, fmt.Sprintf("%s: no %q section", tasksReadme, matrixHeading))
	}
	primary := map[string]string{}
	for _, m := range matrixRow.FindAllStringSubmatch(matrix, -1) {
		if !defined[m[1]] {
			problems = append(problems, fmt.Sprintf("%s: coverage matrix names unknown requirement %s", tasksReadme, m[1]))
			continue
		}
		owner := firstTaskInRow.FindStringSubmatch(m[2])
		if owner == nil {
			problems = append(problems, fmt.Sprintf("%s: %s has no primary task", tasksReadme, m[1]))
			primary[m[1]] = ""
			continue
		}
		primary[m[1]] = owner[1]
		if !slices.Contains(tasks[owner[1]].reqs, m[1]) {
			problems = append(problems, fmt.Sprintf("%s: primary task %s does not list %s", tasksReadme, owner[1], m[1]))
		}
	}
	for _, r := range order {
		if !inTask[r] {
			problems = append(problems, fmt.Sprintf("requirement %s is in no task", r))
		}
		if _, ok := primary[r]; !ok {
			problems = append(problems, fmt.Sprintf("%s: %s has no row in the coverage matrix", tasksReadme, r))
		}
	}
	return problems
}

func checkScenarios(files map[string]string) []string {
	defined, ok := section(files[requirementsFile], reqScenarioHeader)
	if !ok {
		return []string{fmt.Sprintf("%s: no %q section", requirementsFile, reqScenarioHeader)}
	}
	table, ok := section(files[tasksReadme], scenariosHeading)
	if !ok {
		return []string{fmt.Sprintf("%s: no %q section", tasksReadme, scenariosHeading)}
	}
	want := len(scenarioItem.FindAllString(defined, -1))
	rows := scenarioRow.FindAllStringSubmatch(table, -1)
	if len(rows) != want {
		return []string{fmt.Sprintf("%s: the requirements define scenarios AS-01…AS-%02d, the table has %d", tasksReadme, want, len(rows))}
	}
	for i, r := range rows {
		if r[1] != fmt.Sprintf("AS-%02d", i+1) {
			return []string{fmt.Sprintf("%s: scenario table row %d is %s, want AS-%02d", tasksReadme, i+1, r[1], i+1)}
		}
	}
	return nil
}

// planFiles reads the task files, the plan README and the requirements.
func planFiles(sys System) (map[string]string, func(string) bool, error) {
	files, err := repositoryFiles(sys)
	if err != nil {
		return nil, nil, err
	}
	plan := map[string]string{}
	for _, f := range files {
		if strings.HasPrefix(f, tasksDir) || f == requirementsFile {
			if content, err := sys.ReadFile(f); err == nil {
				plan[f] = string(content)
			}
		}
	}
	return plan, listedPath(files), nil
}

func planProblems(problems []string) []string {
	out := make([]string, len(problems))
	for i, p := range problems {
		out[i] = "PLAN " + p
	}
	return out
}

// checkPlan verifies the task plan and that its navigation table matches the task files.
func checkPlan(stdout io.Writer, sys System) error {
	summary, problems, err := planCheck(sys)
	return printOwn(stdout, summary, problems, err, "plan problems")
}

// planCheck returns the plan problems, and the summary printed when there are none.
func planCheck(sys System) (string, []string, error) {
	files, exists, err := planFiles(sys)
	if err != nil {
		return "", nil, err
	}
	if problems := CheckPlan(files, exists); len(problems) > 0 {
		return "", planProblems(problems), nil
	}
	return "plan: " + PlanSummary(files) + "; navigation up to date", nil, nil
}

// planNav regenerates the navigation block of the plan README from the task files.
func planNav(stdout io.Writer, sys System) error {
	files, _, err := planFiles(sys)
	if err != nil {
		return err
	}
	readme, problems := RenderNavigation(files)
	if len(problems) > 0 {
		return printOwn(stdout, "", planProblems(problems), nil, "plan problems")
	}
	if readme == files[tasksReadme] {
		fmt.Fprintln(stdout, "plan-nav: navigation already up to date")
		return nil
	}
	if err := sys.WriteFile(tasksReadme, []byte(readme)); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "plan-nav: navigation regenerated")
	return nil
}
