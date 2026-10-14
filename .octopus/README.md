# octopus configuration

[octopus](https://github.com/ytsykvas/octopus) runs several Claude Code agents in parallel, each in
its own git worktree with its own branch and dev server. This folder makes a workspace of this
repository work with nothing configured by hand.

| Path | What it is |
|---|---|
| `scripts/setup.sh` | prepares a workspace: `npm ci`, `frontend/dist` (embedded by `main.go`), Go module cache |
| `scripts/run.sh` | replaces itself with `wails dev` on the workspace's `OCTOPUS_PORT` |
| `instructions/` | per-action pointer into [`.claude/docs/actions/`](../.claude/docs/actions/README.md) plus the invariants that must hold even if nothing is read |
| `carry` | gitignored files copied into every new worktree |
| `project.json` | the base branch and the name, for a fresh install |

There is no `archive.sh`: a workspace creates nothing outside its worktree (no databases, no shared
storage), so deleting the worktree is the whole cleanup.

**The scripts are two-command hand-overs:** build `cmd/devtool` into `build/bin/devtool`, then `exec`
it. The logic lives in Go (`cmd/devtool`, `internal/devtool`) so it is tested and counted in the 100%
coverage gate like the rest of the code; the `cmd/devtool` test runs both scripts against a fake `go`,
which executes every line of them. Change the behavior in Go, test-first.

**`run.sh` replaces processes instead of nesting them** — `exec` into the devtool binary, which
`exec`s into the resolved `wails` binary — so the runner's PID is `wails dev` itself and Stop
(SIGTERM) reaches it directly; verified to stop everything within a second. SIGKILL cannot be
forwarded by anything: Vite and the dev app would be left running.

**Each workspace opens its own app window.** `wails dev` builds and launches a dev copy of the app;
Vite picks a free port of its own. Several workspaces running at once means several windows with the
same bundle ID — target an app by PID or path, never by bundle ID (see `macos-qa`).

**No credentials are here, and none ever will be.** The repository decides what runs; the machine
decides what it runs against.

Why the agent tooling is shaped this way: [agent-tooling ADR](../docs/decisions/agent-tooling.md).
