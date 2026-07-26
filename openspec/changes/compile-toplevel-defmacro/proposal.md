## Why

The bytecode compiler refused every `defmacro`, so any source defining a macro
had that form evaluated by the tree-walker and never cached as a chunk.

The refusal was broader than the reason for it. Macro expansion is a pre-pass
over a whole form, so a macro defined *inside* a form is not yet bound when a
sibling use in that same form is expanded — `(do (defmacro id [x] x) (id 42))`
would compile the use as a plain call and fail at run time with
`TypeError: expected callable, got core.Macro`. That hazard is real, and it is
confined to the nested case. A `defmacro` that is the whole form has no sibling
that could get it wrong.

`correct-fallback-scope` (earlier today) recorded the compiler's actual
behaviour — "any `defmacro`, wherever it appears" — replacing wording that said
"nested in a body". That wording was describing the intended design, and the
implementation was over-rejecting; the correction was accurate about the code
and wrong about the intent. This change makes the code match the design, and
the documentation returns to describing the nested case. Both moves are in the
history on purpose: the first recorded what was true, this one changes what is
true.

Measured on the gold set under the VM at `-benchtime=400ms -count=10`:
`twice-macro` **57 → 50 allocs/op (−12.28%, p=0.000)**, every other cell
unchanged at `p=1.000`.

That is smaller than the earlier probe suggested, and the earlier reading was
wrong rather than the result disappointing. That probe compared a source with a
`defmacro` against one without it, so its 29-allocation gap was the cost of
*having the form at all* — expansion, cache lookup, execution — not the cost of
the fallback. Only the fallback part is recoverable here.

## What Changes

- `core.BindMacro` is extracted from `evalDefmacro`: it binds through the cell
  the dialect owns and bumps the macro epoch unless the rebind is identical.
  Both evaluators now go through it.
- `OpDefMacro` and `OpDefMacroFunc` build a macro from a prototype constant,
  fill in the defining environment at run time, and bind through
  `core.BindMacro`. Two opcodes, not one, because the VM carries no dialect —
  the compiler picks the cell, exactly as with `OpSetGlobal`/`OpSetFunc`.
  Appended to the opcode block so no existing opcode value shifts.
- `Chunk.Validate` gains cases for both: constant index in range, and the
  constant is a `Macro`.
- `compileDefmacro` emits them when the `defmacro` is the whole form, and
  returns the typed unsupported error when nested.
- Documentation returns to naming the nested case, in the six places
  `correct-fallback-scope` touched.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: `Bytecode VM execution`'s fallback scenario names the trigger.
  It gains the accurate boundary plus two scenarios — one that a top-level
  `defmacro` compiles and binds through the same path as the tree-walker, one
  that a nested `defmacro` defers — so the boundary is pinned on both sides
  rather than asserted on one.

## Impact

- Code: `core/eval.go`, `core/vm/opcode.go`, `core/vm/chunk.go`,
  `core/vm/vm.go`, `core/compiler/compiler.go`, plus tests and docs.
- **Risk, and the one that actually bit: the epoch.** A compiled `defmacro`
  that bumped the macro epoch unconditionally would invalidate the chunk cache
  on every evaluation and evict the very chunk it was compiled into — the fix
  would defeat itself. This is why the bind path is shared rather than
  reimplemented: `core.BindMacro` carries the identical-rebind check from
  `idempotent-macro-rebind`, so the VM inherits it instead of having to
  remember it.
- Risk: an opcode with a constant operand and no `Validate` case panics on an
  already-validated chunk. This repo has shipped that twice. Both opcodes got
  their `Validate` cases in the same commit as their definitions, and the case
  asserts the constant's *type*, not merely its index.
- Risk: the prototype constant carries `Env: nil`, and only the run knows the
  defining scope. If a future change made the VM reuse the prototype rather
  than copy it, macros would share an environment. The exec path copies by
  value before assigning `Env`; `Macro` is a value type, which makes that safe
  by construction rather than by discipline.
- Not in scope: `unquote-splicing`, now the sole remaining fallback trigger.
  Compiling it needs a splice opcode interacting with `OpMakeList` and the
  `OpStructEnter`/`OpStructLeave` depth tracking, which is independent of this.
