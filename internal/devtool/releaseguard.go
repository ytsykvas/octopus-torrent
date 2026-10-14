// Package devtool holds the repository's own development tooling: the agent hook that keeps the
// dependency release cooldown, and the workspace commands that parallel-agent runners call.
package devtool

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	minReleaseAgeDays = 14
	policyDoc         = "docs/decisions/toolchain-versions.md"
)

// HookEvent is the PreToolUse payload Claude Code and Codex send on stdin.
type HookEvent struct {
	Cwd       string    `json:"cwd"`
	ToolName  string    `json:"tool_name"`
	ToolInput ToolInput `json:"tool_input"`
}

// ToolInput carries the fields of the Bash, Edit, MultiEdit and Write tools that the guard inspects.
type ToolInput struct {
	Command    string     `json:"command"`
	FilePath   string     `json:"file_path"`
	Content    string     `json:"content"`
	OldString  string     `json:"old_string"`
	NewString  string     `json:"new_string"`
	ReplaceAll bool       `json:"replace_all"`
	Edits      []FileEdit `json:"edits"`
}

// FileEdit is one replacement of the MultiEdit tool.
type FileEdit struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

// Decision is the guard's verdict. Deny blocks the call; Ask hands it to the owner for approval.
// Reason is shown with either.
type Decision struct {
	Deny   bool
	Ask    bool
	Reason string
}

func deny(reason string) Decision {
	return Decision{Deny: true, Reason: reason + " Policy: " + policyDoc + "."}
}

func ask(reason string) Decision {
	return Decision{Ask: true, Reason: reason + " Policy: " + policyDoc + "."}
}

// Decide reports whether a tool call would weaken the 14-day release cooldown or package integrity.
// readFile reads the current state of files the call depends on (.npmrc, package.json).
//
// The guard is a safety net against the usual and the expedient ways around the policy, including
// the ones npm itself suggests; it is not a sandbox. The authoritative check is the age of every
// locked version (task 004).
func Decide(ev HookEvent, readFile func(string) ([]byte, error)) Decision {
	switch ev.ToolName {
	case "Bash":
		return decideCommand(ev.ToolInput.Command, ev.Cwd, readFile)
	case "Edit", "MultiEdit", "Write":
		return decideFileChange(ev.ToolInput, readFile)
	}
	return Decision{}
}

var (
	assignment   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*=`)
	fullVersion  = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)
	registrySpec = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*(@[A-Za-z0-9._~^<>=*+ -]+)?$`)
)

// wrappers run the command that follows their own flags.
var wrappers = map[string]bool{
	"env": true, "sudo": true, "time": true, "nice": true, "nohup": true, "command": true,
	"builtin": true, "exec": true, "xargs": true,
}

// fileWriters are programs whose file arguments may be written, moved or removed.
var fileWriters = map[string]bool{
	"tee": true, "rm": true, "mv": true, "cp": true, "ln": true, "truncate": true, "chmod": true,
	"install": true, "rsync": true, "dd": true,
}

// npmInstalling lists npm subcommands that resolve, download or run package versions.
var npmInstalling = map[string]bool{
	"install": true, "i": true, "in": true, "isnt": true, "add": true,
	"install-test": true, "it": true, "install-ci-test": true, "cit": true,
	"ci": true, "clean-install": true, "ic": true, "install-clean": true,
	"update": true, "up": true, "upgrade": true, "dedupe": true, "ddp": true,
	"uninstall": true, "remove": true, "un": true, "link": true, "prune": true,
	"exec": true, "x": true, "create": true, "init": true,
}

// npmAdding are the installing subcommands whose arguments name packages to fetch.
var npmAdding = map[string]bool{
	"install": true, "i": true, "in": true, "isnt": true, "add": true, "install-test": true, "it": true,
	"update": true, "up": true, "upgrade": true,
}

