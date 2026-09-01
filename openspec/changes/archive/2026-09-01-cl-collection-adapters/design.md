## Context

The Dialect vocabulary supports adapter GoFuncs, but the stock CL vocabulary maps
`nth`, `mapcar`, and `sort` directly to canonical stdlib names. Canonical `nth`
takes collection before index, `map` accepts one collection, and `sort` uses an
implicit natural comparator. The CL names require different argument shaping and,
for `mapcar`/`sort`, parameterized callback behavior.

The shared-implementation rule permits thin Dialect adapters but forbids a second
copy of the collection algorithms. Lispico values are immutable and do not model
CL cons-cell mutation, so the adapter can match call/selection semantics without
claiming destructive identity behavior.

## Goals / Non-Goals

**Goals:**

- Make the three exposed CL names accept their documented CL argument shapes.
- Keep indexing, zip traversal, callback application, and sorting kernels shared.
- Preserve Lisp-2 function resolution and typed/Terminal error propagation.
- Give the full CL `sort` call grammar, callback-count, error-order, result-type,
  and resource-accounting contract.
- Carry later nil sequence-boundary changes through adapters naturally.

**Non-Goals:**

- Implement dotted lists, displaced arrays, destructive sequence identity, or
  every ANSI CL sequence function.
- Change canonical `nth`, `map`, or natural `sort` behavior.
- Make bracket literals available in the CL reader.

## Decisions

### Share parameterized internal kernels

Move the minimum indexing, multi-sequence mapping, and sorting mechanics into an
internal package imported by both stdlib registration and the CL Dialect adapter
constructors. Canonical Builtins provide their existing argument policy and call
the kernels; CL adapters normalize their visible arguments and call the same
kernels. Kernels receive the active `core.Evaluator`, context, and a
`core.BuiltinWorkBudget`; they do not rediscover dynamic limits through
`env.Evaluator()`.

Resolving canonical names dynamically from the environment was rejected because
the adapter may occupy the same visible name and user rebinding could redirect
its dependency. Exporting implementation details from the stdlib plugin was
rejected in favor of a module-internal package with no public API commitment.

### Adapt `nth` order and absent result

CL `nth` accepts a non-negative index followed by a list. The adapter validates
that shape, invokes the shared accessor with reordered arguments, and supplies
`nil` as its out-of-range result. It does not expose the canonical optional
default argument under the CL name. `nil` is accepted as the empty CL list;
negative indices are `EvalError`, and non-list/non-`nil` values are `TypeError`.

### Generalize the mapping kernel, not canonical `map`

The shared mapping kernel accepts one or more ordered input sequences and applies
the callback to one aligned tuple per output element until the shortest input is
exhausted. Canonical `map` retains exactly one input sequence; `mapcar` admits one
or more lists, where `nil` is an empty list. The result is always a `List`.
Callback application continues through the active Evaluator so Lisp-2 function
values, re-entry state, metering, and Terminal errors are shared.

### Parameterize sorting while retaining immutable results

The sorting kernel accepts a comparison strategy and optional key projection.
Canonical `sort` supplies the existing natural comparator. CL accepts exactly
`(sort sequence predicate)` or `(sort sequence predicate :key key-fn)`. Missing
required arguments, a dangling option, or extra positional arguments are
`ArityError`; an unknown or duplicate keyword is `EvalError`. `:key nil` means
identity. Lists return Lists, vectors return Vectors, and `nil` returns an empty
List. Non-list/vector/non-`nil` input is `TypeError`.

The adapter copies the input without mutation, then applies the key function
exactly once per element in original order and stores projected keys. Only after
all keys succeed does stable sorting begin. The predicate receives two projected
keys and its result uses the active Dialect's generalized truthiness. On the
first key or predicate error, no later Lisp callback is invoked and the original
typed or Terminal error is returned unchanged after the mandatory work-budget
flush. If that flush discovers a Terminal error while the callback error is
non-Terminal, the shared Terminal precedence rule wins. Stable ordering for
predicate-equivalent elements is a deterministic project guarantee even though
ANSI CL does not require it.

Input copying, tuple alignment, key/result storage, and comparator scheduling
accrue one `BuiltinWorkBudget` step per semantic unit and flush on every return.
Key and predicate evaluation are owned by evaluator re-entry and are not charged
again as Builtin work. This division applies to canonical callers of the shared
kernels as well as CL adapters.

### Build adapters into the memoized stock Dialect

Construct adapter GoFuncs once with the memoized CL Dialect and bind them through
`WithAdapter(name, semanticID, value)`. Add `AdapterID` to `VocabEntry`; the ID
contains an operation name and contract version such as `cl/sort@1` and is the
only adapter implementation identity hashed. The fingerprint includes visible
name, canonical name, and adapter ID in sorted vocabulary order. Empty/missing
IDs are rejected during Dialect resolution/Engine construction. Function pointers
and `%T` are not hashed: they are unstable or fail to distinguish semantics.
Empty-base custom Dialects receive no adapters unless explicitly added.

## Risks / Trade-offs

- [Risk] Kernel extraction changes canonical behavior → characterize canonical
  values, errors, callback counts, and allocations before refactoring.
- [Risk] Multi-list `mapcar` invokes a callback after one input is exhausted →
  test unequal and zero-length inputs and stop before reading/applying a tuple.
- [Risk] Predicate/key errors are flattened → return typed and Terminal callback
  errors unchanged under both execution paths.
- [Risk] Sort calls callbacks after the first failure → precompute keys, latch the
  first comparator error, and prohibit further evaluator calls once latched.
- [Risk] Adapter code changes without cache-key invalidation → require an explicit
  versioned semantic ID and test same-ID stability/different-ID separation.
- [Risk] Shared kernels bypass resource limits → pass the active work budget and
  evaluator into every scalable phase and test late VM deadline entry.
- [Risk] Users infer destructive CL sort identity → document immutable input and
  test value/type results rather than pointer identity.

## Migration Plan

After both prerequisites land, add behavior goldens for current canonical names
and desired CL names, extend fingerprint identity, extract the shared kernels,
then switch the CL vocabulary entries to adapters. Document the breaking call
shapes and Go builder signature in Unreleased. Rollback restores direct mappings
and the old CL behavior; no stored data changes.
