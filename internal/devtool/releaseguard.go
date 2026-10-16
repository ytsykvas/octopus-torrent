// Package devtool holds the repository's own development tooling: the agent hook that keeps the
// dependency release cooldown, and the workspace commands that parallel-agent runners call.
package devtool

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	minReleaseAgeDays = 14
	policyDoc         = "docs/decisions/toolchain-versions.md"
)

// HookEvent is the PreToolUse payload Claude Code and Codex send on stdin.
type HookEvent struct {
	Cwd            string    `json:"cwd"`
	PermissionMode string    `json:"permission_mode"`
	ToolName       string    `json:"tool_name"`
	ToolInput      ToolInput `json:"tool_input"`
}

// ownerPromptingModes are the permission modes in which an "ask" reaches the owner. In the others
// (auto, acceptEdits, bypassPermissions, …) Claude Code settles it without them; an empty mode is an
// agent that does not report one.
var ownerPromptingModes = map[string]bool{"default": true, "plan": true, "": true}

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
// the ones npm itself suggests; it is not a sandbox. The results are checked by the quality gate
// (task 004): the registry age of every locked version and npm's and Go's effective settings.
func Decide(ev HookEvent, readFile func(string) ([]byte, error)) Decision {
	var d Decision
	switch ev.ToolName {
	case "Bash":
		d = decideCommand(ev.ToolInput.Command, ev.Cwd, readFile)
	case "Edit", "MultiEdit", "Write":
		d = decideFileChange(ev.ToolInput, readFile)
	}
	if d.Ask && !ownerPromptingModes[ev.PermissionMode] {
		return Decision{Deny: true, Reason: d.Reason + " The current permission mode (" + ev.PermissionMode +
			") would approve this without the owner, so it is blocked; the owner switches to the default mode to approve it."}
	}
	return d
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
	command, bodies := stripHeredocBodies(command)
	for _, inner := range substitutions(command) {
		if d := decideCommand(inner, cwd, readFile); d.Deny {
			return d
		}
	}
	segments := shellSegments(command)
	dir := cwd
	for _, words := range segments {
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
		case (name == "source" || name == ".") && len(program) > 1 && readsStdin(program[1]):
			d = denyStdinShell()
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
	toSinks := len(bodies) > 0 && heredocsGoToDataSinks(command, len(bodies))
	// A body is expanded where its command runs: in the starting directory or after a cd.
	dirs := slices.Compact([]string{cwd, dir})
	for _, body := range bodies {
		var texts []string
		if !body.quoted {
			texts = bodySubstitutions(body.text)
		}
		if !toSinks {
			texts = append(texts, body.text)
		}
		for _, text := range texts {
			for _, where := range dirs {
				if d := decideCommand(text, where, readFile); d.Deny {
					return d
				}
			}
		}
	}
	return Decision{}
}

func denyProtectedShellWrite() Decision {
	return deny("Shell writes to .npmrc or to the release-guard hook, its configuration or module files are blocked; change them with the Edit tool, which checks the result and asks the owner.")
}

// shellSegments splits a command line into the words of each simple command. Quotes group words
// and are removed, a backslash escapes the next character as the shell does; unquoted ;, &, |,
// parentheses and newlines end a command, except in a redirection such as >| or 2>&1. It is
// deliberately small: enough to see programs and their arguments, not a shell parser.
func shellSegments(command string) [][]string {
	segments, _ := shellPipeline(command)
	return segments
}

// shellPipeline is shellSegments that also reports, for each segment, whether it reads the output of
// the one before it through a pipe (| or |&).
func shellPipeline(command string) (segments [][]string, piped []bool) {
	var words []string
	var word strings.Builder
	inWord, quote, escaped := false, rune(0), false
	pipe, prev := false, rune(0)
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
			segments, piped = append(segments, words), append(piped, pipe)
			words, pipe = nil, false
		}
	}
	for _, r := range command {
		switch {
		case escaped:
			if quote == '"' && !strings.ContainsRune("$`\"\\\n", r) {
				word.WriteRune('\\')
			}
			if r != '\n' {
				word.WriteRune(r)
				inWord = true
			}
			escaped = false
		case quote == '\'' && r == '\'':
			quote = 0
		case quote == '\'':
			word.WriteRune(r)
		case r == '\\':
			escaped = true
		case quote == '"' && r == '"':
			quote = 0
		case quote == '"':
			word.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case (r == '|' || r == '&') && strings.HasSuffix(word.String(), ">"):
			word.WriteRune(r)
		case strings.ContainsRune(";&|()\n", r):
			endSegment()
			// | starts a pipe and |& keeps it; the second | of || ends it.
			pipe = (r == '|' && prev != '|') || (r == '&' && prev == '|')
		case r == ' ' || r == '\t':
			endWord()
		default:
			word.WriteRune(r)
			inWord = true
		}
		prev = r
	}
	endSegment()
	return segments, piped
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

