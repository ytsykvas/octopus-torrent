package devtool

import (
	"strings"
	"testing"
)

const (
	fixtureNav = "<!-- TASKS-NAV:BEGIN -->\n" +
		"| ID / file | Task | Phase | Mandatory dependencies | Requirements | Status |\n" +
		"|---|---|---|---|---|---|\n" +
		"| [001](001-start.md) | Start the work | P0 | — | AI-01 | `done` |\n" +
		"| [002](002-build.md) | Build the shell | P1 | [001](001-start.md) | AI-02, QA-01 | `in_progress` |\n" +
		"| [003](003-check.md) | Check everything | P1 | [002](002-build.md), [001](001-start.md) | QA-01 | `planned` |\n" +
		"<!-- TASKS-NAV:END -->\n"
	fixtureMatrix = "## Full coverage of the 3 requirements\n\n" +
		"| ID | Requirement | Primary task | Check |\n|---|---|---|---|\n" +
		"| AI-01 | Start with a scope. | [001](001-start.md) | [003](003-check.md) |\n" +
		"| AI-02 | Build | reproducibly. | [002](002-build.md) | [003 — partially](003-check.md) |\n" +
		"| QA-01 | Test first. | [003](003-check.md) | [003](003-check.md) |\n\n"
	fixtureScenarios = "## Coverage of the 2 end-to-end scenarios\n\n" +
		"| Scenario | Behavior | Implementation | Acceptance checks |\n|---|---|---|---|\n" +
		"| AS-01 | First launch | [002](002-build.md) | [003](003-check.md) |\n" +
		"| AS-02 | Second launch | [002](002-build.md) | [003](003-check.md) |\n\n" +
		"## Open decisions and their responsible tasks\n\n| Question | Owner |\n|---|---|\n| AI-99 | not a requirement row |\n"
	fixtureRequirements = "# Requirements\n\n## 5. Quality\n\n| ID | Requirement | Acceptance criterion |\n|---|---|---|\n" +
		"| AI-01 | Scope. | Written. |\n| AI-02 | Build. | Reproducible. |\n| QA-01 | TDD. | RED first. |\n\n" +
		"## 6. End-to-end acceptance scenarios\n\n1. **First launch:** opens it.\n2. **Second launch:** opens it again.\n\n" +
		"## 7. Parameters\n\n1. **Not a scenario:** a numbered item in another section.\n"
)

func fixtureTask(id, title, status, phase, deps, reqs string) string {
	return "# " + id + " — " + title + "\n\n**Status:** `" + status + "`  \n**Phase:** " + phase +
		" — Some phase  \n**Priority:** high  \n**Size:** small  \n**Hard dependencies:** " + deps +
		"  \n**Requirements:** " + reqs + "\n\n## Outcome\n\nText with a [link](README.md).\n"
}

// planFixture is a small consistent plan: three tasks, three requirements, two scenarios.
func planFixture() map[string]string {
	return map[string]string{
		"product/tasks/001-start.md": fixtureTask("001", "Start the work", "done", "P0", "None — initial task.", "`AI-01`"),
		"product/tasks/002-build.md": fixtureTask("002", "Build the shell", "in_progress", "P1", "[001](001-start.md)", "`AI-02`, `QA-01`"),
		"product/tasks/003-check.md": fixtureTask("003", "Check everything", "planned", "P1", "[002](002-build.md), [001](001-start.md)", "`QA-01`"),
		"product/tasks/README.md": "# Plan\n\nIntro [requirements](../PROJECT_REQUIREMENTS.md).\n\n## Navigation of all tasks\n\n" +
			fixtureNav + "\n" + fixtureMatrix + fixtureScenarios,
		"product/PROJECT_REQUIREMENTS.md": fixtureRequirements,
	}
}

func planExists(files map[string]string) func(string) bool {
	return func(p string) bool {
		_, ok := files[p]
		return ok
	}
}

func checkPlanFixture(files map[string]string) []string {
	return CheckPlan(files, planExists(files))
}