// npmOther lists the other npm subcommands, so a flag value is never mistaken for the subcommand.
var npmOther = map[string]bool{
	"run": true, "run-script": true, "test": true, "t": true, "start": true, "stop": true, "restart": true,
	"view": true, "v": true, "info": true, "show": true, "audit": true, "config": true, "c": true,
	"outdated": true, "ls": true, "list": true, "ll": true, "la": true, "explain": true, "why": true,
	"query": true, "pack": true, "publish": true, "version": true, "help": true, "doctor": true,
	"cache": true, "prefix": true, "root": true, "search": true, "fund": true, "ping": true,
	"whoami": true, "sbom": true, "pkg": true, "rebuild": true, "diff": true, "docs": true, "repo": true,
}

// npmValueFlags take a separate value word.
var npmValueFlags = map[string]bool{
	"--loglevel": true, "--workspace": true, "-w": true, "--cache": true,
	"--tag": true, "--omit": true, "--include": true, "--install-strategy": true,
}

// npmConfigSources would make npm read settings from somewhere other than the project .npmrc, or
// read package times from somewhere other than the public registry.
var npmConfigSources = map[string]bool{"--userconfig": true, "--globalconfig": true, "--registry": true}

// cooldownKeys are the npm settings that decide whether the cooldown applies.
var cooldownKeys = map[string]bool{"min-release-age": true, "before": true, "min-release-age-exclude": true}

// otherManagers are package managers this repository does not use; they read no .npmrc cooldown.
var otherManagers = map[string]bool{"pnpm": true, "yarn": true, "bun": true, "bunx": true}

func decideCommand(command, cwd string, readFile func(string) ([]byte, error)) Decision {
	command = stripHeredocBodies(command)
	for _, inner := range substitutions(command) {
		if d := decideCommand(inner, cwd, readFile); d.Deny {
			return d
		}
	}
	dir := cwd
	for _, words := range shellSegments(command) {
		if redirectsIntoProtected(words) {
			return denyProtectedShellWrite()
		}
		program, d := unwrap(words)
		if d.Deny {
			return d
		}
		if len(program) == 0 {
			continue
		}
		name := filepath.Base(program[0])
		if writesProtectedArgument(name, program[1:]) {
			return denyProtectedShellWrite()
		}
		switch {
		case (name == "cd" || name == "pushd") && len(program) > 1:
			dir = resolve(dir, program[1])
		case strings.HasPrefix(program[0], "$"):
			d = deny("A command name taken from a variable (" + program[0] + ") cannot be checked; write the program name.")
		case shells[name]:
			d = decideShell(program[1:], dir, readFile)
		case name == "eval":
			d = decideCommand(strings.Join(program[1:], " "), dir, readFile)
		case name == "export":
			d = decideAssignments(program[1:])
		case name == "npx":
			d = decideNpx(program[1:])
		case name == "npm":
			d = decideNpm(program[1:], dir, readFile)
		case name == "go":
			d = decideGo(program[1:])
		case otherManagers[name]:
			d = deny(name + " is not used in this repository and ignores frontend/.npmrc; use npm --prefix frontend.")
		}
		if d.Deny {
			return d
		}
	}
	return Decision{}
}

func denyProtectedShellWrite() Decision {
	return deny("Shell writes to .npmrc or to the release-guard hook, its configuration or module files are blocked; change them with the Edit tool, which checks the result and asks the owner.")
}

