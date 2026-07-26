# core-engine — delta

## MODIFIED Requirements

### Requirement: Value construction is depth-bounded

Value construction that can increase nesting depth — the VM `OpMakeList`,
`OpMakeVector`, `OpMakeMap` opcodes and the stdlib `list`/`cons`/`vector`/
`conj`/`assoc`/`merge` builders and `json/decode` — SHALL reject a result whose
nesting depth exceeds `MaxStructuralDepth` (default 1024) with a terminal
`ResourceLimitError` (`CodeResourceLimit`). The depth check SHALL be bounded so
it cannot itself overflow the Go stack (descend at most `MaxStructuralDepth + 1`
levels). Value *breadth* (a wide flat collection) SHALL NOT be limited by this
requirement.

Enforcing the bound SHALL NOT cost time proportional to the collection being
extended. Extending an already-checked collection can only exceed the limit
through the element being added, so the check for `cons` and `conj` SHALL be
bounded by that element rather than by the accumulated result — otherwise a
loop accumulating collections is quadratic in time while allocating linearly.
This is a bound on the enforcement cost, not a relaxation of the bound itself:
the same constructions are rejected, at the same limit.

#### Scenario: Deeply nested construction fails with a terminal error

- **WHEN** a script builds a value whose nesting exceeds `MaxStructuralDepth` (for example via `loop`/`recur` wrapping `list` repeatedly, or `json/decode` of deeply nested input)
- **THEN** construction SHALL return a terminal `ResourceLimitError`, not crash the process, and the error SHALL NOT be catchable by in-script `try`/`catch`

#### Scenario: Escalating nesting through cons or conj still fails

- **WHEN** a loop repeatedly wraps its accumulator as the added element, so each step nests one level deeper, past `MaxStructuralDepth`
- **THEN** construction SHALL return a terminal `ResourceLimitError`, whether the wrapping is done with `cons` or with `conj`

#### Scenario: Accumulating collections stays linear

- **WHEN** a loop conses a small collection onto a growing accumulator at sizes spanning several doublings
- **THEN** the time SHALL grow in proportion to the number of elements added rather than to its square, matching the allocation growth for the same loop

#### Scenario: Wide flat collections are unaffected

- **WHEN** a script builds a shallow collection with many elements
- **THEN** it SHALL succeed, bounded only by the allocation ledger, not by the depth limit
