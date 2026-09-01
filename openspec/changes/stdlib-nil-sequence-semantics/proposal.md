## Why

Nil handling across sequence builtins is internally inconsistent: operations
such as `count`, `empty?`, and `concat` accept `nil`, while `reverse`, `cons`,
`conj`, `map`, `filter`, `reduce`, `apply`, and `string/join` reject it. Historical
stdlib intent treated `nil` as an empty collection input, but the current spec
preserves the errors, so the boundary contract must be made explicit without
reviving the deferred `nil == '()` value-model change.

Blocked by: `stdlib-typed-error-compliance`.

## What Changes

- Define `nil` as an empty input at selected sequence builtin boundaries while
  retaining `nil` as a distinct runtime value.
- Freeze the accepted argument positions as a closed matrix; future Builtins do
  not inherit nil acceptance merely because they are described as sequences.
- Make `reverse`, `nth`, `cons`, `conj`, `map`, `filter`, `reduce`, `apply`, and
  `string/join` handle `nil` as they handle an empty list, with each operation's
  existing empty-list result and argument-order rules.
- Ensure functions passed to `map` and `filter` are not invoked for `nil`, and
  that reduction/application behavior matches an empty argument sequence.
- Preserve every operation's existing non-`nil` scalar behavior, including
  `(empty? scalar) => false`, as well as collection length, construction depth,
  cancellation, and allocation-ledger enforcement.
- Resolve collection/depth policy from the active GoFunc evaluator rather than
  `env.Evaluator()`, so nested lexical environments cannot lose Engine limits.
- Apply the contract to the one shared builtin implementation used by all
  dialects; this is not a claim of complete Clojure or Common Lisp compatibility.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: define coherent nil handling for sequence-consuming and
  sequence-extending builtins, replacing the requirement that `cons`/`conj`
  preserve their current nil type errors.

## Impact

- Affects collection, higher-order, and string builtins in `plugins/stdlib`,
  their direct tests, runtime Evaluator/VM behavior tests, and stdlib docs.
- **BREAKING** for callers that deliberately catch the current type errors from
  passing `nil` to the affected operations; those calls will now produce the
  same observable result as an empty-list input.
- Does not affect map-only operations (`assoc`, `dissoc`, `keys`, `vals`,
  `contains?`, `merge`), any non-`nil` scalar outcome, or the runtime
  representation, equality, printing, and truthiness of `nil` and empty lists.
