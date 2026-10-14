Check whether the agent instructions (`AGENTS.md`, `CLAUDE.md`, `.claude/`, `.codex/`, `.octopus/`, `.conductor/`) still match the project after a change, and update them after confirmation.

## Step 1: Detect changes to the sources the instructions mirror
```bash
git diff main --name-only -- AGENTS.md docs/engineering/ product/APPLICATION_ARCHITECTURE.md \
  product/PROJECT_REQUIREMENTS.md docs/decisions/ README.md frontend/package.json frontend/vite.config.ts \
  frontend/.npmrc go.mod main.go internal/ cmd/ .claude/ .codex/ .octopus/ .conductor/
git status --short
```
If none of these changed, stop: no action needed.

## Step 2: Map each change to the instructions it affects

| Changed source | Instructions to re-check |
|---|---|
| `AGENTS.md`, `docs/engineering/TDD_AND_QUALITY.md`, `product/APPLICATION_ARCHITECTURE.md` | `.claude/docs/actions/review.md` and `review-style.md` (they mirror these rules on purpose — the one accepted duplication, so the one that silently rots) |
| Files, paths, deletion, privacy, Wails facade rules; `docs/decisions/toolchain-versions.md` | `.claude/docs/actions/review-security.md`, `fix-checks.md`, `internal/devtool/releaseguard.go` (the hook enforces the dependency policy — change it test-first) |
| README commands, `frontend/vite.config.ts`, new check tooling (task 004) | `.claude/commands/check.md`, `fix-checks.md` § Traps, `CLAUDE.md` commands |
| Base branch, PR or commit conventions | `pull-request.md`, `commit-message.md`, `.octopus/project.json` |
| A review agent added, removed or re-scoped | the lens table in `.claude/docs/actions/README.md`, `.claude/agents/*`; then regenerate the Codex mirrors with `go tool devtool codex-agents` (`go test` fails while they are stale) |
| An action file added or renamed | `.claude/docs/actions/README.md`, `.conductor/settings.toml` `[prompts]`, `.octopus/instructions/*` |
| Workspace setup/run needs (new dependency manager, ports) | `internal/devtool/devtool.go` (test-first), `.octopus/scripts/*`, `.conductor/settings.toml`, `.conductor/README.md` |
| A `.claude/docs/*` file added or removed | `.claude/docs/README.md` |

Runner configs (`.conductor/settings.toml`, `.octopus/instructions/*`) hold only invariants and a plain
path — never fix a drift by adding a rule there.

## Step 3: Propose — mandatory before any edit

| File | Section | Current (summary) | Proposed | Why |
|---|---|---|---|---|

Ask: "Apply N change(s)? 1. Yes, all 2. Let me pick 3. Skip". **Wait.** Never edit these files
without confirmation.

## Step 4: Apply

English only. Keep instructions short: a rule an agent cannot act on, or one obvious from the code, does
not belong. Remove rules whose basis is gone. Then run check 7 of [`/check`](check.md) (`go tool devtool check-docs`).
