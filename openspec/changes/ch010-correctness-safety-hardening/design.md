# Design: Correctness and safety hardening

## Decisions of record

The load-bearing decisions are captured as ADRs and glossary entries, not restated
here:

- `docs/adr/0001-literal-evaluation-semantics.md` — literals evaluate their elements.
- `docs/adr/0002-bytecode-vm-disposition.md` — the VM is an opt-in subset optimizer; the on-disk cache is removed.
- `docs/adr/0003-concurrency-model.md` — one engine is safe for concurrent eval; per-evaluation state is not shared.
- `CONTEXT.md` — Evaluator / VM / Compiler / Special-form / Builtin / Macro / Sandbox / Literal vocabulary.

## Per-evaluation state (concurrency)

Today `macroDepth` (plain `int`), `callDepth`, and `loopDepth` (both
`atomic.Int64`) are fields on the shared `engine` struct, so depth limits and the
"recur outside loop" check are process-global across every goroutine using the
engine. The fix threads these three counters through the evaluation call path as a
small per-call value instead of reading engine fields.

Preferred approach: carry the counters in a per-call state value created at each
top-level `Eval`/`Apply` entry and passed down, leaving the `engine` struct
stateless with respect to a single evaluation. `Env` locking is unchanged — it
already covers binding access; this change covers only the counters that
environment locking does not.

Rejected alternative: a fresh `*engine` per `Eval` — it re-allocates the
special-form dispatch table on every call and still isolates nothing that matters.

## VM subset boundary (ADR-0002)

The compiler already self-documents `unquote-splicing` as unsupported, and
`defmacro` is routed around the compiler by a top-level check that breaks when
nested. Rather than complete parity now, the compiler returns a typed "unsupported
in bytecode" error for these forms and the bytecode `Eval` path falls back to the
tree-walker for that form, so no valid program fails or panics under
`WithBytecode()`. `throw` is aligned so the value bound by `catch` has the same
runtime type under both evaluators (the tree-walker's coercion is the reference).

## Cache removal (ADR-0002)

`BytecodeCache`, its gob registration, versioning, atomic-rename writer, and
dedicated tests are deleted, and `WithBytecodeCache` is removed. Nothing on the
runtime path referenced them. A future cache is reintroduced only when wired into
evaluation and benchmarked to beat the tree-walker end to end.

## Literal evaluation (ADR-0001)

`Vector` and `*HashMap` move out of the self-evaluating case in `core/eval.go`:
their elements are evaluated in order, producing a new immutable value. Quasiquote
gains the `HashMap` case so `` `{:k ~x} `` expands consistently with the vector
case. This is a breaking change, called out in the changelog and README.

## Sandbox (Q6 — resolved as a security boundary)

`plugins/lio/sandbox.go` resolves the target with `filepath.EvalSymlinks` (or its
parent, for a not-yet-existing write target) before both `withinRoot` and
`DenyPattern`, closing the intermediate-symlink escape. This makes the sandbox a
real boundary, matching the `CONTEXT.md` definition.