// shellSegments splits a command line into the words of each simple command. Quotes group words
// and are removed; unquoted ;, &, |, parentheses and newlines end a command, except in a redirection
// such as >| or 2>&1. It is deliberately small: enough to see programs and their arguments, not a
// shell parser.
func shellSegments(command string) [][]string {
	var segments [][]string
	var words []string
	var word strings.Builder
	inWord, quote := false, rune(0)
	endWord := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	endSegment := func() {
		endWord()
		if len(words) > 0 {
			segments = append(segments, words)
			words = nil
		}
	}
	for _, r := range command {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case (r == '|' || r == '&') && strings.HasSuffix(word.String(), ">"):
			word.WriteRune(r)
		case strings.ContainsRune(";&|()\n", r):
			endSegment()
		case r == ' ' || r == '\t':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	endSegment()
	return segments
}

// isProtected reports a file only the owner may change: any .npmrc, and the guard itself.
func isProtected(path string) bool {
	return strings.EqualFold(filepath.Base(path), ".npmrc") || isGuard(path)
}

// guardFiles can define, disable or rewrite the hook: agent settings that hold hooks, the guard's
// source, and the module files that make it a go tool.
var guardFiles = []string{
	".claude/settings.json", ".claude/settings.local.json", ".codex/hooks.json", ".codex/config.toml",
}

func isGuard(path string) bool {
	p := "/" + strings.TrimPrefix(strings.ToLower(filepath.ToSlash(path)), "/")
	for _, f := range guardFiles {
		if strings.HasSuffix(p, "/"+f) {
			return true
		}
	}
	base := filepath.Base(p)
	return strings.Contains(p, "/internal/devtool/") || strings.Contains(p, "/cmd/devtool/") ||
		base == "go.mod" || base == "go.sum"
}

// redirectsIntoProtected reports an output redirection (>, >>, >|, 2>) into a protected file.
func redirectsIntoProtected(words []string) bool {
	for i, w := range words {
		at := strings.LastIndex(w, ">")
		if at < 0 {
			continue
		}
		target := strings.TrimLeft(w[at+1:], "|&")
		if target == "" && i+1 < len(words) {
			target = words[i+1]
		}
		if isProtected(target) {
			return true
		}
	}
	return false
}

// writesProtectedArgument reports a program that writes, moves or removes a protected file given as
// an argument (sed and perl only with in-place editing).
func writesProtectedArgument(name string, args []string) bool {
	writer := fileWriters[name]
	if name == "sed" || name == "perl" {
		for _, arg := range args {
			if arg == "--in-place" || (strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "i")) {
				writer = true
			}
		}
	}
	if !writer {
		return false
	}
	for _, arg := range args {
		if isProtected(arg) {
			return true
		}
	}
	return false
}

// unwrap drops environment assignments (checking each) and wrapper programs with their flags, and
// returns the words of the program that actually runs.
func unwrap(words []string) ([]string, Decision) {
	for {
		for len(words) > 0 && assignment.MatchString(words[0]) {
			if d := decideAssignment(words[0]); d.Deny {
				return nil, d
			}
			words = words[1:]
		}
		if len(words) == 0 || !wrappers[filepath.Base(words[0])] {
			return words, Decision{}
		}
		words = words[1:]
		for len(words) > 0 && (strings.HasPrefix(words[0], "-") || isNumber(words[0])) {
			words = words[1:]
		}
	}
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

var shells = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true}

// decideShell checks the script a shell runs: a -c string (also inside combined flags such as -lc) is
// decided like any command line; a script file is outside the guard; a shell with neither reads its
// commands from stdin — a pipe or a here-document the guard cannot see — and is refused.
func decideShell(args []string, dir string, readFile func(string) ([]byte, error)) Decision {
	for i, arg := range args {
		switch {
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") && i+1 < len(args):
			return decideCommand(args[i+1], dir, readFile)
		case !strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "<"):
			return Decision{}
		}
	}
	return deny("A shell reading commands from stdin (a pipe or a here-document) cannot be checked; run the commands directly.")
}

var heredocStart = regexp.MustCompile(`<<(-?)\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)

// stripHeredocBodies removes the text of here-documents, which is data for the receiving program
// (a commit message, a file), not commands. A here-document fed to a shell is refused separately.
func stripHeredocBodies(command string) string {
	var out strings.Builder
	lines := strings.Split(command, "\n")
	for i := 0; i < len(lines); i++ {
		out.WriteString(lines[i])
		if i < len(lines)-1 {
			out.WriteString("\n")
		}
		for _, m := range heredocStart.FindAllStringSubmatch(lines[i], -1) {
			for i+1 < len(lines) {
				i++
				body := lines[i]
				if m[1] == "-" {
					body = strings.TrimLeft(body, "\t")
				}
				if body == m[2] {
					break
				}
			}
		}
	}
	return out.String()
}

// substitutions returns the commands inside $(...) and `...` outside single quotes, outermost first;
// nested ones are found when those are decided in turn.
func substitutions(command string) []string {
	var found []string
	inSingle := false
	for i := 0; i < len(command); i++ {
		switch c := command[i]; {
		case c == '\'':
			inSingle = !inSingle
		case inSingle:
		case c == '`':
			if end := strings.IndexByte(command[i+1:], '`'); end >= 0 {
				found = append(found, command[i+1:i+1+end])
				i += end + 1
			}
		case c == '$' && i+1 < len(command) && command[i+1] == '(':
			depth := 0
			for j := i + 1; j < len(command); j++ {
				if command[j] == '(' {
					depth++
				} else if command[j] == ')' {
					depth--
					if depth == 0 {
						found = append(found, command[i+2:j])
						i = j
						break
					}
				}
			}
		}
	}
	return found
}

