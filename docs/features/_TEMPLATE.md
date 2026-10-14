# <Feature name>

**Status:** `planned`  
**Requirements:** <ID>  
**Related tasks:** <links>  
**Last verified version:** <not yet implemented / commit or source hash>

## Goal and behavior

Who performs the action, preconditions, the positive outcome, boundaries and observable errors. Distinguish planned from implemented.

## Architecture analysis before the change

Affected modules, state owner, dependencies, side effects, cancellation, reconciliation after a crash. What can already be reused; what needs refactoring; why the chosen design is sufficient. Links to ADRs if needed.

## Contracts and data

DTO/API, units, validation, persistence, migrations, revisions and errors. The engine's own objects do not become a UI contract.

## Scenarios and TDD

- Expected behavior and the test that initially fails for it.
- Expected RED reason, command and observation.
- Minimal GREEN implementation, required regression and refactoring on green tests.
- Negative, crash and concurrency cases relevant to this feature.

## Implementation and reuse

Actual modules, shared components/services and dependency directions. Do not copy all the code here; explain responsibilities and important invariants.

## Checks and 100% coverage

Exact commands, environment, test data, code version, `pass/fail/not_run`. Links to raw profiles, the inventory of own files and all defined 100% metrics. Separate module-level checks from the real engine and the system `.app`. Until executable code exists — `not_applicable`, without an invented percentage.

## Refactoring and debt check

Which duplications or wrong boundaries were removed, how behavior and 100% coverage were preserved. An unresolved workaround required by the current function blocks completion; explain how it will be removed.

## Usage and recovery

Short route, settings, possible errors and safe recovery. Do not show internal technical details in the product UI unless the user needs them.

## Change history and evidence

Date/version, what changed, links to tasks, decisions and actual reports. Do not call planned checks completed.
