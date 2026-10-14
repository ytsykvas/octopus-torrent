# Action: review with several agents

Run the three lenses from [README.md](README.md) in parallel, each as its own subagent, and merge the
results. Give each agent the changed files (`git diff <base>...HEAD --name-only`) and the commit list;
do **not** paste the checklists — each agent definition loads only its half.

1. `code-reviewer` — architecture & quality.
2. `security-reviewer` — attack surface & data safety.
3. `tech-lead` — design & readiness against the task's acceptance criteria.

Merge into one report: changed files, then one section per lens, then a 3–5 bullet summary
(readiness, blockers, recommended fixes). Drop duplicates, keeping the finding under the lens that
owns it. Every finding carries `file:line`. Reviewing is read-only.

The base is `main` unless the branch says otherwise.
