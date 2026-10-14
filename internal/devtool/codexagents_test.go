package devtool

import (
	"strings"
	"testing"
)

const reviewerAgent = `---
name: code-reviewer
description: |
  Reviews changes for architecture
  and quality. Read-only.
model: sonnet
effort: xhigh
tools:
  - Read
---

You review code.

- @.claude/docs/actions/review.md — what blocks a merge

Read both files every time — they are the source of truth.
`

func TestCodexAgentMirrorsAClaudeAgent(t *testing.T) {
	got, err := CodexAgentTOML(reviewerAgent)
	if err != nil {
		t.Fatal(err)
	}
	want := `name = "code-reviewer"
description = '''Reviews changes for architecture and quality. Read-only.'''
model_reasoning_effort = "high"
developer_instructions = '''
You review code.

- ` + "`.claude/docs/actions/review.md`" + ` — what blocks a merge

Read both files every time (plain paths; open them yourself) — they are the source of truth.
'''
`
	if got != want {
		t.Errorf("CodexAgentTOML =\n%s\nwant\n%s", got, want)
	}
}

func TestCodexAgentKeepsReadOnlyAgentsFromEditing(t *testing.T) {
	agent := strings.Replace(reviewerAgent, "tools:\n  - Read\n", "disallowedTools:\n  - Write\n", 1)
	got, err := CodexAgentTOML(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "developer_instructions = '''\nDo not edit files, commit, or start other agents.\n\nYou review code.") {
		t.Errorf("an agent with disallowedTools must carry the restriction in its Codex text:\n%s", got)
	}
}

func TestCodexAgentRejectsWhatItCannotMirror(t *testing.T) {
	for name, agent := range map[string]string{
		"no front matter":   "You review code.\n",
		"no name":           strings.Replace(reviewerAgent, "name: code-reviewer\n", "", 1),
		"no description":    strings.Replace(reviewerAgent, "description: |\n  Reviews changes for architecture\n  and quality. Read-only.\n", "", 1),
		"no effort":         strings.Replace(reviewerAgent, "effort: xhigh\n", "", 1),
		"unknown effort":    strings.Replace(reviewerAgent, "effort: xhigh", "effort: huge", 1),
		"breaks the string": reviewerAgent + "'''\n",
	} {
		if _, err := CodexAgentTOML(agent); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
