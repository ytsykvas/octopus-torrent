Pre-flight checks, commit, and — when a remote exists — push and open a pull request.

Phases: **Update docs → Update rules → Safety scan → Checks → Commit → Push & PR**. Stop and ask the
owner whenever a phase finds a problem.

## Phase 0a: Update documentation
Run `/update-docs` and follow its flow.

## Phase 0b: Update rules
Run `/update-rules` and follow its flow (it asks before editing anything).

## Phase 1: Safety scan
```bash
git status --short
git diff -U0 | grep -iE '(api_key|secret|password|token|private_key|passkey)\s*[:=]'
git diff --cached -U0 | grep -iE '(api_key|secret|password|token|private_key|passkey)\s*[:=]'
```
Nothing personal or generated may be committed: `.torrent` files, downloaded payload, `*.sqlite*` /
`*.db`, logs, `build/bin/`, `build/coverage/`, `frontend/dist/`, `frontend/node_modules/`, `.env*`,
keys and certificates. `.gitignore` already covers them — a match here means a new pattern is needed.
On a problem show a table (file, problem, suggestion) and ask: fix / continue / abort.

## Phase 2: Checks
Run [`/check`](check.md) — all of it. On failures show its table and ask: fix / continue / abort.

## Phase 3: Commit
Stage deliberately (`git add <paths>`; `git add -A` only after Phase 1 is clean), review
`git diff --cached --stat`, and write the message per
[commit-message.md](../docs/actions/commit-message.md). Never `--no-verify`.

## Phase 4: Push & pull request
```bash
git remote
```
- **No remote:** stop here and say: "Committed locally; push and PR skipped — the repository has no
  remote yet." Do not create one; that is the owner's decision.
- **Remote exists:** `git push -u origin HEAD`. Never force-push automatically; on a non-fast-forward
  rejection merge `main` in ([resolve-conflicts.md](../docs/actions/resolve-conflicts.md)) or ask
  about `--force-with-lease`. Then `gh pr view --json number,url,state`; create or update the PR per
  [pull-request.md](../docs/actions/pull-request.md) with `--base main`.

Finish with: branch, commit hash and subject, files changed, PR URL or the reason there is none.
