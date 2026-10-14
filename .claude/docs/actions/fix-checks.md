# Action: fix failing checks

Read the failing output before changing anything: it names a symptom, the test says what was
expected.

## The suite

The checks and their order are in [`/check`](../../commands/check.md). Run the ones relevant to what
you touched, and never stop at the first failure: a later check often explains an earlier one.

## Fixing

- Formatting first: `gofmt -w <files>`.
- Fix the narrowest set of files and the root cause. Re-run the failing check, then the full suite.
- Never weaken the gate to get a green run: no lowered coverage thresholds, no narrowed `include` in
  `frontend/vite.config.ts`, no ignore directives for own logic, no skipped, disabled or deleted
  tests, no hand edits in `frontend/wailsjs/`. If a test itself is wrong, say so and fix it openly.
- A coverage gap means missing behavior tests; write the test (RED if it is new behavior), not a
  test that merely executes the line.

## Traps that are not code problems

- **`go test` fails to compile `main` with a missing `frontend/dist`.** `main.go` embeds the built
  frontend: run `npm --prefix frontend run build` first.
- **Coverage profile shows `main.go` `main()` uncovered.** It runs only inside the real `.app`; the
  merged-profile procedure in the [README](../../../README.md) launches the instrumented app.
- **The `release-guard` hook refused a command.** It is not a bug to work around: the command would
  weaken the 14-day release cooldown or checksum verification. Use the exact-version, `--prefix
  frontend` or `--no-install` form it names, or ask the owner.
- **The hook asks the owner** (Claude Code shows a permission prompt) for any change to an `.npmrc`,
  the agent settings that hold hooks (`.claude/settings*.json`, `.codex/hooks.json`,
  `.codex/config.toml`), the guard's source (`internal/devtool/`, `cmd/devtool/`) or `go.mod` /
  `go.sum`. Wait for the answer; never route the change around the Edit tool. In a permission mode
  that would approve the prompt by itself (`auto`, `acceptEdits`, `bypassPermissions`) the hook denies
  these changes instead: say so and ask the owner to switch to the `default` mode.
- **`go test` fails with "`.codex/agents/<name>.toml` is out of date".** An agent in `.claude/agents/`
  changed: run `go tool devtool codex-agents` and commit the result.
- **`npm ci` refuses a version.** `min-release-age=14` holds back a release younger than 14 days. Pick
  the newest version that is old enough; never lower the setting.
- **`frontend/wailsjs/runtime/*` shows as modified with no content diff.** A file-mode flip:
  `go tool wails generate module` writes 755, `go tool wails build` writes 644. Restore with
  `git checkout -- frontend/wailsjs/runtime`; it is not a binding change.
- **Linker warning about macOS 13.0 vs 11.0.** Known and harmless
  ([toolchain-versions](../../../docs/decisions/toolchain-versions.md)).
