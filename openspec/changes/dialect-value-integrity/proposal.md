## Why

A Dialect is documented as an immutable value, and the stock dialects are process-wide singletons. Two defects break that:

- `Vocab()` returns the internal map (`core/dialect.go:258`). Probe: `cl.Dialect().Vocab()["car"] = VocabEntry{Canonical: "rest"}` changes `car` for every later engine in the process, and the memoized fingerprint stays the same. On an empty-base policy dialect, the same write widens the allowlist after construction, which breaks the ADR 0005 policy boundary.
- The fingerprint joins vocabulary fields with `:` and `|` and no escaping (`core/dialect.go:526-540`). Probe: `Vocabulary({"a:b": "c"})` and `Vocabulary({"a": "b:c"})` produce the same digest. The fingerprint keys the process-wide stdlib template registry (`runtime/lazy_template.go:707`), so colliding dialects share rename templates.

A third issue is a missing contract, not a bug: embedders have stored `Fingerprint()` on disk (zhk's former resume pins), but nothing says whether it is stable across releases.

## What Changes

- `Vocab()` returns a copy (nil stays nil, an empty map stays a non-nil empty map, so the identity-versus-allowlist distinction holds); add `VocabEntry(name) (VocabEntry, bool)` for single lookups without a copy. Runtime callers keep using `Vocab()`: they copy once per plugin load, or once per registered builtin during the lazy template build, which runs once per process per dialect and plugin.
- Encode every fingerprint field unambiguously (length-prefixed or quoted). Existing fingerprints change; they are in-process keys only.
- Document and spec `Fingerprint()` as a process-local identity that may change between releases.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: add **Dialect accessors never expose mutable state** and **Dialect fingerprint is a process-local, collision-free identity**.

## Impact

`core/dialect.go`, `core/dialect_test.go`, `cl/cl_test.go`, `runtime/dialect_vocab_test.go`, `docs/dialect-layer.md` (its "stable hash across processes" line contradicts the new contract), CHANGELOG. Independent of the builder-vs-spec API decision; if `dialect-declarative-spec` lands first, apply these rules to its frozen state.
