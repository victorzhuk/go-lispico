# Design

## What the fix actually is

Not "make `nativeAdd` inlinable." Its cost of 174 is inherent to its contract — an N-ary
loop over `[]core.Value`, a type switch per element, int/float promotion, and a formatted
error. Shrinking that under 80 while keeping the contract is not plausible, and attempting
it would mean rewriting working code that handles every case correctly.

The fix is to *bypass* it for the shape that dominates. `execNativeFastFused`
(`core/vm/vm.go:1110`) already receives the operand slice directly off the VM stack:

```go
d := len(vm.stack) - argc
args := vm.stack[d:]
result, err := execNative(eval, op, args, env)
```

Two arguments, both `core.Int`, is the overwhelmingly common case in rule code — loop
counters, comparisons, accumulator arithmetic. For that shape the entire general machinery
is unnecessary: no loop, no `hasFloat` tracking, no error construction. A helper handling
exactly it is small enough to inline, so the win is a removed call layer plus the removed
per-element dispatch, not a smaller `nativeAdd`.

## Shape

One helper per fused operator, each doing the minimum:

```go
func addInt2(a, b core.Int) core.Value { return core.BoxInt(a.V + b.V) }
func ltInt2(a, b core.Int) core.Value  { return core.BoxBool(a.V < b.V) }
```

`core.BoxInt` (`core/types.go:108`) and `core.BoxBool` (`core/types.go:66`) already exist
and are already the allocation-avoiding path — the preboxed integer range is
`[-128, 1023]` and `Bool` is two singletons. The helpers must use them, not construct
`core.Int{V: ...}` directly, or they reintroduce boxing the codebase deliberately removed.

Gating in `execNativeFastFused`, ahead of the `execNative` call:

```go
if argc == 2 {
    if a, ok := args[0].(core.Int); ok {
        if b, ok := args[1].(core.Int); ok {
            if v, handled := nativeInt2(op, a, b); handled {
                // charge and replace stack exactly as today
            }
        }
    }
}
```

`nativeInt2` switches on `op` and returns `handled == false` for anything it does not
cover, so an operator without a fast path falls through unchanged. That keeps the gate in
one place rather than scattered per opcode, and makes "which operators have a fast path" a
single readable list.

## Semantics that must not drift

- **`int64` wraparound.** Go wraps on overflow and `nativeAdd` does not special-case it.
  `addInt2` must wrap identically. "Fixing" overflow here would be an observable behavior
  change smuggled into a performance change.
- **Division.** `nativeDiv` is furthest over budget (513) and carries the most semantics:
  division by zero, and whether int/int division yields `Int` or `Float`. Read its current
  behavior and mirror it exactly, or leave it out. It is also the least valuable — division
  is rare in rule code next to addition and comparison. Do it last, separately, and drop it
  if mirroring is not obviously correct.
- **Charging.** `MeterScalarBytes` is charged once per fused op today
  (`core/vm/vm.go:1122`). The fast path charges identically. A cheaper result must not
  become a cheaper *charge* — the ledger is a contract, not a cost model.
- **Comparison chains.** `nativeOrder` takes a name and a predicate and handles N-ary
  ordering (`(< a b c)`). The fast path covers `argc == 2` only; N-ary comparison keeps
  falling through.

## Verification

Inlining is a compile-time property and the profile cannot show it, so it is checked
directly:

```
go build -gcflags='-m' ./core/vm/ 2>&1 | rg 'can inline .*Int2'
```

Plain `-m` prints positive decisions; `-m -m` is needed for costs and reasons. Both are
worth recording — the positive confirms the helpers inline, the second confirms the general
functions still do not, which is expected and fine.

Correctness rests on a differential test, not spot checks: for each operator, assert the
fast path and the general path agree over a wide `int64` range including
`math.MaxInt64`/`math.MinInt64` and values straddling the preboxed `[-128, 1023]`
boundary, since that is where `BoxInt` changes behavior.

Benchmarks: the win should appear on `Fibonacci_VM`, `ArithmeticLoop_VM`,
`SimpleArithmetic_VM`, and the `loop-sum` gold-set cell. Before measuring, confirm an
existing benchmark exercises the *mixed-type* and N-ary paths; if none does, add one
first, so a regression there cannot hide behind an improvement in the int path.

## Rejected alternatives

- **Rewriting `nativeAdd` to be inlinable.** Cost 174 is the N-ary contract, not
  carelessness. A rewrite risks the mixed-type and error paths for no gain the fast path
  does not already provide.
- **A fast path for mixed int/float.** Doubles the surface for a case that is rare in rule
  code and already correct. Revisit only if a profile shows float arithmetic hot.
- **Widening the preboxed integer range instead.** Addresses allocation, not dispatch, and
  is a separate question with its own memory trade-off.
- **`//go:noinline` experiments or hand-written assembly.** The prior dispatch-loop study
  already found that the mere presence of a call perturbs register allocation for the whole
  function; chasing that further is not warranted by a fast path that avoids the call
  outright.
