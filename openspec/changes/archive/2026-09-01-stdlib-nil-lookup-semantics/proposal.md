## Why

Map lookup currently has no canonical contract for `nil`, and the Lisp bootstrap
can express only the two-argument happy path of `get-in`. This leaves safe nested
lookup incomplete: a missing intermediate must propagate without an error, while
a supplied default must still distinguish a missing terminal key from a terminal
key whose stored value is `nil`.

Blocked by: `builtin-resource-accounting` and
`stdlib-bootstrap-evaluator-ownership`.

## What Changes

- Define `get` on a `nil` subject as lookup in an empty map: return `nil` without
  a default and the supplied default with one.
- Preserve map presence semantics: a present key whose value is `nil` returns
  `nil`, not the supplied default.
- Give `get-in` two- and three-argument forms with short-circuiting traversal,
  correct missing-versus-present-`nil` behavior, and an empty-path identity case.
- Keep Builtin nested traversal copy-free, cooperatively cancellable, and charged
  through the shared batched Builtin-work checkpoint once per visited key.
- Preserve the typed error for non-map, non-`nil` lookup subjects and
  intermediates. This change does not broaden `get` to vectors, strings, arrays,
  or sets.
- Treat stored values, defaults, and empty-path subjects as borrowed results, so
  returning them does not create an allocation charge.
- Keep the behavior in the shared stdlib implementation so every dialect and
  execution path that resolves the operations observes the same contract.
- **BREAKING** Change the public `get-in` value from a Lisp Lambda to a Builtin;
  its printed/equality behavior changes, and an empty-base Dialect must explicitly
  allowlist it like every other Builtin.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: specify nil-tolerant map lookup, presence-aware defaults, and
  complete nested lookup semantics for `get-in`.

## Impact

- Affects `plugins/stdlib` lookup registration and tests, the Lisp bootstrap
  entry for `get-in`, runtime bootstrap/lazy-materialization tests, and public
  stdlib documentation.
- Existing successful map lookups are unchanged. Calls that currently fail only
  because the lookup subject or a missing intermediate is `nil` will return a
  value instead.
- Removing the only reusable Lisp bootstrap entry leaves the process-level
  bootstrap artifact cache without a producer; the blocked-by-successor
  `stdlib-bootstrap-cache-retirement` removes that dormant subsystem.
- No core value-model, reader, printer, equality, or truthiness change is made;
  in particular, `nil` does not become an empty list or map value. The callable
  representation change is limited to `get-in` itself.