func decideAssignments(words []string) Decision {
	for _, w := range words {
		if !assignment.MatchString(w) {
			continue
		}
		if d := decideAssignment(w); d.Deny {
			return d
		}
	}
	return Decision{}
}

// decideAssignment checks one NAME=value environment setting. npm reads npm_config_* names case- and
// dash-insensitively, so they are normalized first.
func decideAssignment(w string) Decision {
	name, value, _ := strings.Cut(w, "=")
	switch strings.ReplaceAll(strings.ToLower(name), "-", "_") {
	case "npm_config_min_release_age":
		return decideCooldownValue(value)
	case "npm_config_before":
		return denyBefore()
	case "npm_config_min_release_age_exclude":
		return denyExclude()
	case "npm_config_userconfig", "npm_config_globalconfig", "npm_config_registry":
		return denyConfigSource(name)
	}
	switch name {
	case "GONOSUMDB", "GONOSUMCHECK", "GOINSECURE", "GOPRIVATE":
		return denyChecksum()
	case "GOSUMDB":
		if value != "sum.golang.org" {
			return denyChecksum()
		}
	case "GOFLAGS":
		if strings.Contains(value, "-insecure") {
			return denyChecksum()
		}
		if strings.Contains(value, "-mod=mod") {
			return denyModMod()
		}
	}
	return Decision{}
}

func decideCooldownValue(value string) Decision {
	days, err := strconv.Atoi(value)
	if err != nil || days < minReleaseAgeDays {
		return deny("Overriding min-release-age below 14 days is blocked: new releases wait 14 days before use.")
	}
	return Decision{}
}

func denyBefore() Decision {
	return deny("Setting npm's before is blocked: it takes precedence over min-release-age and lifts the cooldown.")
}

func denyExclude() Decision {
	return deny("Setting min-release-age-exclude is blocked: it exempts packages from the cooldown.")
}

func denyConfigSource(name string) Decision {
	return deny(name + " is blocked: another config file or registry can switch off or fake the cooldown.")
}

func denyChecksum() Decision {
	return deny("Disabling Go module checksum verification is blocked (GOSUMDB other than sum.golang.org, GONOSUMDB, GONOSUMCHECK, GOINSECURE, GOPRIVATE, -insecure).")
}

func denyModMod() Decision {
	return deny("-mod=mod lets the go command add missing modules at their latest version; add each with go get module@vX.Y.Z first.")
}

// decideNpx allows only npx --no-install / --no before the package, which runs an installed binary
// and never downloads; --yes anywhere would override it.
func decideNpx(args []string) Decision {
	noInstall := false
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			break
		}
		switch arg {
		case "--no-install", "--no":
			noInstall = true
		case "--yes", "-y":
			noInstall = false
			return deny("npx --yes downloads an unchecked latest version; use npx --no-install or an exact devDependency.")
		}
	}
	if !noInstall {
		return deny("npx without --no-install downloads an unchecked latest version; use npx --no-install or an exact devDependency.")
	}
	return Decision{}
}

// npmArgs is an npm command line split into the parts the guard needs.
type npmArgs struct {
	prefix     string
	positional []string
	global     bool
	noInstall  bool
	yes        bool
}