// shellValueOptions take the next argument as their value.
var shellValueOptions = map[string]bool{"-o": true, "+o": true, "-O": true, "+O": true, "--rcfile": true, "--init-file": true}

var redirectionWord = regexp.MustCompile(`^[0-9]*[<>]`)

// decideShell checks the script a shell runs, reading the arguments the way the shell does: options
// (and their values) come first; with -c — also inside combined flags such as -lc — the first operand
// is the command line, decided like any other; otherwise the first operand is a script file, outside
// the guard unless it is stdin in disguise. A shell with no operand reads its commands from stdin — a
// pipe or a here-document the guard cannot see — and is refused.
func decideShell(args []string, dir string, readFile func(string) ([]byte, error)) Decision {
	withCommand, fromStdin := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			if i+1 < len(args) {
				return decideShellOperand(args[i+1], withCommand, fromStdin, dir, readFile)
			}
			i = len(args)
		case shellValueOptions[arg]:
			i++
		case strings.HasPrefix(arg, "--") || redirectionWord.MatchString(arg):
		case len(arg) > 1 && (arg[0] == '-' || arg[0] == '+'):
			withCommand = withCommand || (arg[0] == '-' && strings.Contains(arg, "c"))
			fromStdin = fromStdin || (arg[0] == '-' && strings.ContainsAny(arg, "si")) // -s, -i: commands from stdin
		default:
			return decideShellOperand(arg, withCommand, fromStdin, dir, readFile)
		}
	}
	if withCommand {
		return Decision{} // -c without a command line: the shell itself fails
	}
	return denyStdinShell()
}

func decideShellOperand(operand string, withCommand, fromStdin bool, dir string, readFile func(string) ([]byte, error)) Decision {
	switch {
	case withCommand:
		return decideCommand(operand, dir, readFile)
	case fromStdin || readsStdin(operand):
		return denyStdinShell()
	}
	return Decision{}
}

// readsStdin reports a script path that is really the shell's standard input.
func readsStdin(path string) bool {
	return path == "-" || path == "/dev/stdin" || strings.HasPrefix(path, "/dev/fd/") || strings.HasPrefix(path, "/proc/self/fd/")
}

func denyStdinShell() Decision {
	return deny("A shell reading commands from stdin (a pipe, a here-document or /dev/stdin) cannot be checked; run the commands directly.")
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// joinArgv renders an argument list as one command line, quoting each argument so it stays one word.
func joinArgv(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		if safeWord.MatchString(a) {
			quoted[i] = a
		} else {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

var (
	heredocOperator = regexp.MustCompile(`^<<(-?)[ \t]*(?:'([A-Za-z_]\w*)'|"([A-Za-z_]\w*)"|([A-Za-z_]\w*))`)
	// heredocEnd is what may follow the delimiter word; anything else means a word this parser does
	// not fully understand (E"O"F, \EOF), which is then not treated as a here-document at all.
	heredocEnd = regexp.MustCompile(`^($|[ \t\n;&|<>()])`)
)

// dataSinks receive a here-document as data, never as commands. git and gh only through these
// built-in commands, which aliases and extensions cannot shadow, and git without -c, which could
// define an alias or a hook command.
var (
	dataSinks       = map[string]bool{"cat": true, "tee": true}
	gitDataCommands = map[string]bool{"commit": true, "tag": true, "notes": true, "apply": true, "hash-object": true}
	ghDataCommands  = map[string]bool{"pr": true, "issue": true, "release": true, "api": true, "gist": true}
)

// isDataSink reports whether a program reads its standard input as data.
func isDataSink(program []string) bool {
	if len(program) == 0 {
		return false
	}
	switch filepath.Base(program[0]) {
	case "git":
		for i := 1; i < len(program); i++ {
			switch w := program[i]; {
			case w == "-C":
				i++
			case w == "-c" || strings.HasPrefix(w, "--config-env") || strings.HasPrefix(w, "--exec-path"):
				return false
			case !strings.HasPrefix(w, "-"):
				return gitDataCommands[w]
			}
		}
		return false
	case "gh":
		return len(program) > 1 && ghDataCommands[program[1]]
	}
	return dataSinks[filepath.Base(program[0])]
}

// heredocBody is the text of one here-document. The shell expands $(...) and `...` in the body
// unless the delimiter was quoted ('EOF' or "EOF"), whatever program reads it.
type heredocBody struct {
	text   string
	quoted bool
}

// stripHeredocBodies separates the text of here-documents from the command line. Only an unquoted,
// unescaped << whose delimiter word is fully understood, and whose terminator line exists, counts;
// anything else stays in the command line and is decided as commands (fail closed). The bodies are
// returned so the caller can decide their substitutions, and the bodies themselves unless every
// here-document goes to a data sink.
func stripHeredocBodies(command string) (string, []heredocBody) {
	var out strings.Builder
	var bodies []heredocBody
	type heredoc struct {
		word   string
		tabs   bool
		quoted bool
	}
	var pending []heredoc
	var quote byte
	escaped := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case escaped:
			escaped = false
		case quote != 0:
			if c == quote {
				quote = 0
			} else if c == '\\' && quote == '"' {
				escaped = true
			}
		case c == '\\':
			escaped = true
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || strings.IndexByte(" \t\n;&|()", command[i-1]) >= 0):
			// A comment: a << in it is not a here-document, so the next lines stay commands.
			end := strings.IndexByte(command[i:], '\n')
			if end < 0 {
				end = len(command) - i
			}
			out.WriteString(command[i : i+end])
			i += end - 1
			continue
		case strings.HasPrefix(command[i:], "<<<"):
			out.WriteString("<<<")
			i += 2
			continue
		case heredocOperator.MatchString(command[i:]):
			m := heredocOperator.FindStringSubmatch(command[i:])
			if heredocEnd.MatchString(command[i+len(m[0]):]) {
				pending = append(pending, heredoc{word: m[2] + m[3] + m[4], tabs: m[1] == "-", quoted: m[4] == ""})
			}
			out.WriteString(m[0])
			i += len(m[0]) - 1
			continue
		case c == '\n' && len(pending) > 0:
			pos := i + 1
			var found []heredocBody
			for _, h := range pending {
				start, closed := pos, false
				for pos < len(command) && !closed {
					line, next := command[pos:], len(command)
					if end := strings.IndexByte(line, '\n'); end >= 0 {
						line, next = line[:end], pos+end+1
					}
					body := command[start:pos]
					pos = next
					if h.tabs {
						line = strings.TrimLeft(line, "\t")
					}
					if line == h.word {
						found, closed = append(found, heredocBody{text: body, quoted: h.quoted}), true
					}
				}
				if !closed {
					out.WriteString(command[i:]) // no terminator: not a here-document after all
					return out.String(), bodies
				}
			}
			bodies = append(bodies, found...)
			pending = nil
			out.WriteByte('\n')
			i = pos - 1
			continue
		}
		out.WriteByte(c)
	}
	return out.String(), bodies
}

