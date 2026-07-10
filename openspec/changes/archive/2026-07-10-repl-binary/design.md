# Design — repl-binary

## Context

Library REPL (`runtime/repl.go`) is io-based and stays that way. The binary adds
terminal handling on top. Grill decision: `golang.org/x/term` only — no
third-party readline, zero CGO.

## Goals / Non-Goals

- Goals: usable interactive REPL (editing, history, multiline, signals), dialect/bytecode flags, file execution.
- Non-Goals: completion, syntax highlighting, Windows console quirks beyond what x/term provides, changing `Engine.REPL`.

## Decisions

- **Hand-rolled line editor over `x/term.Terminal`.** `term.Terminal` already
  provides prompt, history, and basic editing over a raw-mode connection — use it
  rather than writing key handling from scratch. If its editing proves too
  limited, extend locally; still no third-party dep. Alternative — chzyer/readline
  — rejected by grill decision (dep posture).
- **Balance check reuses the library rule.** The comment-aware `isBalanced`
  (fixed in review-bugfix-batch) is exported or shared internally so binary and
  library agree on "input complete". Two divergent balance rules was the exact
  bug class just fixed.
- **History at `${XDG_STATE_HOME:-~/.local/state}/lispico/history`**, plain text,
  one entry per line, loaded into `term.Terminal`'s history; failures to read or
  write are silently non-fatal.
- **Non-TTY fallback.** `term.IsTerminal(stdin)` false → plain `bufio` loop
  through the existing io-based REPL path, keeping pipes and tests trivial.
- **Binary layout per project convention:** source `cmd/lispico/main.go`, build
  output `bin/lispico` via the project's build tooling.

## Risks / Trade-offs

- [`term.Terminal` editing is minimal (no kill-ring, limited keys)] → accepted for v1; the seam allows swapping the editor later without touching the engine.
- [Raw mode left enabled on panic/exit] → single deferred restore around the session; signals routed through the same cleanup.

## Migration Plan

Additive — new binary, one new x/ dependency. Rollback = delete `cmd/lispico`.

## Open Questions

None.
