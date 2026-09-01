## Context

See `proposal.md` for motivation. `get` is a Go stdlib builtin backed by
`HashMap.Get`, whose `(value, found)` result can distinguish an absent key from a
stored `nil`. `get-in` is currently a two-argument Lisp bootstrap function built
as `reduce` over `get`; that composition loses presence information and cannot
implement a collision-free three-argument default.

The existing reduction performs Evaluator work per key. Replacing it with one
Builtin dispatch must preserve cancellation, the Engine-owned Evaluation
deadline, and reduction-budget enforcement under the contract introduced by
`builtin-resource-accounting`.
Large persistent `List` paths also cannot be walked with indexed `At(i)` calls,
which become quadratic, or flattened into an unmetered temporary slice.

Bootstrap definitions can be eagerly evaluated or published as lazy source
templates. Both execution paths eventually call the same registered Builtin, so
one Builtin lookup implementation is sufficient for Evaluator/VM parity.
`stdlib-bootstrap-evaluator-ownership` first makes the remaining source
definitions use the environment-owned bootstrap evaluator.

## Goals / Non-Goals

**Goals:**

- Preserve key presence through every nested lookup step.
- Keep list traversal linear, vector traversal bounded by trie depth, and both
  paths free of a path-sized copy, cancellable, and metered.
- Keep eager and lazy stdlib startup behavior correct after `get-in` leaves the
  bootstrap source set.
- Retain the typed error policy for non-map subjects and invalid key paths.

**Non-Goals:**

- Generalize `get` to vectors, strings, arrays, sets, or arbitrary lookup types.
- Change keyword-as-function behavior.
- Change nil handling for map mutation and inspection operations such as
  `assoc`, `dissoc`, `keys`, `vals`, `contains?`, or `merge`.
- Make `nil` equal to an empty map or list.

## Decisions

### Implement `get-in` as a shared stdlib Builtin

Register `get-in` beside `get` and remove its Lisp bootstrap entry. The builtin
accepts two or three arguments and walks a list/vector key path in order without
recursively evaluating forms. A `nil` path produces zero keys.

At each step, a map lookup uses `HashMap.Get` directly. `found == false` returns
the optional default (or `nil`); `found == true` carries the value onward even if
it is `nil`. On the next iteration, a carried `nil` means the remaining path is
missing. A non-map value with keys remaining returns the same subject type error
as `get`. With zero keys, the original subject is returned unchanged.

Alternatives rejected:

- A private keyword or other Lisp-level sentinel can collide with user data.
- Composing `get` and `contains?` duplicates traversal policy in bootstrap Lisp,
  still requires extra nil handling, and cannot reuse the map's single
  `(value, found)` lookup.
- Adding a public sentinel value expands the runtime data model for an internal
implementation concern.

The callable representation change is accepted for this alpha release: `get-in`
prints and compares as a Builtin instead of a Lambda. Under a full-base Dialect
it remains registered normally; under an empty-base Dialect it is removed unless
the vocabulary explicitly allowlists it. A public Lisp wrapper was rejected
because it would retain the bootstrap/cache lifecycle and add another call solely
to preserve representation behavior that the proposal explicitly marks breaking.

### Walk paths without flattening and preserve evaluation limits

Use a representation-aware cursor: advance a `List` with `At(0)` plus `Rest()`,
and a `Vector` by index. The list path is O(k); vector indexing performs bounded
persistent-trie traversal per key under the collection-length cap. Neither path
allocates a full-path copy; in particular, never call `At(i)` repeatedly on a
shared-tail `List`.

Create one `core.BuiltinWorkBudget` from the active call context. Before each map
lookup, record one local step; synchronize at the shared 128-unit boundary and
flush the remainder before every success, missing-path short circuit, validation
error, or callback/Terminal return. Do not separately inspect `ctx.Err()`, call
`PollEvalState` per key, or directly charge the ledger. Returning on a missing
key stops cursor work at the semantic short-circuit point, then flushes work
already completed.

Alternative rejected: `ToSlice()` is simple but allocates another path-sized
slice outside result accounting and needlessly increases peak memory.

### Mark lookup outputs as borrowed results

Stored map values, the caller-supplied default, `nil`, and the original subject
for an empty path are not allocated by lookup. Immediately before each successful
return, mark zero result bytes through the Builtin result-accounting helper from
`builtin-resource-accounting`; otherwise the centralized apply site would charge
the returned container's shallow size as though lookup created it.

### Keep `get` deliberately map-only

The nil arm is an empty-map boundary case, not a general Clojure `ILookup`
implementation. Map values retain presence-aware defaults; all other non-`nil`
subjects follow the current error path. This bounds the compatibility change and
avoids silently assigning index/key semantics to vectors and strings.

### Treat bootstrap removal as a startup-contract change

Remove only the `get-in` source entry. Update cache, lazy-template,
materialization-count, and bootstrap-name tests to reflect that `get-in` is
available immediately as a Go builtin and no longer materializes `reduce` and
`get` as transitive template dependencies. The process-level artifact cache is
temporarily left with no producer; `stdlib-bootstrap-cache-retirement`, blocked
by this change, removes it and its public contract. The remaining macros continue
through the environment-owned bootstrap evaluator established by the prerequisite.

This change does not repeat the ownership requirement in its delta. Before
implementation or archive, `stdlib-bootstrap-evaluator-ownership` must already be
completed and synchronized into the canonical `stdlib-plugin` spec. That serial
promotion rule avoids two active deltas replacing the same canonical requirement
and prevents reverse archive order from restoring the old bootstrap name set.

### Test observable results, not engine parity alone

Direct stdlib tests pin arity, typed errors, path types, missing intermediates, stored terminal
`nil`, empty paths, and scalar errors. Runtime tests run the result matrix through
both the Evaluator and VM, under eager and lazy startup where applicable. Source
forms used with the default CL Dialect avoid disabled bracket literals. Behavior
goldens supplement equivalent-result checks; two execution paths returning the
same wrong value is not sufficient.

## Risks / Trade-offs

- [Risk] Removing a bootstrap definition invalidates tests and benchmarks that
  assert materialized-template counts → update those assertions and add a test
  that first use of `get-in` does not publish a source template.
- [Risk] A default can accidentally replace a stored terminal `nil` → use the
  map's `found` bit until the terminal step and cover both terminal and
  intermediate `nil` in tests.
- [Risk] Native `get-in` could drift from `get` error wording → share a small
  map-lookup helper or assert equivalent subject errors in direct tests.
- [Risk] A Builtin traversal bypasses the Engine deadline or Reduction budget →
  use the shared Builtin work budget once per visited key, flush every return,
  and test caller cancellation, an expired/late VM deadline, and a low budget
  independently.
- [Risk] Indexed traversal of a persistent list becomes quadratic → advance by
  `Rest()`, assert no path-copy allocations, and benchmark geometric shared-tail
  path sizes rather than relying on wall-clock correctness tests.
- [Risk] Returning an existing large collection consumes allocation budget →
  mark lookup outputs as borrowed and test tight budgets through both execution
  paths.
- [Risk] The Lambda-to-Builtin migration changes restricted-Dialect visibility →
  pin fail-closed empty-base behavior and full-base availability explicitly.

## Migration Plan

Land the Builtin and its tests in the same change that removes the
bootstrap entry, then update documentation and startup assertions. Remove the
premature README claim until that atomic milestone passes. No persisted data
migration is required. Rollback restores the bootstrap entry and removes the
Builtin registration; it also restores Lambda printing/equality and the previous
empty-base visibility. The public `get` nil behavior can be rolled back
independently if required.
