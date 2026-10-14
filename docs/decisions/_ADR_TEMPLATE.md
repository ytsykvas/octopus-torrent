# <Short decision name>

**Status:** `proposed` | `accepted` | `superseded` (by: <link>)  
**Date:** <YYYY-MM-DD>  
**Task:** <link to the numbered task>  
**Register item:** <O-number from the [baseline](project-baseline.md#6-open-decision-register) or "new">  
**Requirements:** <ID>

The file is named `docs/decisions/<slug>.md` (an English slug, as in the baseline register). An ADR is needed for a significant choice (a version, driver, contract, data format, macOS mechanism), not for every cosmetic change.

## Question

What decision is needed, why now and what depends on it. Constraints from the requirements, the architecture and the agreed stack.

## Decision

What exactly is chosen: specific versions, APIs, modes, module boundaries. Unambiguous, so that the next agent can apply it without the chat history.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|

## Verified grounds

Facts, not intentions: a spike, test, build, measurement. For each: the command, the environment (Mac, macOS, versions), the test data, the result `pass`/`fail`/`not_run` and a link to the report in `docs/verification/`. Separately mark assumptions not yet proven by a check.

## Consequences

What changes in the code, contracts, commands and documents; new risks and limitations; what needs to be rechecked if conditions change (a new dependency version, a different macOS). A change to agreed behavior or the stack needs the owner's decision before the `accepted` status.
