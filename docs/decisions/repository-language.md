# Repository language: English only

**Status:** `accepted`  
**Date:** 15 October 2026, owner's decision  
**Task:** none (repository-wide convention)  
**Requirements:** AI-01, AI-02, AI-06

## Question

The specifications, plan, ADRs and reports were written in Ukrainian; code identifiers were already
English (AI-02). In which language is text written into the repository?

## Decision

- **Everything written into the repository is English**: documentation, task files, ADRs,
  verification reports, agent instructions (`AGENTS.md`, `CLAUDE.md`, `.claude/`, `.codex/`,
  `.octopus/`, `.conductor/`), code comments, commit messages and pull requests.
- Conversation with the owner stays in Ukrainian; that is chat, not repository text.
- The only Ukrainian left is product data about the Ukrainian language itself — for example the
  language's own name in the mockup's language picker. The product's Ukrainian localization
  (task 041) is product data under the same exception.
- The existing documentation was translated on 15 October 2026. The Ukrainian originals are the
  repository state at commit `0f3d8ef` and the owner's copy in `~/Desktop/OctopusTorrent`; neither is
  a source of truth any more.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Keep Ukrainian documents, write only new text in English | No translation risk | Two languages in one repository; links and terms drift between them | Rejected by the owner |
| Translate gradually, file by file as tasks touch them | Spreads the effort | The plan and requirements would stay mixed for months | Rejected by the owner: everything at once |

## Verified grounds

- **Structure:** every Markdown file kept the same number, order and levels of headings as its
  original. Link fragments were rewritten mechanically from the heading positions (45 fragments), and
  every relative link and anchor resolves.
- **Meaning:** nine independent reviewers compared every sentence, list item and table row of every
  translated file against `git show 0f3d8ef:<path>`, without sampling. Each requirement row was
  included (80 IDs in `PROJECT_REQUIREMENTS.md`, every acceptance criterion of the 68 tasks, every
  cell of the plan's matrices). About 40 places were corrected, either for changed meaning or for
  ambiguity the original did not have. The most important was DEL-01, whose file-deleting option had
  become "Remove torrent and files" instead of "Delete torrent and files". Several others also restored
  the distinction between removing an **entry** ("Remove") and deleting **files** ("Delete"), and
  between the operation **journal** and diagnostic **logs**.
- **Mockup:** `MOCKUP.html` shows English UI copy by default (`lang="en"`); its script parses
  (`node --check`).
- **Leftover Cyrillic:** only the Ukrainian language name in the language picker of `MOCKUP.html`.

## Consequences

- `AGENTS.md` carries the rule; reviewers treat non-English repository text as a finding.
- Recurring task wording (phase, priority, size labels) and task titles were normalized so every task
  file and the navigation table use the same English text.
- Requirement IDs, task numbers, decision IDs and status tokens are unchanged, so references made
  before the translation still resolve.
