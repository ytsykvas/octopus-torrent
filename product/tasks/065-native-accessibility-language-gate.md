# 065 — Check keyboard, VoiceOver and macOS WebView

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [054](054-global-upload-ui-mvp-gate.md), [062](062-automation-end-to-end-gate.md), [041](041-six-languages-formatters.md), [042](042-appearance-theme-opaque.md)  
**Requirements:** `UI-04`, `UI-06`, `SET-06`, `SET-04`, `STACK-05`

## Outcome

Check accessibility in the real built WKWebView .app.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-04`, `UI-06`, `SET-06`, `SET-04`, `STACK-05`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Go through the six languages, system/light/dark, opaque, contrast and text scale.
2. Perform the main actions with the keyboard only, including the menu, a dialog, the tree and group selection.
3. VoiceOver reads full names/paths/statuses/errors without announcing every stats update.
4. ARIA tree role only with the full keyboard contract; nested lists are acceptable.
5. Check the actual Mac focus/Tab settings.
6. Record build/Mac/fixtures and pass/fail/not_run, and fix the specific gaps.

## Artifacts and change locations

**Proposed locations:** `docs/verification/native-accessibility.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/065-native-accessibility-language-gate.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] AS-11/12/17 pass at the system boundary.
- [ ] Focus is preserved during snapshots.
- [ ] Long text is accessible, elements do not overlap.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check focus return from a child dialog, removal of the selected record and collapsed folders.
- If virtualization is present, check focus/VoiceOver after rerender.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

ARIA tree role requires the full keyboard contract. Nested collapsible lists are acceptable. Browser WebKit is useful for regressions but does not prove the system integration of the .app.

**Basis for technical decisions:** [Apple WKPreferences](https://developer.apple.com/documentation/webkit/wkpreferences/tabfocuseslinks), [W3C modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

Keyboard and VoiceOver checks do not need to wait until the 064 profiling is complete: these are independent branches. If 064 later changes the DOM, virtualization or focus, repeat exactly the affected 065 scenarios on the new version; the earlier report does not confirm an interface that does not yet exist.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