func parseNpm(args []string) (npmArgs, Decision) {
	var p npmArgs
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		// separate value: the next word, unless it is another flag
		if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") &&
			(name == "--min-release-age" || name == "--prefix" || name == "-C" || name == "--location" || npmValueFlags[name]) {
			i++
			value = args[i]
		}
		switch {
		case name == "--":
			return p, Decision{}
		case name == "--min-release-age":
			if d := decideCooldownValue(value); d.Deny {
				return p, d
			}
		case name == "--before":
			return p, denyBefore()
		case name == "--min-release-age-exclude":
			return p, denyExclude()
		case npmConfigSources[name]:
			return p, denyConfigSource(name)
		case name == "--prefix" || name == "-C":
			p.prefix = value
		case name == "-g" || name == "--global" || (name == "--location" && value == "global"):
			p.global = true
		case name == "--no" || name == "--no-install":
			p.noInstall = true
		case name == "--yes" || name == "-y":
			p.yes = true
		case !strings.HasPrefix(name, "-"):
			p.positional = append(p.positional, args[i])
		}
	}
	return p, Decision{}
}

func decideNpm(args []string, dir string, readFile func(string) ([]byte, error)) Decision {
	p, d := parseNpm(args)
	if d.Deny {
		return d
	}
	sub, rest := "", []string(nil)
	for i, word := range p.positional {
		if npmInstalling[word] || npmOther[word] {
			sub, rest = word, p.positional[i+1:]
			break
		}
	}
	if (sub == "config" || sub == "c") && len(rest) > 0 && rest[0] != "get" && rest[0] != "list" && rest[0] != "ls" {
		if rest[0] == "edit" {
			return deny("npm config edit is blocked: it can change the cooldown settings unchecked.")
		}
		for _, word := range rest[1:] {
			key, _, _ := strings.Cut(word, "=")
			if cooldownKeys[key] {
				return deny("Changing " + key + " through npm config is blocked; the cooldown lives in frontend/.npmrc.")
			}
		}
	}
	installing := npmInstalling[sub] || (sub == "audit" && len(rest) > 0 && rest[0] == "fix")
	if !installing || (sub == "init" && len(rest) == 0) {
		return Decision{}
	}
	switch {
	case p.global:
		return deny("A global npm install is blocked: it bypasses the project .npmrc cooldown.")
	case sub == "create" || sub == "init":
		return deny("npm create / npm init <initializer> downloads and runs an unchecked latest package; add it as an exact devDependency instead.")
	case (sub == "exec" || sub == "x") && (!p.noInstall || p.yes):
		return deny("npm exec without --no (or with --yes) downloads an unchecked latest version; use npm --prefix frontend exec --no -- <bin>.")
	}
	if npmAdding[sub] {
		for _, spec := range rest {
			if !registrySpec.MatchString(spec) {
				return deny("npm " + sub + " " + spec + " is blocked: only registry packages pass the release-age check, not git, GitHub, URL or file sources.")
			}
		}
	}
	target := dir
	if p.prefix != "" {
		target = resolve(dir, p.prefix)
	}
	project := projectRoot(target, readFile)
	content, err := readFile(filepath.Join(project, ".npmrc"))
	if err != nil {
		return deny("npm " + sub + " in " + target + " is not covered by an .npmrc with min-release-age >= 14; run it as npm --prefix frontend " + sub + ".")
	}
	if problem := cooldownProblem(string(content)); problem != "" {
		return deny("npm " + sub + " in " + target + " is not covered: " + problem)
	}
	return Decision{}
}

// projectRoot finds the directory npm treats as the project: the nearest one with a package.json,
// walking up from dir; dir itself when there is none.
func projectRoot(dir string, readFile func(string) ([]byte, error)) string {
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := readFile(filepath.Join(current, "package.json")); err == nil {
			return current
		}
		if filepath.Dir(current) == current {
			return dir
		}
	}
}

