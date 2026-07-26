## Why

`bytecode-vm`'s `Compiled-chunk cache` requirement already states the property
this change restores:

> **Repeated evaluation reuses the chunk** — WHEN the same source is evaluated
> twice on one Engine under the VM, THEN the second evaluation SHALL not
> recompile and SHALL return the same result.

Any source containing a `defmacro` violates it. `evalDefmacro` calls
`env.BumpMacroEpoch()` unconditionally (`core/eval.go:903`), the epoch is part of
the chunk cache key (`runtime/eval.go:358`), and a changed epoch flushes the cache
(`runtime/eval.go:366-368`). So re-evaluating an unchanged source recompiles every
form in it, every time. Measured on the `twice-macro` fixture, four identical
evaluations on one engine:

```
eval#1 epoch=…:1    eval#3 epoch=…:3
eval#2 epoch=…:2    eval#4 epoch=…:4
```

The requirement's own wording already scopes the invalidation more narrowly than
the code does — "defining or redefining a macro SHALL invalidate **affected**
entries". Re-binding a macro to an identical definition affects nothing: the
expansion every cached chunk embedded is the expansion the new binding produces.

The cost is not marginal. Skipping the bump when the rebind is identical, measured
on the gold set's `twice-macro` cell under the VM at `-benchtime=400ms -count=10`:

| metric | before | after | delta |
| --- | --- | --- | --- |
| allocs/op | 103 | 68 | **−33.98%** |
| B/op | 7.967 Ki | 5.864 Ki | −26.40% |
| sec/op | 9.262µs | 7.194µs | −22.33% |

`allocs/op` and `B/op` are deterministic at ±0%, so those are the load-bearing
numbers; the latency figure agrees with them but is the weaker signal.

The staleness guard this touches was introduced deliberately —
`2026-07-10-vm-first-staged` design note: "epoch bump hooks the single `defmacro`
path" — as a conservative correctness measure, not as a judgement that identical
rebinds must invalidate. That guard is preserved: anything that actually differs
still bumps.

This is the smaller half of the planned "compile the two tree-walker fallback
forms" work, separated because measurement said it carries most of the value at a
fraction of the risk. Compiling `defmacro` and `unquote-splicing` — which needs a
new opcode and mid-compile macro expansion — remains open and unaffected.

## What Changes

- `evalDefmacro` bumps the macro epoch only when the new binding differs from the
  macro already bound to that name. Identical name, defining environment,
  parameters, variadic tail, and body means no bump.
- Everything else is untouched: a genuine redefinition still invalidates, the
  epoch still keys the cache, and the flush path is unchanged.

No API change, no new exported identifier, no behavioral change a program can
observe other than not recompiling what it already compiled.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: `Compiled-chunk cache` says redefining a macro invalidates
  "affected" entries but never says what makes an entry affected, which left room
  for an implementation that invalidates everything on any `defmacro`. It gains an
  explicit statement that an identical rebind is not a redefinition for cache
  purposes, plus a scenario pinning the case its existing reuse scenario silently
  excluded.

## Impact

- Code: `core/eval.go` only, plus tests.
- Risk: **an unsound equality test would serve a stale expansion**, which is the
  one failure mode that matters here — a program would silently run an outdated
  macro. Two controls. The comparison requires pointer equality of the defining
  `*Env`, so a macro closing over different bindings never compares equal. And
  `Value.Equals` is depth-bounded (`boundedEquals`, `core/depth.go:165-168`) and
  returns **false** past the limit, so a body too deep to compare falls through to
  today's unconditional bump rather than to a false match. It fails closed.
- Risk: the comparison must consult the cell the active dialect binds through —
  the function cell under Lisp-2, the value cell under Lisp-1. A first attempt
  checked only `GetFunc` and was a silent no-op under the gold set's Clojure
  dialect: the benchmark showed a flat result, which read as "this fix is
  worthless" rather than "this fix never ran". Caught by watching the epoch
  counter instead of trusting the benchmark. A test covers both dialects.
- Risk: comparing bodies costs an O(body size) walk on every `defmacro`. That is
  definition-time, not call-time, and it replaces a full cache flush plus
  recompilation of every cached form. Not a hot path.
- Sequencing: independent of the remaining fallback-compilation work; either can
  land first.
