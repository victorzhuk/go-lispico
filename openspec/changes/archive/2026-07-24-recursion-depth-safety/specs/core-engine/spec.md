# core-engine — delta

## ADDED Requirements

### Requirement: Value construction is depth-bounded

Value construction that can increase nesting depth — the VM `OpMakeList`,
`OpMakeVector`, `OpMakeMap` opcodes and the stdlib `list`/`cons`/`vector`/
`conj`/`assoc`/`merge` builders and `json/decode` — SHALL reject a result whose
nesting depth exceeds `MaxStructuralDepth` (default 1024) with a terminal
`ResourceLimitError` (`CodeResourceLimit`). The depth check SHALL be bounded so
it cannot itself overflow the Go stack (descend at most `MaxStructuralDepth + 1`
levels). Value *breadth* (a wide flat collection) SHALL NOT be limited by this
requirement.

#### Scenario: Deeply nested construction fails with a terminal error

- **WHEN** a script builds a value whose nesting exceeds `MaxStructuralDepth` (for example via `loop`/`recur` wrapping `list` repeatedly, or `json/decode` of deeply nested input)
- **THEN** construction SHALL return a terminal `ResourceLimitError`, not crash the process, and the error SHALL NOT be catchable by in-script `try`/`catch`

#### Scenario: Wide flat collections are unaffected

- **WHEN** a script builds a shallow collection with many elements
- **THEN** it SHALL succeed, bounded only by the allocation ledger, not by the depth limit

### Requirement: Value-tree walks cannot crash on pathological depth

`String`, `Equals`, `ValueDeepBytes`, and `ValueNodeCount` SHALL be depth-bounded
so that a value exceeding `MaxStructuralDepth` degrades safely — a truncation
marker for `String`, a defined result for `Equals`, a capped count for the
byte/node walks — rather than recursing until the Go stack overflows. Ordinary
values within the depth limit SHALL be walked exactly as before.

#### Scenario: Stringifying an over-deep value does not crash

- **WHEN** `String()` (or an `Equals`/deep-bytes walk) is called on a value deeper than `MaxStructuralDepth`
- **THEN** it SHALL return a bounded result and SHALL NOT trigger a Go stack overflow

### Requirement: Bytecode compilation is depth-bounded

`Compiler.Compile` SHALL compare its compile depth against a limit (default 1024)
and return a terminal `ResourceLimitError` when exceeded, so a deeply nested form
— including one produced by macro expansion after the reader's own depth cap —
cannot overflow the Go stack during compilation. `literalDepth()` SHALL be
guarded the same way.

#### Scenario: Macro-expanded deep form fails to compile safely

- **WHEN** a macro expands to a form nested beyond the compile depth limit and that expansion is compiled
- **THEN** compilation SHALL return a terminal `ResourceLimitError`, not crash the process
