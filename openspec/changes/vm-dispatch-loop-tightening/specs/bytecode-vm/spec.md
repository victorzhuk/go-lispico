# bytecode-vm — delta

## MODIFIED Requirements

### Requirement: Bytecode VM robustness

The bytecode VM SHALL never panic on any input — valid source, a malformed form, or
a structurally malformed chunk; it SHALL return an error instead. Every error the
VM returns SHALL be a `*core.LispicoError`. For every special form the Compiler
handles, arity and shape SHALL be validated before any operand is indexed, so no
malformed special form can panic compilation. Structural validation of a chunk —
constant indices, symbol-constant types, jump and loop targets, and sub-chunk
references — SHALL happen once when the chunk is constructed or enters the chunk
cache; a chunk that fails validation SHALL be rejected there with a typed error
and SHALL never execute. Execution of a validated chunk SHALL NOT re-validate
these properties per instruction and SHALL still never panic.

#### Scenario: Empty-body function

- **WHEN** an empty-body function such as `((fn []))` or an empty-body `defn` is evaluated under `WithBytecode()`
- **THEN** the VM SHALL return an error, never panic

#### Scenario: Malformed chunk rejected at load

- **WHEN** a chunk contains an opcode referencing an out-of-range stack slot, constant index, jump target, or a non-symbol where a symbol constant is required
- **THEN** it SHALL be rejected with a `*core.LispicoError` before any instruction runs, never indexing out of range and never panicking

#### Scenario: Max call depth is a typed error

- **WHEN** VM execution exceeds the maximum call depth
- **THEN** the returned error SHALL satisfy `errors.As(err, &lispicoErr)` like every other VM error

#### Scenario: Malformed special form is a typed error

- **WHEN** any compiled special form is given too few, too many, or wrongly shaped operands and evaluated through `Engine.Eval` under `WithBytecode()`
- **THEN** the result SHALL be a typed error from validation performed before operand indexing, never a panic
