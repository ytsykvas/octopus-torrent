package devtool

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	agentName        = regexp.MustCompile(`(?m)^name: (.+)$`)
	agentDescription = regexp.MustCompile(`(?m)^description: \|\n((?:  .*\n)+)`)
	agentEffort      = regexp.MustCompile(`(?m)^effort: (\w+)$`)
	agentImport      = regexp.MustCompile(`(?m)^- @(\.claude/\S+)`)
)

// codexEffort maps Claude Code effort levels to Codex model_reasoning_effort values.
var codexEffort = map[string]string{"low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high"}

// CodexAgentTOML renders a Claude Code agent definition (.claude/agents/*.md) as the equivalent
// Codex agent (.codex/agents/*.toml), so both tools run the same instructions. Codex does not
// expand @path imports, so they become plain paths the agent opens itself; an agent whose
// definition forbids editing tools carries that restriction in its text.
func CodexAgentTOML(markdown string) (string, error) {
	parts := strings.SplitN(markdown, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		return "", errors.New("no front matter")
	}
	front, body := parts[1], strings.TrimSpace(parts[2])+"\n"
	name := agentName.FindStringSubmatch(front)
	description := agentDescription.FindStringSubmatch(front)
	effort := agentEffort.FindStringSubmatch(front)
	switch {
	case name == nil:
		return "", errors.New("no name")
	case description == nil:
		return "", errors.New("no block description")
	case effort == nil:
		return "", errors.New("no effort")
	}
	reasoning, ok := codexEffort[effort[1]]
	if !ok {
		return "", fmt.Errorf("unknown effort %q", effort[1])
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(description[1], "\n"), "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	body = agentImport.ReplaceAllString(body, "- `$1`")
	body = strings.Replace(body, "Read both files every time", "Read both files every time (plain paths; open them yourself)", 1)
	if strings.Contains(front, "disallowedTools:") {
		body = "Do not edit files, commit, or start other agents.\n\n" + body
	}
	desc := strings.Join(lines, " ")
	if strings.Contains(desc+body, "'''") {
		return "", errors.New("contains ''' which ends a TOML literal string")
	}
	return fmt.Sprintf("name = %q\ndescription = '''%s'''\nmodel_reasoning_effort = %q\ndeveloper_instructions = '''\n%s'''\n",
		name[1], desc, reasoning, body), nil
}
