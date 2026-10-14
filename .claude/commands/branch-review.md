Review the changes on the current branch (or the uncommitted work, if the branch has no commits of its own) with three parallel reviewers.

## Steps

1. Pick the base: `main`. Run `git diff main...HEAD --stat`, `git diff main...HEAD --name-only` and
   `git log main..HEAD --oneline`. If the branch is `main` itself, review the uncommitted work instead
   (`git status --short`, `git diff --stat`, untracked files from
   `git ls-files --others --exclude-standard`).
2. If there is nothing to review, say so and stop.
3. Do **not** paste any checklist into the prompts — each agent definition loads only its own half
   ([lenses](../docs/actions/README.md)). Launch in parallel with the Agent tool:
   - **code-reviewer**: "Review these changes vs main. Changed files: [list]. Commits: [log]. Read the
     diffs and files yourself. Your lens is Architecture & quality."
   - **security-reviewer**: "Audit these changes vs main for data safety and attack surface. Changed
     files: [list]. Commits: [log]. Follow every path across the boundary it enters."
   - **tech-lead**: "Design and readiness review of these changes vs main. Changed files: [list].
     Commits: [log]. Task: [the task file, if the branch belongs to one]."
4. Merge the results.

## Output (English)

```
# Code review: <branch>

## Changed files
<files with one line each on what changed>

## Architecture & quality
<code-reviewer>

## Data safety & attack surface
<security-reviewer>

## Design & readiness
<tech-lead>

## Summary
<3–5 bullets: readiness, blockers, fixes before merge>
```

## Rules

- Every finding carries `file:line`; drop duplicates, keeping each under the lens that owns it.
- Skip `package-lock.json`, `go.sum`, binaries and generated `frontend/wailsjs/` unless they are out
  of sync with their source.
- Review only what the branch changed. Reviewing is read-only.
