---
status: accepted
---

# Reduction and allocation metering use a fixed deterministic ledger

Per-evaluation metering now complements ADR 0007's structural-depth, reader-depth, collection-length, and cache-entry ceilings. Every evaluation carries two more hard ceilings: reductions and cumulative allocation bytes. The ledger is deterministic by construction: the same source under the same engine configuration charges the same units regardless of Go version, allocator behavior, or host architecture.

## Reduction model

Reductions are evaluator-local work units, not wall-clock time and not cross-evaluator comparable counters.

- Tree-walker: one reduction per form dispatch, plus one per apply-trampoline `GoFunc` dispatch.
- Macro expansion: one reduction per expansion step.
- Bytecode VM: one reduction per decoded instruction, plus one per `GoFunc` dispatch.
- Compiler: one reduction per emitted instruction.

The hot loops do not increment a shared atomic on every step. Both evaluators already keep a 128-step cancellation budget, so metering piggybacks that countdown and flushes consumed work at the existing sync points. This keeps the context-observation bound comfortably inside the required 1,024-reduction window while avoiding a new per-step branch or atomic write.

## Allocation model

Allocation charging is shallow and deterministic. It counts the produced value's own container cost, not a recursive deep walk of already-existing children. Values built incrementally in Lisp are therefore charged incrementally at each construction site; values materialized inside a Go builtin are charged once by their shallow result size.

### Fixed size table

| Unit | Charge |
| --- | ---: |
| Scalar value (`nil`, `bool`, `int`, `float`) | 16 bytes |
| String / symbol / keyword header | 16 bytes |
| String / symbol / keyword payload | `len(utf8 bytes)` |
| List header | 24 bytes |
| Vector header | 24 bytes |
| Collection element slot | 16 bytes per element |
| Hash map header | 32 bytes |
| Hash map entry | 64 bytes per key/value pair |
| Closure header | 64 bytes |
| Closure capture slot | 8 bytes per capture |
| Bytecode instruction | 4 bytes |
| Reader node | 32 bytes per parsed node |
| Reader byte payload | `len(source bytes copied into values)` |

### Why these values are conservative

- `16` bytes per value slot matches the project baseline that a boxed Lisp value occupies one interface slot on supported 64-bit targets; keeping it fixed avoids platform drift.
- `24` bytes for list/vector headers corresponds to one slice header; element storage is charged separately through slots.
- `64` bytes per hash-map pair intentionally over-counts both the small sorted form and the promoted Go-map form. The exact in-memory shape differs by path; the ledger must stay simple and fail closed.
- `64 + 8*caps` for closures over-counts small closures a little, but it captures the closure object plus capture-array growth without consulting runtime layout.
- `32` bytes per reader node intentionally prices parse-tree shape higher than the minimum object footprint. Reader metering must reject wide flat literals before evaluation starts, even though the exact token/value mix varies.

## Determinism requirement

The ledger MUST NOT depend on `unsafe.Sizeof`, allocator classes, pointer width, map bucket layout, or any other runtime-specific measurement. Those values vary across architectures and Go releases; a metering ceiling tied to them would make the same source pass on one host and fail on another. The published table is therefore normative even when the real heap footprint is smaller.

## Charge sites

The fixed table is applied only at evaluator-owned construction boundaries:

- reader output, charged immediately after `Read` and before the first form runs;
- tree-walker collection literals and quasiquote construction;
- VM `OpMakeList`, `OpMakeVector`, `OpMakeMap`, and `OpClosure`;
- compiler-emitted bytecode and constant pools, charged before a compiled chunk is cached;
- shallow `GoFunc` results at the centralized apply sites.

This keeps the meter complete without trying to instrument every composite literal or every Go allocation in the process.
