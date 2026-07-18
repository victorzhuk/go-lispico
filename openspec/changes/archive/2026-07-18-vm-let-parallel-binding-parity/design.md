# Design

## Context

`compileLet` and `compileLetStar` (`core/compiler/compiler.go`) are byte-identical: both interleave init compilation with local registration — `Compile(init)`, `addLocal(sym)`, `Emit(OpSetLocal, len(c.locals)-1)` per pair. Because each init is compiled after the previous binding's `addLocal`, `resolveLocal` inside a later init sees the earlier bindings — sequential scope. The tree-walker evaluates all `let` inits in the parent env (`core/eval_test.go:248` pins it), so the two modes disagree on shadowing forms.

Locals live on the value stack: each compiled init pushes its value into what becomes its slot, and `OpSetLocal` copies top-of-stack into the slot (a no-op copy for the just-pushed value) plus mirrors captured locals into the frame env. The emitted instruction stream is therefore already correct for parallel semantics — the defect is purely compile-time name resolution.

## Goals / Non-Goals

**Goals:**
- Kernel `let` init expressions resolve in the enclosing scope under the VM, matching the tree-walker.
- `let*` sequential behavior unchanged and pinned.

**Non-Goals:**
- No bytecode format or VM opcode changes.
- No dialect-layer changes — which surface name maps to kernel `let` vs `let*` is dialect-owned and already settled.

## Decisions

- **Two-phase `compileLet`, identical bytecode.** Phase one walks the binding pairs, validates each name is a symbol, compiles each init, and emits `OpSetLocal` with the binding's precomputed slot index (`base + i`, `base := len(c.locals)` before the loop) — without registering locals. Phase two runs `addLocal` for every name. Symbol resolution during phase one cannot see the new bindings, so inits resolve in the enclosing scope; the instruction stream is unchanged from today.
  - Alternative — compile all inits first, then register and sync slots with new bytecode (`OpBindLocal` or `OpGetLocal`/`OpSetLocal`/`OpPop` triples): rejected, adds opcodes or per-binding instruction overhead in hot paths for a problem that needs neither.
- **Symbol validation stays in phase one** so a non-symbol binding name still fails before any init side effects are encoded, preserving current error ordering.
- **Duplicate names in one vector** (`[a 1 a 2]`): both inits see the outer scope; `resolveLocal` scans back-to-front so the body sees the last slot — same observable result as the tree-walker's double define.

## Risks / Trade-offs

- [Chunk cache holds stale sequential-`let` compilations] → cache keys on source; any form that hits the changed path was producing spec-violating results already, and recompilation yields the corrected chunk. No format change, no cache invalidation needed.
- [Existing code depending on sequential kernel `let` under the VM] → that dependency was a divergence from the documented contract; `let*` is the supported sequential form.
