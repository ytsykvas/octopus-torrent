Check whether the documentation in `docs/` matches the current changes, and update or create documents after confirmation.

## Step 1: What changed

```bash
git diff main --name-only
git status --short
```
On `main` itself, use the working tree (`git diff --name-only`, `git diff --cached --name-only`,
untracked files from `git ls-files --others --exclude-standard`).

## Step 2: Understand the changes

Read the changed files and name the flows they add or change. Focus on:

- facade methods, request or snapshot DTOs, error codes, enumerations;
- the operation ledger, bridge wrappers, decoders, ordering rules;
- React features and their props;
- `devtool` commands and gate steps, runner scripts, agents and commands;
- configuration that changes behavior (`wails.json`, `Info*.plist`, `vite.config.ts`, `.npmrc`,
  `go.mod`).

## Step 3: Map changes to documents

```bash
git ls-files -co --exclude-standard 'docs/*.md'
```

| Change | Document that must agree |
|---|---|
| Behavior, module, contract or command of a feature | its feature document `docs/<domain>/<feature>.md` (from `docs/_FEATURE_TEMPLATE.md`), and its row in `docs/README.md` |
| How a domain works (flow, API, recipe) | the guide or reference in that domain. `app/`: shell, facade, operations, the library store (`internal/library`); `bridge/`: contract, calling Go, ordering, adding a command; `frontend/`: structure; `tooling/`: gate, devtool, task plan, test torrents and the swarm (`internal/torrentengine/testutil`, and the engine contract tests in `internal/torrentengine`), the engine spike app (`cmd/engine-spike`, `internal/enginespike`, `frontend/src/spike`), agent tooling; `development/`: getting started, testing, workflow |
| A significant choice (library, version, boundary, policy) | an ADR in `docs/decisions/` (from `_ADR_TEMPLATE.md`), its row in `docs/README.md`, and the register entry in `docs/decisions/project-baseline.md` if it closes an O-item |
| Work on a numbered task | `docs/verification/<NNN>-<slug>.md`; the status in **both** the task file and the `product/tasks/README.md` navigation (`go tool devtool plan-nav`); the "Execution result" section |
| Build or check commands, directories, current state | `README.md` (commands, directory map, current state) and [getting started](../../docs/development/getting-started.md) |
| A dependency added or bumped | the version table in `docs/decisions/toolchain-versions.md` |
| Agent rules, module boundaries | `AGENTS.md` (instructions in `.claude/` belong to `/update-rules`) |

For each flow decide its status:

- **Missing:** no document exists yet.
- **Outdated:** a document exists but is wrong.
- **Current:** a document exists and is right.

A planned feature is never described as implemented, and a document never claims a check that did
not run.

## Step 3b: Reconcile the reference tables in full

Some documents list things that live in the code, so they decay silently: someone adds a method,
and nobody remembers the table. Compare them **both directions and in full**, not only against the
diff. The diff only catches this branch; the full comparison also catches what earlier branches
missed.

```bash
grep -hoE 'func \([a-z]* \*App\) [A-Z][A-Za-z]*' internal/app/*.go | sed 's/.* //'            # bound methods → docs/app/facade.md "Bound methods"
grep -o 'usage: devtool [^`]*' internal/devtool/devtool.go                               # commands → docs/tooling/devtool.md
grep -hoE 'export (async )?function [a-zA-Z]+' frontend/src/bridge/*.ts | sed 's/.* //'  # wrappers → docs/bridge/calling-go.md "Wrappers" (callGo is the helper)
grep -o '"[a-z]*\.[a-zA-Z]*"' internal/app/contracts/testdata/vocabulary.json            # error codes → ADR api-contract "Errors" table (codes only; error.unknown is a message key)
grep -o '"name": "[a-z0-9-]*"' internal/torrentengine/testutil/fixture/testdata/fixtures.json  # fixtures → docs/tooling/test-torrents.md catalog tables
```

- **Missing row:** the code has it and the document does not. Action: Create.
- **Stale row:** the document lists something the code no longer has. Action: Remove.

## Step 4: Present findings and ask

| Flow / Feature | Document | Status | Action |
|---|---|---|---|
| … | `docs/…` or *none* | Missing / Outdated / Current | Create / Update / None |

Include the Step 3b gaps as rows in the same table. Then ask:
"Found N document(s) that need attention. 1. Yes, update/create all 2. Let me pick 3. Skip".
**Wait for the answer**; nothing is written unasked.

## Step 5: Apply

- **New document:** put it in `docs/<domain>/`. Start it with `> Last updated: <date>`, then an
  overview, a **Flow** (the call chain from the user's action to the result), behavior, any
  configuration, and examples. Feature documents use the template's sections. Add the document to
  `docs/README.md`.
- **Existing document:** update only the affected sections and the `Last updated` date. Add a
  section if the change brings new behavior.
- Document behavior and how to use it, not a file-by-file tour. Link instead of repeating; the ADR
  keeps the reasons.
- Every claim must be true in the code at this commit. Name the file or test that shows it, and do
  not write from memory.

## Rules

- All repository text is English.
- Never delete a document without asking. A move keeps its history (`git mv`), and every link to it
  is fixed in the same change.
- If unsure whether a change needs a document, put it in the table and let the owner decide.
- Afterwards run `go tool devtool check-docs`; after a task status change also run `plan-nav` and
  `check-plan`.
