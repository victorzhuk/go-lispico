## Context

See `proposal.md` for the reproduced failures at commit `3bcf9c1`. `Compiler.locals` supplies lexical slot indices, while `VM.stack` also holds call heads, arguments and collection elements. `OpSetLocal` copies the top operand into a frame-relative slot without removing it; `compileLet` restores compiler locals without reclaiming operands. `compileLoop` does not restore compiler locals at all.

The current execution requirement already promises one result per expression. The older `Kernel let binding scope parity` requirement contradicts both current evaluators and the archived `align-clojure-dialect-surface` change: kernel `let` is sequential. Loop initializers remain evaluated in the enclosing scope by `evalLoop`.

## Goals / Non-Goals

**Goals:** A single frame layout for top-level execution, closure application and variadics; balanced operands across bindings, branches and loop back edges; unchanged flat-capture semantics.

**Non-Goals:** Native support for scoped definitions, error-routing changes, automatic tail-call optimization, new environment mirroring, or changing kernel binding semantics.

## Decisions

1. Reserve the frame's local region before expression evaluation, with temporary operands above it. Use existing `Chunk.Locals` and `Frame.base` as the layout inputs; local loads/stores must never address call heads or prior arguments. Apply the same rule at `Run`, `apply` and `call`, including rest arguments and captured parameters. Keeping locals at whatever operand height happens to exist cannot satisfy nested expression composition.
2. Binding stores consume their initializer operand; mutation stores preserve their expression result. Update `emitBind`, capture rewriting, stack analysis and validation together. Reserve storage once per frame instead of allocating an `Env` for uncaptured locals. Keep captured locals in existing `cellBox` storage and preserve descriptor addressing through `Caps`.
3. Track operand height through compilation and control transfer. Every normally completed expression adds one result; `recur` restores the loop-entry operand height after evaluating all replacement values and before jumping. Bindings inside a loop cannot accumulate discarded operands. Preserve simultaneous replacement of recur arguments and fresh cells for captured loop slots.
4. Treat `loop` as a lexical scope. Compile initializers before making new loop bindings visible; restore the previous local layout when compilation leaves the body. Keep `let` and `let*` sequential. The spec corrections remove obsolete wording rather than changing current tree-walker behavior.
5. Keep native fusion and head freezing intact. Local operands still address local slots; canonical operator calls still avoid placing their head on the operand stack. A binding expression used as an argument must not change the frozen callee or prior arguments.

## Risks / Trade-offs

- Frame offsets affect captures, variadics and pooled application → pin explicit expected values at compiler/VM and public runtime seams, then run existing capture, fusion and re-entry suites.
- Loop cleanup can discard replacement values or reuse an old captured cell → test swaps, nested loops and per-iteration closures before changing emission.
- Instruction and stack metadata changes can alter reductions and allocations → retain existing deterministic charge rules and run the existing gold-set correctness checks; do not weaken limits or performance thresholds.
- Reserving locals from `Chunk.Locals` can reserve more than peak live bindings → correctness first; avoid a separate slot-allocation optimization in this change.

## Migration Plan

Add failing regressions, repair frame layout and scope bookkeeping, run bounded correctness/race checks, then update existing architecture and changelog text. Runtime caches are in-process; no persistent bytecode migration is required. `WithTreeWalker()` remains the rollback path. Implement and archive this change before its scoped-definition and error-routing successors.
