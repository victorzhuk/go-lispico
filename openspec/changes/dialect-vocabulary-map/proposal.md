# Dialect vocabulary name map

## Why

Dialects differ in what they call their builtins — `car`/`cdr` versus `first`/`rest`, `mapcar` versus `map` — but they should share one implementation of each operation. Duplicating `map`/`filter`/`reduce` per dialect would fork the stdlib-completeness workstream (ADR 0004) and let copies drift. A Dialect needs to add *names* onto one shared pure-builtin core, not reimplement it.

## What Changes

- The Dialect carries a vocabulary name map: dialect-visible name → shared builtin implementation.
- Where a dialect's semantics differ by more than a name (argument order, arity, list variants), a thin adapter wraps the shared implementation; the shared implementation is not duplicated.
- A restricted (empty-base) Dialect's vocabulary is likewise an allowlist: a builtin name absent from its map is uncallable, and a builtin added later never leaks in.
- The identity Dialect maps today's names onto today's implementations, unchanged.

## Impact

- Affected specs: `dialect`, `stdlib-plugin`.
- Affected code: the Dialect resolution (name map applied over registered builtins) and the shared builtin core in `plugins/stdlib`.
- No change for existing embedders on the default Dialect.