func decideGo(args []string) Decision {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") { // go's own flags, e.g. -C dir
		if args[0] == "-C" || args[0] == "--C" {
			args = args[1:]
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return Decision{}
	}
	sub, rest := args[0], args[1:]
	for _, arg := range rest {
		if strings.TrimLeft(arg, "-") == "mod=mod" {
			return denyModMod()
		}
	}
	switch {
	case sub == "env":
		return decideAssignments(rest)
	case sub == "mod" && len(rest) > 0 && rest[0] == "tidy":
		return deny("go mod tidy adds missing modules at their latest version; add each with go get module@vX.Y.Z first.")
	case sub == "mod" && len(rest) > 0 && rest[0] == "download":
		return decideGoVersions("mod download", rest[1:])
	case sub == "get" || sub == "install" || sub == "run":
		return decideGoVersions(sub, rest)
	}
	return Decision{}
}

// decideGoVersions requires an explicit full version for every module query.
func decideGoVersions(sub string, args []string) Decision {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flag := "-" + strings.TrimLeft(arg, "-")
			if sub == "get" && (flag == "-u" || strings.HasPrefix(flag, "-u=")) {
				return deny("go get -u upgrades modules to unchecked latest versions; pin each version explicitly.")
			}
			continue
		}
		if at := strings.LastIndex(arg, "@"); at >= 0 {
			if version := arg[at+1:]; version != "none" && !fullVersion.MatchString(version) {
				return deny("go " + sub + " with @" + version + " does not name a full version; pin vX.Y.Z published at least 14 days ago.")
			}
		} else if sub == "get" && !strings.HasPrefix(arg, ".") {
			return deny("go get " + arg + " without an explicit version resolves the latest release; pin a version published at least 14 days ago.")
		}
		if sub == "run" {
			break // the rest are the program's own arguments
		}
	}
	return Decision{}
}

func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

// decideFileChange checks Edit, MultiEdit and Write calls on protected files: a hook configuration
// change goes to the owner; an .npmrc change is denied if it weakens the cooldown and otherwise also
// goes to the owner, because npm's own parser, not this one, decides what the file means.
func decideFileChange(in ToolInput, readFile func(string) ([]byte, error)) Decision {
	if isGuard(in.FilePath) {
		return ask("Changing the release-guard hook, its configuration or its module files (" + in.FilePath + ") is the owner's decision.")
	}
	if !isProtected(in.FilePath) {
		return Decision{}
	}
	result := in.Content
	edits := in.Edits
	if in.OldString != "" || in.NewString != "" {
		edits = append([]FileEdit{{OldString: in.OldString, NewString: in.NewString, ReplaceAll: in.ReplaceAll}}, edits...)
	}
	if len(edits) > 0 {
		current, err := readFile(in.FilePath)
		if err != nil {
			return deny("The guard cannot read " + in.FilePath + " to check that min-release-age stays >= 14.")
		}
		result = string(current)
		for _, e := range edits {
			count := 1
			if e.ReplaceAll {
				count = -1
			}
			result = strings.Replace(result, e.OldString, e.NewString, count)
		}
	}
	if problem := cooldownProblem(result); problem != "" {
		return deny("This .npmrc change is blocked: " + problem)
	}
	return ask("Changing an .npmrc (" + in.FilePath + ") is the owner's decision; the result keeps min-release-age >= 14.")
}

// cooldownProblem explains why an .npmrc does not enforce the cooldown, or returns "" if it does:
// only plain key=value lines, the last min-release-age at least 14 days, and neither before nor
// min-release-age-exclude set.
func cooldownProblem(npmrc string) string {
	days := -1
	for _, line := range strings.Split(npmrc, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.ContainsAny(line, "[]\"'") {
			return "sections, arrays or quotes are not allowed in an .npmrc here; npm may read them differently from the min-release-age written."
		}
		key, value, _ := strings.Cut(line, "=")
		switch strings.TrimSpace(key) {
		case "min-release-age":
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				n = -1 // not a plain number (for example an inline comment): do not trust it
			}
			days = n
		case "before", "min-release-age-exclude":
			return strings.TrimSpace(key) + " is set, which lifts the min-release-age cooldown."
		}
	}
	if days < minReleaseAgeDays {
		return "min-release-age must stay >= 14 as the effective setting."
	}
	return ""
}