func TestCheckPlanAcceptsAConsistentPlan(t *testing.T) {
	files := edit(t, "product/tasks/003-check.md", "Text with a [link](README.md).\n", "Text with a [link](README.md).\n\n- [ ] Open while planned.\n")
	if got := checkPlanFixture(files); len(got) != 0 {
		t.Errorf("CheckPlan = %q, want no problems", got)
	}
	if summary := PlanSummary(files); summary != "3 tasks, 3 dependencies, 3 requirements, 2 scenarios" {
		t.Errorf("PlanSummary = %q", summary)
	}
}

// edit replaces old with new in one file of a fresh fixture, failing the test if old is absent.
func edit(t *testing.T, file, old, new string) map[string]string {
	t.Helper()
	files := planFixture()
	if !strings.Contains(files[file], old) {
		t.Fatalf("%s does not contain %q", file, old)
	}
	files[file] = strings.Replace(files[file], old, new, 1)
	return files
}

func TestCheckPlanReportsEveryKindOfInconsistency(t *testing.T) {
	const readme, reqs = "product/tasks/README.md", "product/PROJECT_REQUIREMENTS.md"
	const t1, t2, t3 = "product/tasks/001-start.md", "product/tasks/002-build.md", "product/tasks/003-check.md"
	withFile := func(name, content string) map[string]string {
		files := planFixture()
		files[name] = content
		return files
	}
	cases := map[string]struct {
		files map[string]string
		want  string
	}{
		"broken link":           {edit(t, t2, "[link](README.md)", "[link](gone.md)"), "gone.md"},
		"broken nav link":       {edit(t, readme, "[requirements](../PROJECT_REQUIREMENTS.md)", "[requirements](../MISSING.md)"), "MISSING.md"},
		"cycle":                 {edit(t, t1, "None — initial task.", "[003](003-check.md)"), "cycle among 001, 002, 003"},
		"unknown dependency":    {edit(t, t3, "[001](001-start.md)", "[009](009-missing.md)"), "dependency 009 does not exist"},
		"mislabelled link":      {edit(t, t3, "[001](001-start.md)", "[001](002-build.md)"), "names task 002"},
		"unparsed dependencies": {edit(t, t1, "None — initial task.", "after 002"), "**Hard dependencies:**"},
		"unknown requirement":   {edit(t, t3, "`QA-01`", "`QA-01`, `QA-09`"), "unknown requirement QA-09"},
		"no requirements":       {edit(t, t3, "`QA-01`", "—"), "**Requirements:**"},
		"requirement in no task": {edit(t, reqs, "| QA-01 | TDD. | RED first. |",
			"| QA-01 | TDD. | RED first. |\n| QA-02 | Cover. | 100%. |"), "QA-02 is in no task"},
		"duplicate requirement": {edit(t, reqs, "| QA-01 | TDD. | RED first. |", "| QA-01 | TDD. | RED first. |\n| QA-01 | Again. | Twice. |"), "QA-01 is defined twice"},
		"missing matrix row":    {edit(t, readme, "| QA-01 | Test first. | [003](003-check.md) | [003](003-check.md) |\n", ""), "QA-01 has no row in the coverage matrix"},
		"unknown matrix row": {edit(t, readme, "| QA-01 | Test first. |",
			"| QA-07 | Extra. | [003](003-check.md) | [003](003-check.md) |\n| QA-01 | Test first. |"), "unknown requirement QA-07"},
		"primary not listing":  {edit(t, readme, "| AI-01 | Start with a scope. | [001](001-start.md)", "| AI-01 | Start with a scope. | [002](002-build.md)"), "primary task 002 does not list AI-01"},
		"primary missing":      {edit(t, readme, "| AI-01 | Start with a scope. | [001](001-start.md)", "| AI-01 | Start with a scope. | none"), "AI-01 has no primary task"},
		"matrix missing":       {edit(t, readme, "## Full coverage of the 3 requirements", "## Coverage matrix"), "no \"## Full coverage of the\" section"},
		"scenario missing":     {edit(t, readme, "| AS-02 | Second launch | [002](002-build.md) | [003](003-check.md) |\n", ""), "scenarios AS-01…AS-02, the table has 1"},
		"scenario misnumbered": {edit(t, readme, "| AS-02 |", "| AS-03 |"), "row 2 is AS-03"},
		"scenarios missing":    {edit(t, readme, "## Coverage of the 2 end-to-end scenarios", "## Scenarios"), "no \"## Coverage of the\" section"},
		"no scenario section":  {edit(t, reqs, "## 6. End-to-end acceptance scenarios", "## 6. Scenarios"), "no \"## 6. End-to-end acceptance scenarios\" section"},
		"status mismatch":      {edit(t, readme, "| QA-01 | `planned` |", "| QA-01 | `done` |"), "navigation table is out of date"},
		"stale nav":            {edit(t, t3, "Check everything", "Check every gate"), "navigation table is out of date"},
		"no nav markers":       {edit(t, readme, "<!-- TASKS-NAV:END -->", ""), "TASKS-NAV markers"},
		"unknown status":       {edit(t, t3, "`planned`", "`started`"), "unknown status started"},
		"done before deps":     {edit(t, t1, "`done`", "`planned`"), "002 is in_progress but its dependency 001 is planned"},
		"missing status":       {edit(t, t3, "**Status:** `planned`  \n", ""), "**Status:**"},
		"repeated status":      {edit(t, t3, "**Status:** `planned`  \n", "**Status:** `planned`  \n**Status:** `done`  \n"), "**Status:**"},
		"missing phase":        {edit(t, t3, "**Phase:** P1", "**Phase:** later"), "**Phase:**"},
		"wrong title":          {edit(t, t3, "# 003 — ", "# 004 — "), "first line must be \"# 003 — <title>\""},
		"duplicate id":         {withFile("product/tasks/003-again.md", fixtureTask("003", "Again", "planned", "P1", "—", "`QA-01`")), "two task files with ID 003"},
		"no requirements file": {withFile(reqs, ""), "no requirement rows"},
		"done with an open item": {edit(t, t1, "Text with a [link](README.md).\n", "Text with a [link](README.md).\n\n- [x] Met.\n- [ ] Not met yet.\n"),
			"001-start.md: is done but has an unchecked item: - [ ] Not met yet."},
	}
	for name, c := range cases {
		got := strings.Join(checkPlanFixture(c.files), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: CheckPlan =\n%s\nwant a problem containing %q", name, got, c.want)
		}
	}
}

