## Context

See `proposal.md` for motivation. Sequence extraction is currently repeated in
collection, higher-order, and string builtins, and each switch independently
decides whether `core.Nil` is valid. `cons` and list-`conj` also have persistent
extension paths with incremental allocation charging that must not be replaced by
a flatten-and-rebuild implementation.

ADR 0005 explicitly defers the data-model identity `nil == '()`. The desired
behavior is therefore an adapter at builtin argument boundaries, not a core
representation or equality change.

The prerequisite `stdlib-typed-error-compliance` runs after the lookup and CL
adapter changes, so this change consumes their final shared kernels, the resource
foundation, bootstrap ownership, and typed error classifications transitively.

## Goals / Non-Goals

**Goals:**

- Make nil acceptance auditable and consistent across sequence consumers.
- Reuse each operation's existing empty-list semantics, including output type,
  argument order, errors, metering, and function-invocation count.
- Preserve persistent sequence extension and incremental allocation charging.

**Non-Goals:**

- Implement cons cells, dotted pairs, lazy sequences, or `nil == '()`.
- Claim full ANSI Common Lisp or Clojure collection compatibility.
- Change nil semantics for map-only operations or accept arbitrary iterables.
- Change empty-list behavior that is independent of nil acceptance.

## Decisions

### Freeze a closed boundary matrix

Nil acceptance applies only to these positions:

| Operation | Nil position | Empty behavior |
| --- | --- | --- |
| `first`, `last`, `count`, `empty?`, `reverse`, canonical `sort` | argument 1 | existing empty-list behavior |
| `rest` | argument 1 | empty List |
| `concat` | every sequence argument | contributes zero elements |
| canonical `nth` | argument 1 | empty-list bounds/default behavior |
| `cons` | argument 2 | extend an empty List |
| `conj` | argument 1 | select List extension/order |
| canonical `map`, `filter` | argument 2 | empty List and no callback |
| `reduce` | final sequence argument in both arities | existing empty reduction behavior |
| `apply` | final expanded argument only | contributes zero tail arguments |
| `string/join` | argument 2 | empty String |

Dialect adapters normalize their own positions before entering shared kernels:
CL `nth` argument 2, every CL `mapcar` list argument after the function, and CL
`sort` argument 1 follow the adapter contract. No other stdlib operation or
argument position gains nil acceptance from this change.

### Normalize read-only sequence inputs through one internal adapter

Add a small unexported adapter that returns the ordered elements of a `List` or
`Vector`, returns zero elements for `Nil`, and reports “not a sequence” for every
other value. `reverse`, `map`, `filter`, `reduce`, `apply`, and
`string/join` use it while retaining their operation-specific arity and error
messages. Existing nil-aware observers/producers (`first`, `rest`, `last`,
`count`, `empty?`, `sort`, and `concat`) receive regression coverage against the
same contract.

The adapter normalizes only input. Each caller still constructs its current
empty result: for example, `map nil` returns an empty `List`, `reduce` without an
initializer returns `Nil`, and `string/join` returns an empty `String`.

Alternative rejected: changing `Nil` to implement `List` behavior would leak into
equality, printing, type predicates, truthiness, reader behavior, and every core
consumer, contradicting ADR 0005.

The adapter and shared kernels receive the active `core.Evaluator` argument from
the GoFunc. Collection-length and construction-depth helpers query
`core.CollectionLimiter` and `core.ConstructionDepthEvaluator` on that evaluator;
they do not query `env.Evaluator()`. A child lexical environment may not own an
evaluator even though its callback is executing under a limited Engine, so the
environment is not an authoritative source of dynamic execution policy.

### Preserve empty-list behavior exactly, including `nth`

Nil dispatch takes the same branch as a zero-element list. Consequently,
two-argument `nth` remains an out-of-bounds error and three-argument `nth` returns
its default. This is deliberately project-local empty-list equivalence rather
than Clojure's JVM-null special case. Add a direct `Nil` case to `nth`; do not
flatten a non-empty sequence through the shared adapter, because that would turn
an indexed lookup into a whole-collection copy.

### Route `cons` and `conj` through persistent empty-list extension

For `cons`, construct/select an empty list as the sequence base and use the
existing list extension path. For `conj`, select the existing list branch with an
empty base, preserving this project's current multi-argument order. Do not
convert nil through a generic slice builder: the list branch already enforces
incremental allocation charging and construction limits.

### Expand nil to zero tail arguments in `apply`

The last argument of `apply` remains the only expanded sequence. A nil last
argument contributes zero values; any explicit arguments before it are retained.
The target function remains responsible for validating whether that resulting
arity is legal.

### Pin results across engines and dialect names

Direct builtin tests provide the full operation matrix and explicit expected
values/errors. Runtime tests execute behavior goldens with both execution paths
and cover aliases such as CL `mapcar`/`length` through their adapters. Equivalent
results supplement, rather than replace, independent expected values.

## Risks / Trade-offs

- [Risk] A shared adapter homogenizes error text unintentionally → let callers
  format their existing operation-specific errors and add regression assertions.
- [Risk] A blanket scalar rule accidentally changes `empty?` from `false` to an
  error → characterize values as well as errors and keep a dedicated scalar
  `empty?` regression.
- [Risk] Nil extension bypasses allocation or depth accounting → enter the
  existing empty-list `cons`/`conj` path and retain resource-limit tests.
- [Risk] A nested callback's child environment loses Engine limits → thread the
  active evaluator through kernels/helpers and test low collection/depth limits
  inside a Lambda under both execution paths.
- [Risk] “Nil as empty input” is mistaken for value identity → document and test
  that `nil?`, `list?`, equality, printing, and truthiness remain unchanged.
- [Risk] Future sequence Builtins inherit behavior accidentally → keep this
  matrix closed and require a spec amendment plus a test-table row for each new
  accepted operation/position.

## Migration Plan

Add characterization tests for current empty-list behavior first, then enable
the nil adapter one operation group at a time while keeping Evaluator/VM tests
green. Document the breaking nil-input outcomes in the Unreleased changelog. No
persisted data migration is required. Rollback removes the nil adapter arms and
restores the previous nil type errors; the core value model is untouched.
