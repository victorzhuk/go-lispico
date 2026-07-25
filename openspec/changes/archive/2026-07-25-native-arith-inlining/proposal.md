## Why

`nativeAdd` and its siblings are the innermost functions in the bytecode VM. Every
unshadowed `(+ a b)`, `(< i n)`, or `(= x y)` reaches them through the fused
`OpFreezeNative` + `OpAdd` pair, bypassing GoFunc dispatch entirely — that fusion is
already in place and works. What is not in place is inlining: they exceed Go's inline
budget by a wide margin and always have.

Measured directly with `go build -gcflags='-m -m' ./core/vm/`:

```
core/vm/vm.go:1249  nativeAdd    cost 174 exceeds budget 80
core/vm/vm.go:1277  nativeSub    cost 418 exceeds budget 80
core/vm/vm.go:1323  nativeMul    cost 176 exceeds budget 80
core/vm/vm.go:1351  nativeDiv    cost 513 exceeds budget 80
core/vm/vm.go:1385  nativeOrder  cost 307 exceeds budget 80
core/vm/vm.go:1404  nativeEq     cost 185 exceeds budget 80
```

The cause is structural, not incidental: each is written as an N-ary loop over
`[]core.Value` with a type switch per element, mixed int/float promotion tracked in a
`hasFloat` flag, and an `fmt.Errorf` error path. All of that is necessary for the general
case and none of it is needed for the overwhelmingly common one — two arguments, both
`Int`.

This was first recorded in the 2026-07-18 dispatch-loop study and never acted on. The
profiling baseline captured in `docs/profiling-baseline.md` re-confirms the costs are
unchanged in kind, `nativeAdd` having drifted from 156 to 174. Unlike the other candidate
optimizations examined alongside it, this one is justified by a compiler diagnostic rather
than an inference from a profile.

## What Changes

- A two-argument, both-`Int` fast path for each fused operator, small enough to inline,
  checked ahead of the existing general implementation.
- `execNativeFastFused` gates on that shape before falling through to `execNative`, so
  the mixed-type, N-ary, and error paths are reached exactly as they are today.
- Inlining is verified with `go build -gcflags='-m'` rather than assumed, and the check is
  recorded so a future change that pushes a fast path back over budget is detectable.

Purely additive. No existing function is rewritten, no opcode changes, no behavior
changes.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: `Native arithmetic and comparison opcodes` already requires semantics
  identical to the stdlib builtins. Introducing a second internal implementation of the
  same operation creates a contract that did not need stating while there was only one —
  that a specialized path is indistinguishable from the general path in result, error, and
  charged allocation, and that integer overflow wraps identically on both, since the
  general path relies on Go's wrapping arithmetic. A scenario pins it.

The observable contract is otherwise unchanged: same results, same errors, same
`ResourceLimitError` behavior, same charging.

## Impact

- Code: `core/vm/vm.go` only.
- Risk: divergence between the fast path and the general path. Go's `int64` wraps on
  overflow and `nativeAdd` does not special-case it, so a fast path must wrap identically
  rather than "improve" on it. The control is a differential test asserting the two agree
  across a wide `int64` range including the overflow boundaries, not a spot check.
- Risk: `nativeDiv` at cost 513 is the furthest over budget and also the one with genuine
  semantics to preserve — division by zero, and int/float result typing. It is the most
  likely to be got wrong and the least likely to matter, since division is rarer than
  addition and comparison in rule code. Treat it last and separately.
- Risk: adding a branch ahead of the general path could cost the mixed-type case. The
  control is that the existing benchmarks include float and N-ary shapes; if none does,
  one is added before the change rather than after.
- Sequencing: first optimization stage after the profiling harness, chosen because it is
  the only candidate whose evidence is a direct compiler measurement.
