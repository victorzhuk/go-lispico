## Context

`loadPlugin` (`runtime/plugin.go:165-192`) runs `Init`, then `applyVocabulary(env)` over the registration view, then diffs a before/after snapshot. `applyVocabulary`:

1. builds `goFuncs` from every `LocalNames()` entry (unsized map);
2. on an empty base, deletes every GoFunc not in the vocabulary;
3. `Set`s every vocabulary entry, whichever plugin is loading;
4. under Lisp-2, walks `LocalNames()` again and `SetFunc`s every GoFunc whose function cell is absent or equal (equal cells are rewritten, each costing a journal record and a cell version bump).

The lazy path applies (3) and the allowlist per registered value and binds function cells at materialization.

## Decisions

### One decision function in core

A core function takes the dialect and one registered `(name, value, canonical)` and returns the bindings to make: zero or more value-cell names (the registered name unless the allowlist drops it, plus every vocabulary alias whose canonical name matches), whether each is canonical, and whether to mirror each into the function cell. Runtime eager and lazy paths only execute that plan. Adapters are dialect-owned bindings applied once per engine, not once per plugin.

### Scope to the operation

The eager pass runs over the names `Init` added or changed in this operation. A vocabulary alias is (re)bound only when its canonical name is in that set. Equal function cells with the same canonical flag are not rewritten.

### Host bindings

The allowlist filters plugin registrations as they happen. Nothing deletes existing root bindings afterwards, so `Bind` values survive any later plugin operation. This matches ADR 0005: the allowlist restricts what plugins expose to rule code; the host is trusted.

## Risks

- A host that relied on the allowlist deleting its own earlier `Bind` values: none known; the behavior was order-dependent.
- The core decision function becomes public surface for core-only embedders; keep it minimal (one function, one small result type).
