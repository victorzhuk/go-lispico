## Why

`core.Dialect` is built by a chain of copy-on-write methods ending in an optional `Memoized()`. The chain buys the ability to extend an existing dialect (`cl.Dialect().Remove(...)`), and no production code or consumer uses it: go-lispico's own production code builds only the two stock dialects (13 in-repo runtime tests extend `clojure.Dialect()` to get a unique fingerprint; they migrate to a spec with `FlatCond` plus the extra form), yagel and zhk use only `clojure.Dialect()`, and every custom dialect built or planned by them (zhk's former workflow dialect, yagel's candidate workflow hardening) is an empty base plus a form list plus a vocabulary map, assembled from data before any builder runs.

The chain's costs are real:

- It has no freeze point. Resolution, validation and fingerprinting happen lazily or through `Memoized()`, and every builder must clear the cache by hand.
- It cannot fail. An adapter without an ID or a rename of an unknown form is reported at engine construction or evaluation, far from the code that built the dialect.
- `Vocabulary` replaces the vocabulary map, so a `WithAdapter` call before it is silently dropped; the same calls in the other order keep the adapter.
- The fingerprint hashes the operation history, so two dialects with identical dispatch can fingerprint differently and miss each other's template entries.
- Dispatch is derived twice: `resolve()` builds the table and `CanonicalName` rescans the ops. They already disagree on renamed-away names (see `dialect-surface-name-resolution`). A frozen resolved state gives both one source.

## What Changes

- **BREAKING**: add `core.NewDialect(DialectSpec) (Dialect, error)`. `DialectSpec` is plain data: base, a visible→canonical form map, a list of hidden kernel names, the axis settings, the vocabulary map and the adapters. `NewDialect` validates, resolves and fingerprints once and returns a frozen value, or an error and the zero Dialect; there is no separate `Memoized()`.
- The fingerprint hashes the normalized resolved configuration (base, axes, form table, whether a vocabulary is configured, vocabulary entries, adapter IDs), not the history, so equal dispatch means equal fingerprint. This also separates an empty-base dialect with no vocabulary from one with an empty vocabulary, which collide today.
- `cl` and `clojure` build their stock dialects from a spec.
- The builder methods, `FullDialect`, `EmptyDialect` and `Memoized` are removed in this change; every in-repo caller migrates to `NewDialect`.

Decisions (resolved 2026-09-26; the order audit remains task 0.2):

- [x] Accepted over keeping the builder; `dialect-builder-invariants` is withdrawn.
- [x] No deprecation window: the builders are removed in this change. The project is alpha and no external consumer calls them.
- [x] An invalid specification returns an error and the zero Dialect (Go convention). The zero Dialect is the full-kernel identity, so callers must not ignore the error.
- [x] Construction also rejects an adapter without a value, a name both hidden and mapped, and a Lisp-2 spec that maps `funcall` or `function` (today the axis silently overwrites them).
- [x] Applied before `dialect-registration-rules`, swapping their order in `DEPENDENCIES.md`; the overlap is comments in `runtime/engine.go` and `runtime/lazy_template.go`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: modify **A Dialect is a Delta over a declared base**, **Stock dialect construction is memoized and immutable** and **Dialect adapters have semantic fingerprint identity**; add **Dialects are constructed from a validated specification**.

## Impact

`core/dialect.go` (constructor, fingerprint), `core/reader.go` (`Read` uses the zero Dialect), `cl/cl.go`, `clojure/clojure.go`, and every test that builds a dialect: at `a2dd7b4`, 174 `FullDialect()`/`EmptyDialect()` calls (108 bare `FullDialect`, 12 bare `EmptyDialect`, the rest chained), 8 `Memoized()` and 13 `clojure.Dialect().Add(...)` calls across 35 test files (core 16, runtime 15, cl 2, clojure 1, core/vm 1). Docs: ADR 0005 amendment, the README Dialects section (a `NewDialect` example), `docs/dialect-layer.md` and `ARCHITECTURE.md` (builder text), CHANGELOG with a migration note. yagel and zhk are unaffected (they call `clojure.Dialect()` only). Fingerprints of stock dialects change; the fingerprint is an in-process cache key only.
