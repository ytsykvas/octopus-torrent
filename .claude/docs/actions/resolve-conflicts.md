# Action: resolve merge conflicts

The base branch is `main`. Once a branch has an open pull request, merge `main` in rather than
rebasing — a rebase rewrites what the PR is made of. Rebasing is fine before a PR exists.

Work out what **both** sides intended before choosing; taking one side wholesale is how a change
silently disappears.

- **Generated bindings** (`frontend/wailsjs/`): never merge by hand. Resolve the Go facade, then run
  `go tool wails generate module` and take its output.
- **Lockfiles** (`frontend/package-lock.json`, `go.sum`): resolve `package.json` / `go.mod` first,
  then regenerate with `npm --prefix frontend install` / `go mod tidy`; the 14-day cooldown still
  applies to any version that moves.
- **Task status**: a status must agree between the task file and the `product/tasks/README.md` navigation;
  keep the more advanced status only if its evidence is present.
- **Verification reports and ADRs**: keep both sides' entries; they are history, not alternatives.

After resolving, run [`/check`](../../commands/check.md) before considering it done.
