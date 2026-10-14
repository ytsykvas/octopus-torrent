# Action: open a pull request

The format for an OctopusTorrent pull request. Single source for `/pushit`, the `create_pr` prompt in
`.conductor/settings.toml` and `.octopus/instructions/pull-request.md`.

The commit itself: [commit-message.md](commit-message.md).

## Remote

`origin` is the owner's GitHub repository `https://github.com/ytsykvas/octopus-torrent`. If `git remote`
is empty (a copy without it), do not add one and do not open a PR: say that push and PR were skipped.

## Base branch

`main`. Never force-push; `--force-with-lease` only after the owner says so.

## Title

English, imperative, under 80 characters. Name the task when there is one: `003: Build the first
Wails .app on the owner's Mac`.

## Body

Two sections, in this order, describing the whole branch (`git diff main...HEAD`), not only the last
session:

### `## Summary`

Under five sentences: the behavior or document change, the modules touched, the tests added.

### `## Verification`

The commands that passed (tests, coverage with the raw result, analyzers, build, `.app` checks), and
every check that is `not_run` or `fail` with its reason. Link the `docs/verification/` report.

End the body with the session URL line the session provides.

## Checks before opening

The checks in [`/check`](../../commands/check.md) must pass. When one fails,
[fix-checks.md](fix-checks.md) says how to fix it.
