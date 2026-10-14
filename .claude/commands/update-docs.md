Check whether the documentation matches the current changes, and update it after confirmation.

## Step 1: What changed
```bash
git diff main --name-only
git status --short
```

## Step 2: Map changes to documents

| Change | Document that must agree |
|---|---|
| Behavior, module, contract, command of a feature | `docs/features/<feature>.md` (from `docs/features/_TEMPLATE.md`) and its row in `docs/README.md` |
| A significant choice (library, version, boundary, policy) | an ADR in `docs/decisions/` (from `_ADR_TEMPLATE.md`); its row in `docs/README.md`; the register entry in `docs/decisions/project-baseline.md` if it closes an O-item |
| Work on a numbered task | `docs/verification/<NNN>-<slug>.md`; the task's status in **both** its file and the `product/tasks/README.md` navigation; the "Execution result" section at the end of the task file |
| Build/check commands, directories, current state | `README.md` (commands, directory map, current state) |
| Dependency added or bumped | the version table in `docs/decisions/toolchain-versions.md` |
| Agent rules, module boundaries | `AGENTS.md` |

Read each changed file and the document it maps to. Note only real gaps: a planned feature is not
described as implemented; a document never claims a check that was not run.

## Step 3: Propose

Show a table — document, section, what is missing or wrong, proposed change — then ask:
"Update N document(s)? 1. Yes, all 2. Let me pick 3. Skip". Wait for the answer.

## Step 4: Apply

All repository text is English. Keep documents factual and short; link instead of repeating. After
editing, run check 7 of [`/check`](check.md) (`go tool devtool check-docs`).