func TestPlanNavigationIsRegeneratedFromTheTaskFiles(t *testing.T) {
	files := planFixture()
	files["product/tasks/003-check.md"] = strings.Replace(files["product/tasks/003-check.md"], "`planned`", "`blocked`", 1)
	files["product/tasks/README.md"] = strings.Replace(files["product/tasks/README.md"], "Check everything", "Old title", 1)
	got, problems := RenderNavigation(files)
	if len(problems) != 0 {
		t.Fatalf("RenderNavigation problems: %q", problems)
	}
	want := strings.Replace(planFixture()["product/tasks/README.md"], "`planned` |", "`blocked` |", 1)
	if got != want {
		t.Errorf("RenderNavigation =\n%s\nwant\n%s", got, want)
	}
	files["product/tasks/README.md"] = got
	if again, _ := RenderNavigation(files); again != got {
		t.Error("RenderNavigation is not idempotent")
	}
}

func TestPlanNavigationRefusesAnUnreadablePlan(t *testing.T) {
	files := edit(t, "product/tasks/README.md", "<!-- TASKS-NAV:BEGIN -->", "")
	if _, problems := RenderNavigation(files); len(problems) == 0 {
		t.Error("RenderNavigation accepted a README without markers")
	}
	files = edit(t, "product/tasks/003-check.md", "**Status:** `planned`  \n", "")
	if _, problems := RenderNavigation(files); len(problems) == 0 {
		t.Error("RenderNavigation accepted a task without a status")
	}
}