// heredocsGoToDataSinks reports whether each of the n here-documents in the command line (bodies
// removed) feeds a data sink: as many segments carry a << word as there are bodies, each of those is
// a data sink, and so is every program reading a pipe, which a sink's output could feed.
func heredocsGoToDataSinks(command string, n int) bool {
	segments, piped := shellPipeline(command)
	count := 0
	for i, words := range segments {
		has := false
		for _, w := range words {
			has = has || (strings.HasPrefix(w, "<<") && !strings.HasPrefix(w, "<<<"))
		}
		if !has && !piped[i] {
			continue
		}
		if has {
			count++
		}
		if program, _ := unwrap(words); !isDataSink(program) {
			return false
		}
	}
	return count == n
}

// quoteActive reports, for each byte, whether the shell would expand $(...) and `...` there: outside
// single quotes and not escaped.
func quoteActive(command string) []bool {
	active := make([]bool, len(command))
	var quote byte
	escaped := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case escaped:
			escaped = false
		case quote == '\'':
			if c == '\'' {
				quote = 0
			}
		case c == '\\':
			escaped = true
		case quote == '"':
			if c == '"' {
				quote = 0
			} else {
				active[i] = true
			}
		case c == '\'' || c == '"':
			quote = c
		default:
			active[i] = true
		}
	}
	return active
}

// escapeActive is quoteActive for the body of an unquoted here-document: quotes there are ordinary
// characters, and only a backslash stops the next character from being expanded.
func escapeActive(body string) []bool {
	active := make([]bool, len(body))
	escaped := false
	for i := 0; i < len(body); i++ {
		active[i] = !escaped
		escaped = !escaped && body[i] == '\\'
	}
	return active
}

// substitutions returns the commands inside $(...) and `...` where the shell expands them in a
// command line, outermost first; nested ones are found when those are decided in turn.
func substitutions(command string) []string {
	return expansions(command, quoteActive(command))
}

// bodySubstitutions returns the commands the shell runs while expanding an unquoted here-document.
// expansions counts parentheses and backticks without the shell's grammar (quotes, case patterns,
// nesting), so everything from the first substitution to the end of the body is returned as well: a
// body has no command line of its own that would be decided as a whole.
func bodySubstitutions(body string) []string {
	active := escapeActive(body)
	found := expansions(body, active)
	for i := 0; i < len(body); i++ {
		if active[i] && (body[i] == '`' || strings.HasPrefix(body[i:], "$(")) {
			return append(found, strings.TrimLeft(body[i:], "$(`"))
		}
	}
	return found
}

// expansions returns the commands inside $(...) and `...` that start where active is true.
// $((...)) is arithmetic, not a command.
func expansions(command string, active []bool) []string {
	var found []string
	for i := 0; i < len(command); i++ {
		if !active[i] {
			continue
		}
		switch c := command[i]; {
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
