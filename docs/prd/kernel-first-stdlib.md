# Kernel-first stdlib completeness — PRD

## Problem statement

The one real embedding consumer loads only the pure-computation plugins (stdlib,
data) and writes rule and policy code against the kernel. The first real policy
could not express `(< budget 500)`: comparison and equality builtins do not
exist anywhere outside test fixtures. Rule authors also lack the collection
basics they reach for next — key-presence checks, map merging, key removal,
sorting, numeric ranges. Meanwhile the README advertises eight plugins as live,
evolving surface, inviting embedders to build on llm or agent when those
plugins have no consumer and no future feature work.

## Solution

Complete the pure-computation stdlib so policy code works out of the box:
comparison and equality builtins first (`=`, `<`, `>`, `<=`, `>=`), then the
collection basics (`contains?`, `merge`, `dissoc`, `sort`, `range`). Reposition
the README as kernel-first: the world-touching plugins (llm, agent, lio, net,
exec) are marked frozen — security and correctness fixes only — so nobody
builds on them expecting evolution. No core changes; all new capability arrives
as stdlib Builtins per the "Builtins never in core" invariant.

## User stories

1. As a rule author, I want `(= a b)`, so that policies can branch on equality
   of any two values.
2. As a rule author, I want `(< budget 500)` (and `>`, `<=`, `>=`), so that
   numeric policy thresholds are expressible.
3. As a rule author, I want comparisons to accept mixed Int/Float arguments the
   way arithmetic already does, so that `(< 1 1.5)` works without manual casts.
4. As a rule author, I want `(contains? m :key)`, so that policies can check
   key presence in a map without conflating "absent" with "nil value".
5. As a rule author, I want `(merge base overrides)`, so that layered policy
   and config maps combine with right-most precedence.
6. As a rule author, I want `(dissoc m :key)`, so that policies can strip keys
   from a map without mutation.
7. As a rule author, I want `(sort coll)`, so that rule output is
   deterministically ordered.
8. As a rule author, I want `(range n)` / `(range start end)` /
   `(range start end step)`, so that numeric sequences feed `map`, `filter`,
   and `reduce`.
9. As a rule author, I want a wrong-typed argument to return a graceful error
   naming the builtin, so that a bad policy fails with a message, never a
   panic.
10. As an embedder, I want to evaluate policies concurrently on one Engine (per
    ADR 0003), so that per-request evaluation needs no engine pool.
11. As an embedder, I want the README to state which plugins are frozen, so
    that I don't build on llm or agent expecting new features.
12. As an embedder, I want loading only stdlib and data to keep every
    world-touching surface behind my host's own gated primitives, so that no
    plugin bypasses my permission layer.

## Implementation decisions

- All new functions are Builtins registered by the stdlib Plugin (bare names,
  no namespace prefix, matching `+` and `count`). Core, compiler, and VM are
  untouched — Builtins are GoFunc values in the Env, already callable from both
  the Evaluator and the VM.
- `=` delegates to `Value.Equals`, which is already total across the 13 types.
  Equality is strict-type: `(= 1 1.0)` is false, consistent with the glossary
  ("immutability holds at the Equals level") and Clojure precedent.
- `<`, `>`, `<=`, `>=` are numeric-only and variadic with chained semantics
  (`(< 1 2 3)` → true), using the same Int/Float promotion as arithmetic.
  Non-numeric arguments error in the existing style: `<: expected number, got %T`.
- `contains?` checks key presence in a HashMap via the existing `Get` found
  flag. Behavior on Vector is an open question (see Further notes) — first
  slice ships map-only, erroring on other types.
- `merge` folds `HashMap.Assoc` left-to-right across map arguments; right-most
  key wins; zero args yield an empty map. `dissoc` wraps the existing
  `HashMap.Dissoc`, accepting one or more keys.
- `sort` returns a List in ascending order; numbers order numerically (with
  Int/Float promotion), strings lexicographically; mixed or unorderable
  element types error. No comparator-function arity in this slice.
- `range` is eager (the language has no laziness) and returns a List of Int;
  step 0 errors; the Clojure three arities apply.
- Error convention unchanged: `fmt.Errorf("name: ...")`, errors returned, never
  panics.
- README repositioning: the plugin table marks llm, agent, lio, net, exec as
  frozen (security and correctness fixes only); fsm is noted as pure
  computation, unfrozen but without a consumer; the intro leads with the
  embeddable-kernel framing already present. CLAUDE.md plugin section gets the
  same freeze markers.
- The concurrency contract (concurrent Eval on one Engine, ADR 0003) is already
  implemented via per-evaluation state; this work adds no shared evaluator
  state and must not regress it.
- Frozen plugins receive zero code changes in this work.

## Testing decisions

- Seam: the existing stdlib table-test seam — `core.NewEnv(nil)` +
  `Plugin.Init` + `core.NewEvaluator().Eval` over source strings, asserting via
  `Value.Equals`. Prior art: `plugins/stdlib/stdlib_test.go` (`setupEnv`,
  `eval`, `evalErr` helpers, table-driven per builtin).
- Depth: existing-service mode — each new builtin gets a table covering the
  happy path, arity errors, type errors, and edges (empty collection, single
  argument, mixed Int/Float, equal elements in `sort`, negative `range` step).
- The ADR's motivating case `(< budget 500)` appears as a test row.
- Good coverage includes chained-comparison rows (`(< 1 2 3)`,
  `(< 1 3 2)` → false) and `merge` precedence rows.
- No new seams; no Engine-level tests required for this slice (the runtime
  already smoke-tests plugin loading).

## Out of scope

- Any new feature in llm, agent, lio, net, exec — frozen by ADR 0004.
- Deleting or extracting the frozen plugins (a separate later decision).
- fsm investment — it earns work only when a consumer appears.
- Compiler/VM changes, new special forms, new opcodes.
- Lazy sequences; `sort` comparator-function arity; ordering across mixed
  types.
- Numeric-equality-across-types operator (`==`); `not=`.
- Further stdlib waves (`take`, `drop`, `some`, `every?`, `update`, `get-in`
  extensions) — they follow the same pattern once rule authors pull on them.

## Further notes

- Seam confirmation is pending: the existing stdlib table-test seam was chosen
  as the default; an optional thin smoke test through `runtime.New` +
  `eng.Use(stdlib.New())` + `eng.Eval` remains available if embedder-path
  coverage is wanted.
- Open question: `contains?` on Vector — Clojure checks index membership,
  which surprises users expecting value membership. Decide before extending
  beyond maps.
- Open question: whether `(= 1 1.0)` → false is acceptable for policy authors
  long-term, or whether a numeric `==` is wanted later. Strict-type `=` ships
  first either way.
- Open question: `sort` on Bool/Keyword/nil — proposed to error alongside all
  non-number, non-string types; confirm when a real policy needs it.
- Core and VM test fixtures register ad-hoc `=` GoFuncs
  (`core/integration_test.go`, `core/vm/*_test.go`); they stay as-is because
  core cannot import plugins.
- CHANGELOG: the new builtins and the README freeze markers are user-facing —
  record under `[Unreleased]` Added/Changed on implementation.
