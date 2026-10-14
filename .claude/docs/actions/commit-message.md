# Action: write a commit message

English. A subject line under 72 characters in the imperative ("Add …", not "Added …"), a blank
line, then the body.

The body explains, for the next agent and the owner (AI-09):

- the result — what behavior or document changed and which task it belongs to;
- how it was verified — the commands that passed, the coverage result, and anything `not_run` with
  the reason;
- known limitations or the next step, when there are any.

The subject is not a file list; the diff already shows what was touched. Keep one coherent change per
commit — for example a dependency bump separate from the code it forces.

End the message with the `Claude-Session:` line the session provides, as earlier commits in this
repository do. No `Co-Authored-By` trailer.

Do not commit unless asked to, and never with `--no-verify`.
