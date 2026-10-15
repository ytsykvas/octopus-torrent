package devtool

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

var (
	markdownLink  = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	urlScheme     = regexp.MustCompile(`^[a-z]+:`)
	headingLine   = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	claudePointer = regexp.MustCompile(`\.claude/[a-zA-Z0-9/._-]+\.md`)
)

// CheckLinks reports every relative Markdown link whose file, or whose #anchor in a Markdown file,
// does not exist. docs maps repository-relative paths to Markdown content; exists answers for any
// repository path, including directories.
func CheckLinks(docs map[string]string, exists func(string) bool) []string {
	var dead []string
	for _, file := range sortedKeys(docs) {
		for _, m := range markdownLink.FindAllStringSubmatch(docs[file], -1) {
			target := m[1]
			if urlScheme.MatchString(target) {
				continue
			}
			ref, fragment, _ := strings.Cut(target, "#")
			dest := file
			if ref != "" {
				dest = path.Clean(path.Join(path.Dir(file), ref))
			}
			if !exists(dest) {
				dead = append(dead, "DEAD "+file+": "+target)
				continue
			}
			content, isMarkdown := docs[dest]
			if fragment != "" && isMarkdown && !anchors(content)[fragment] {
				dead = append(dead, "DEAD "+file+": "+target)
			}
		}
	}
	return dead
}

// CheckPointers reports every .claude/**.md path named in sources that does not exist. Runner
// configs and agent definitions point at instruction files by path; a renamed file would make them
// read nothing, silently.
func CheckPointers(sources map[string]string, exists func(string) bool) []string {
	var dead []string
	for _, file := range sortedKeys(sources) {
		for _, pointer := range claudePointer.FindAllString(sources[file], -1) {
			if !exists(pointer) {
				dead = append(dead, "DEAD "+file+": "+pointer)
			}
		}
	}
	return dead
}

// anchors returns the GitHub-style heading anchors of a Markdown document, numbering repeats.
func anchors(markdown string) map[string]bool {
	found, seen := map[string]bool{}, map[string]int{}
	inCode := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
			continue
		}
		m := headingLine.FindStringSubmatch(line)
		if m == nil || inCode {
			continue
		}
		base := slug(m[1])
		n := seen[base]
		seen[base] = n + 1
		if n > 0 {
			base += "-" + strconv.Itoa(n)
		}
		found[base] = true
	}
	return found
}

func slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(strings.ReplaceAll(heading, "`", ""))) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
