package devtool

import (
	"strings"
	"testing"
)

const handOver = "#!/bin/sh\n# Prepares a workspace.\n# The logic is Go (cmd/devtool).\nset -eu\n" +
	"go build -o build/bin/devtool ./cmd/devtool\nexec build/bin/devtool workspace setup\n"

func TestCheckScriptsAcceptsAHandOverToDevtool(t *testing.T) {
	sources := map[string]string{
		".octopus/scripts/setup.sh": handOver,
		".octopus/scripts/run.sh":   strings.Replace(handOver, "workspace setup", "workspace run", 1),
		"README.md":                 "if curl\n",
	}
	if problems := CheckScripts(sources); len(problems) != 0 {
		t.Errorf("CheckScripts = %q, want nothing", problems)
	}
}

func TestCheckScriptsRejectsLogicInShell(t *testing.T) {
	cases := map[string]struct{ script, want string }{
		"a condition":    {strings.Replace(handOver, "set -eu\n", "set -eu\nif [ -d x ]; then rm -rf x; fi\n", 1), "x.sh:5: if [ -d x ]; then rm -rf x; fi"},
		"a download":     {handOver + "curl -fsSL https://example.com/install.sh\n", "x.sh:7: curl -fsSL https://example.com/install.sh"},
		"no set -eu":     {strings.Replace(handOver, "set -eu\n", "", 1), "x.sh: missing set -eu"},
		"no exec":        {strings.Replace(handOver, "exec build/bin/devtool workspace setup\n", "", 1), "x.sh: missing exec build/bin/devtool"},
		"other binary":   {strings.Replace(handOver, "exec build/bin/devtool", "exec build/bin/other", 1), "x.sh:6: exec build/bin/other workspace setup"},
		"exec first":     {strings.Replace(handOver, "set -eu\n", "", 1) + "set -eu\n", "x.sh: missing set -eu as the first command"},
		"after exec":     {handOver + "go build -o build/bin/devtool ./cmd/devtool\n", "x.sh: missing exec build/bin/devtool as the last command"},
		"no build":       {strings.Replace(handOver, "go build -o build/bin/devtool ./cmd/devtool\n", "", 1), "x.sh: missing go build -o build/bin/devtool ./cmd/devtool"},
		"no commands":    {"#!/bin/sh\n# nothing\n", "x.sh: missing exec build/bin/devtool as the last command"},
		"no shebang":     {strings.TrimPrefix(handOver, "#!/bin/sh\n"), "x.sh: missing #!/bin/sh"},
		"other language": {strings.Replace(handOver, "#!/bin/sh", "#!/usr/bin/env python3", 1), "x.sh: missing #!/bin/sh"},
	}
	if got := strings.Join(CheckScripts(map[string]string{"tools/run": "#!/bin/bash\nrm -rf /tmp/x\n"}), "\n"); !strings.Contains(got, "tools/run:2: rm -rf /tmp/x") {
		t.Errorf("an extensionless script with a shebang was not checked: %q", got)
	}
	for name, c := range cases {
		got := strings.Join(CheckScripts(map[string]string{"x.sh": c.script}), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: CheckScripts =\n%s\nwant %q", name, got, c.want)
		}
	}
}
