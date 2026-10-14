# 020 — Implement folder selection and a verified file path plan

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [009](009-storage-and-completion-spike.md), [019](019-metainfo-parser-file-tree.md), [015](015-transactional-repositories.md)  
**Requirements:** `ADD-04`, `ADD-08`, `SET-01`, `SET-05`, `STACK-01`

## Outcome

Implement in Go an explicit folder selection and a file placement plan that does not allow third-party data to be silently overwritten.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-04`, `ADD-08`, `SET-01`, `SET-05`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): the API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Build the mapping of physical paths according to the chosen storage model and re-verify them before use.
2. Create a named folder only after explicit confirmation; do not silently overwrite an already existing folder or file.
3. On the target volume, check case and Unicode collisions and escaping the allowed root through symbolic links.
4. Check access permissions and free space in advance, and also handle actual write errors that may occur later.
5. Apply a change of the default folder only to future imports; do not replace an unusable destination with another one without the user's decision.

## Artifacts and change locations

**Proposed locations:** `internal/files/path_policy.go; destination_plan.go`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/020-destination-filesystem-service.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Canceling the preview does not create the final folder.
- [ ] A folder creation error leaves the previous path value unchanged.
- [ ] The plan contains only the files of the torrent's specific verified list.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Substitute the parent folder or the final file with a symbolic link and rename the path between the check and the use; access must not leave the allowed area.
- Check existing paths, case collisions, a volume without write permission and a full disk; the application must show the exact reason and not change the previous path or third-party files.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Approving the root path before writing does not guarantee safety after it is substituted or renamed. The implementation must rely on verified file APIs of the pinned Go version and on the actual filesystem behavior of the target Mac.

**Basis for technical decisions:** [Go filesystem roots](https://go.dev/blog/osroot). During implementation, check the signatures and constraints against the pinned version. Reading the API documentation does not replace a performed integration check.

Verifying the destination folder does not invoke the Trash. Spike 012 may be used as a reference about paths, but the absence of a working Trash mechanism blocks 032, not ordinary torrent adding. In this task, path and access verification has its own tests.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
