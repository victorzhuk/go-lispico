## Why

`bytecode-vm`'s `Compiled-chunk cache` requirement is explicit:

> A cache hit SHALL skip macro expansion and compilation.

Half of that held. `EvalCached` macro-expanded the form *before* consulting the
cache (`runtime/eval.go`), so a hit skipped compilation but re-expanded every
time. The cache key is built from the source hash, form index, dialect and macro
epoch — never from the expansion — so the expansion was computed and then
discarded on every hit.

It is not only wasted work; it is observable. A macro expander is ordinary Lisp
that runs at expansion time, so re-expanding re-runs it. Counting expander
invocations across four evaluations of one unchanged source:

| eval | before | after |
| --- | --- | --- |
| 1 | 1 | 1 |
| 2 | 2 | 1 |
| 3 | 3 | 1 |
| 4 | 4 | 1 |

Once per evaluation, where the requirement — and Common Lisp and Clojure, where
macroexpansion is compile-time — says once per compilation.

Deferring expansion to the miss branch improves **every** gold-set cell, all
deterministic at ±0%:

| cell | allocs/op before | after |
| --- | --- | --- |
| `twice-macro` | 68 | 57 (−16.2%) |
| `counter-closure` | 69 | 64 (−7.3%) |
| `rule-load` | 189 | 177 (−6.4%) |
| `guard-nil` | 36 | 34 (−5.6%) |
| geomean over 13 cells | 62.14 | 60.78 (−2.20%) |

Every cell moving in the same direction is the signature of removing per-eval
work from the hit path, rather than of a change that happens to suit one
fixture.

## What Changes

- `EvalCached` consults the chunk cache first and macro-expands only on a miss.
  Cancellation polling stays ahead of both, so a cancelled evaluation is still
  refused before any work.

No API change and no new identifier. The stale doc comment on `EvalCached`,
which described the old order, is corrected rather than left to mislead.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: `Compiled-chunk cache` already requires this, so the
  requirement text is unchanged. It gains a scenario stated in terms of an
  observable — how many times an expander body runs — because the existing
  reuse scenario is phrased around results and recompilation, and a result is
  identical whether or not the form was re-expanded to produce it. That is why
  this went unnoticed.

## Impact

- Code: `runtime/eval.go` only, plus one test.
- **Behavior change, deliberate**: a macro whose expander has side effects now
  runs those side effects once per compilation instead of once per evaluation.
  This is what the requirement asks for and what CL and Clojure do, but it is
  observable, so it is called out in the changelog rather than filed as a pure
  optimization. Code relying on an expander re-running per evaluation was
  relying on behavior the spec already forbade.
- Risk: charging now differs between a hit and a miss, since expansion is
  charged where it happens. That asymmetry already existed —
  `chargeCompiledChunk` only runs on a miss — and it is the honest direction:
  a hit genuinely does not allocate the expansion.
- Risk: a form whose expansion fails cannot reach the hit path, because a
  failing expansion never produced a chunk to cache. No error is silently
  skipped.
- Sequencing: independent of the remaining work to compile `defmacro` and
  `unquote-splicing`, which is what this stage was originally scoped as and
  which stays open.
