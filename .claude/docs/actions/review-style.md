# Action: review a branch / diff — architecture and quality

The **Architecture & quality** lens only; blockers and reporting rules are in [review.md](review.md).
Nothing here blocks a merge alone unless `review.md` lists it, but a violation is a violation — report
it under its own heading.

---

### Go

- `gofmt` clean, `go vet` clean.
- Small domain packages; narrow interfaces declared where they are used; dependencies passed
  explicitly (constructors, function parameters), no hidden globals.
- Every error is checked or deliberately returned; no `_ =` on an error that matters.
- Goroutines have an owner, a `context` for cancellation and a guaranteed exit; resources (files,
  the torrent client, DB handles) are closed on every path.
- Pure policy is separate from I/O so it can be tested without the OS.
- `main` stays a thin composition root (see `main.go` and `cmd/devtool/main.go`).

### React and TypeScript

- Strict mode holds: no unjustified `any`, non-null `!` or `as` assertions.
- Components receive their dependencies (bridge functions, data) as props; only the composition root
  wires the real bridge — the pattern in `features/runtime-check`.
- One owner per piece of state; subscriptions are cleaned up; no business state copied into
  components.
- Accessible by default: real buttons, labels, `role="status"` / `role="alert"` for results, keyboard
  operation.

### Contracts

- Go–TS data crosses as the project's own DTOs with explicit units (bytes, bytes/s, time, file index).
- Risky contracts — bindings, events, goroutines, file operations — document lifecycle, ordering,
  errors and cancellation in place (AI-05).

### Tests

- Tests assert behavior and include negative cases; no test that only proves a line ran.
- Test doubles replace external boundaries only and are named as such; they are never presented as
  the real integration (QA-06).
- The verification report records the RED command and its expected failure, the GREEN commands and
  the raw coverage result.

### Code and naming

- Names describe the domain action in plain English (AI-02); one main role per file or type (AI-03).
- Comments match the density of the surrounding code: explain a contract, a trap or a "why", never
  restate the code.
- No unrelated rewriting in the diff (AI-09).

---

Report as in [review.md](review.md): `file:line`, the problem, the fix.
