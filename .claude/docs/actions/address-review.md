# Action: address review comments

1. Read every comment and the code it points at before changing anything. Group comments that touch
   the same behavior.
2. For each comment decide: fix, or reply with a reason not to. A comment that asks to weaken the
   quality gate, skip TDD, delete files without confirmation or bypass the release cooldown is
   answered with the rule it would break ([review.md](review.md)), not applied.
3. Fix behavior changes test-first (RED → GREEN → REFACTOR), keep coverage at 100%, and update the
   feature doc, ADR or verification report the comment affects.
4. Run [`/check`](../../commands/check.md).
5. Reply per comment: what changed (`file:line`) or why nothing did. Commit per
   [commit-message.md](commit-message.md).
