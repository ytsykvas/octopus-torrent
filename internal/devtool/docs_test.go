package devtool

import (
	"strings"
	"testing"
)

func existsIn(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, known := range paths {
			if known == p || strings.HasPrefix(known, p+"/") {
				return true
			}
		}
		return false
	}
}

func TestCheckLinksResolvesFilesDirectoriesAndAnchors(t *testing.T) {
	docs := map[string]string{
		"README.md": "# Project\n\n## Build and check commands\n\n```sh\n# not a heading\n```\n\n## Notes\n## Notes\n" +
			"[ok](docs/a.md) [anchor](docs/a.md#current-state) [self](#build-and-check-commands) " +
			"[dup](#notes-1) [dir](docs) [web](https://example.com/x) [mail](mailto:a@b) " +
			"[code](`not` ) [non-md](build/x.png#frag)\n",
		"docs/a.md": "# A\n\n## Current `state`\n### Données — été 2026\n[up](../README.md#notes) [utf](#données--été-2026)\n",
	}
	exists := existsIn("README.md", "docs/a.md", "build/x.png")
	if got := CheckLinks(docs, exists); len(got) != 0 {
		t.Errorf("CheckLinks = %q, want no dead links", got)
	}
}

func TestCheckLinksReportsMissingFilesAndAnchors(t *testing.T) {
	docs := map[string]string{
		"README.md": "# R\n[gone](docs/gone.md) [bad](docs/a.md#nowhere) [self](#missing)\n",
		"docs/a.md": "# A\n",
	}
	got := CheckLinks(docs, existsIn("README.md", "docs/a.md"))
	want := []string{
		"DEAD README.md: docs/gone.md",
		"DEAD README.md: docs/a.md#nowhere",
		"DEAD README.md: #missing",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("CheckLinks = %q, want %q", got, want)
	}
}

func TestCheckPointersFindsDeadClaudePaths(t *testing.T) {
	sources := map[string]string{
		".conductor/settings.toml":   "Detail: .claude/docs/actions/review.md and .claude/docs/actions/gone.md.",
		".octopus/instructions/x.md": "Read .claude/docs/actions/review.md",
	}
	got := CheckPointers(sources, existsIn(".claude/docs/actions/review.md"))
	if strings.Join(got, "\n") != "DEAD .conductor/settings.toml: .claude/docs/actions/gone.md" {
		t.Errorf("CheckPointers = %q", got)
	}
}
