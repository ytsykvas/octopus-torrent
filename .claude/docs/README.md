# .claude/docs

Agent-facing detail that is not a project rule. The rules themselves live in
[AGENTS.md](../../AGENTS.md) (loaded by `CLAUDE.md`), [TDD_AND_QUALITY.md](../../docs/engineering/TDD_AND_QUALITY.md)
and [the architecture](../../product/APPLICATION_ARCHITECTURE.md); these files never restate them.
None is loaded automatically — read the one that fits the task.

| File | Read it when |
|---|---|
| [planning.md](planning.md) | writing a plan in plan mode — what a plan for a numbered task must decide |
| [dependencies.md](dependencies.md) | adding or upgrading an npm package, a Go module or a toolchain |
| [actions/README.md](actions/README.md) | performing a runner action: review, pull request, commit message, fixing checks, addressing review, resolving conflicts. Single source for the review agents, `/pushit`, Conductor and octopus |

## What is where

| Path | What |
|---|---|
| `.claude/agents/` | `code-reviewer`, `security-reviewer`, `tech-lead` (review lenses), `macos-qa` (drives the real `.app`) |
| `.claude/commands/` | `/task`, `/check`, `/branch-review`, `/qa`, `/update-docs`, `/update-rules`, `/pushit` |
| `.claude/settings.json` | shared permissions and the `release-guard` hook (`go tool devtool hook release-guard`) |
| `.codex/` | the same agents and hook for Codex |
| `.octopus/`, `.conductor/` | parallel-workspace runners: setup/run scripts and action prompts pointing here |

Why it is shaped this way: [agent-tooling ADR](../../docs/decisions/agent-tooling.md).
