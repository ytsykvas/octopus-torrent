# Conductor configuration

[Conductor](https://www.conductor.build/) runs several Claude Code agents in parallel, each in its own
git worktree. This folder makes each workspace ready to work with no manual steps.

| File | Purpose |
|---|---|
| `settings.toml` | files copied into each workspace, the `setup` / `run` scripts, and the `[prompts]` for the Actions tab — invariants plus a plain path into [`.claude/docs/actions/`](../.claude/docs/actions/README.md), never a copy of the rules |

`setup` and `run` reuse [`.octopus/scripts/`](../.octopus/README.md): both runners prepare and start a
workspace the same way, and the logic behind the scripts is tested Go (`cmd/devtool`).
`workspace run` reads `OCTOPUS_PORT` first and falls back to `CONDUCTOR_PORT`.

## Isolation model

- **Port** — `wails dev` serves on the workspace's `$CONDUCTOR_PORT`; Vite picks its own free port.
- **Window** — each running workspace opens its own dev copy of the app.
- **Data** — `workspace run` sets `OCTOPUS_DATA_DIR=<worktree>/build/workspace-data`, so each
  workspace has its own library and nothing is shared; deleting the worktree removes it. After
  switching a workspace to an older branch, delete `build/workspace-data`: its library may be of a
  newer version.
- **Secrets** — none. Only `.claude/settings.local.json` is copied, for the permission allowlist.

## Requirements on the host

Go ≥ 1.27, Node.js ≥ 22.12 with npm ≥ 11.20, Xcode Command Line Tools — the same as a normal checkout
([README](../README.md)).
