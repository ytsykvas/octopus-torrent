# Agent action instructions

Canonical instructions for the actions an agent performs on a branch: review, pull request, commit
message, fixing checks, addressing review comments, resolving conflicts. One file per action, so an
action loads only what it needs.

Every runner points here instead of carrying its own copy:

| Action | File | Conductor prompt | octopus file |
|---|---|---|---|
| Review a branch / diff | [review.md](review.md) + [review-style.md](review-style.md) or [review-security.md](review-security.md) | `code_review` | `review.md` |
| Review with several agents | [multi-agent-review.md](multi-agent-review.md) | — | `multi-agent-review.md` |
| Open a pull request | [pull-request.md](pull-request.md) | `create_pr` | `pull-request.md` |
| Write a commit message | [commit-message.md](commit-message.md) | `create_pr` | `commit-message.md` |
| Fix failing checks | [fix-checks.md](fix-checks.md) | `fix_errors` | `fix-checks.md` |
| Address review comments | [address-review.md](address-review.md) | — | `address-review.md` |
| Resolve merge conflicts | [resolve-conflicts.md](resolve-conflicts.md) | `resolve_merge_conflicts` | `resolve-conflicts.md` |

The interactive commands in [`.claude/commands/`](../../commands) (`/check`, `/branch-review`,
`/pushit`) keep their own flow (tables, confirmation gates) and read the criteria from here.

## Lenses — when several reviewers run in parallel

| Lens | Agent | Reads | Owns |
|---|---|---|---|
| Architecture & quality | `code-reviewer` | `review.md` + `review-style.md`, never `review-security.md` | module boundaries, TDD evidence and 100% coverage, docs in the same change, generated bindings, Go and React/TypeScript conventions, technical debt |
| Attack surface & data safety | `security-reviewer` | `review.md` + `review-security.md`, never `review-style.md` | file deletion and path safety, the Wails boundary, untrusted torrent/magnet input, privacy, supply chain |
| Design & readiness | `tech-lead` | neither checklist | design trade-offs, resource lifetimes and concurrency, whether the task meets its acceptance criteria and may become `done` |

`code-reviewer` and `security-reviewer` overlap on the module boundaries by design: a boundary
violation is both an architecture defect and the usual route to a data-safety bug.

## Rules for these files

- They hold only what is specific to the action. Project rules live in [AGENTS.md](../../../AGENTS.md),
  [docs/engineering/TDD_AND_QUALITY.md](../../../docs/engineering/TDD_AND_QUALITY.md) and the
  architecture; link to them, never restate them. `review.md` and its two halves are the one deliberate
  exception: a reviewer needs one flat list ordered by severity, so they mirror those rules and
  `/update-rules` keeps them in step.
- Runner configs (`.conductor/settings.toml` `[prompts]`, `.octopus/instructions/*`) carry only the
  invariants that must hold even if nothing is read, plus a **plain path** to the file here — never
  `@path`, which would load the file into every run whether it is needed or not. New rules go into these
  files, never into a runner config.
- A renamed file here silently breaks a runner prompt. `/check` verifies that every referenced
  `.claude/**.md` path exists.
- Never `Read` a screenshot in a long session: one image costs tens of thousands of tokens on every
  later turn. Hand visual checks to a subagent (`macos-qa`) and keep only its verdict.
