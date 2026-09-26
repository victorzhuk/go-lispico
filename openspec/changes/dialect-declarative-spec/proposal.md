## Why

`core.Dialect` is built by a chain of copy-on-write methods ending in an optional `Memoized()`. The chain buys the ability to extend an existing dialect (`cl.Dialect().Remove(...)`), and nobody uses it: go-lispico's own production code builds only the two stock dialects, yagel and zhk use only `clojure.Dialect()`, and every custom dialect built or planned by them (zhk's former workflow dialect, yagel's candidate workflow hardening) is an empty base plus a form list plus a vocabulary map, assembled from data before any builder runs.

The chain's costs are real:

- It has no freeze point. Resolution, validation and fingerprinting happen lazily or through `Memoized()`, and every builder must clear the cache by hand.
- It cannot fail. An adapter without an ID or a rename of an unknown form is reported at engine construction or evaluation, far from the code that built the dialect.
- `Vocabulary` replaces the vocabulary map, so a `WithAdapter` call before it is silently dropped; the same calls in the other order keep the adapter.
- The fingerprint hashes the operation history, so two dialects with identical dispatch can fingerprint differently and miss each other's template entries.
- Dispatch is derived twice: `resolve()` builds the table and `CanonicalName` rescans the ops. They already disagree on renamed-away names (see `dialect-surface-name-resolution`). A frozen resolved state gives both one source.

## What Changes

- **BREAKING**: add `core.NewDialect(DialectSpec) (Dialect, error)`. `DialectSpec` is plain data: base, a visible→canonical form map, a list of hidden kernel names, the axis settings, the vocabulary map and the adapters. `NewDialect` validates, resolves and fingerprints once and returns a frozen value; there is no separate `Memoized()`.
- The fingerprint hashes the normalized resolved configuration, not the history, so equal dispatch means equal fingerprint.
- `cl` and `clojure` build their stock dialects from a spec.
- The builder methods, `FullDialect`, `EmptyDialect` and `Memoized` are removed in this change; every in-repo caller migrates to `NewDialect`.

Decisions (resolved 2026-09-26; the order audit remains task 0.2):

- [x] Accepted over keeping the builder; `dialect-builder-invariants` is withdrawn.
- [x] No deprecation window: the builders are removed in this change. The project is alpha and no external consumer calls them.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: modify **A Dialect is a Delta over a declared base**; add **Dialects are constructed from a validated specification**.

## Impact

`core/dialect.go` (constructor, fingerprint), `cl/cl.go`, `clojure/clojure.go`, every test that builds a dialect (about 58 `FullDialect`/`EmptyDialect` sites plus their chains), README, ADR 0005 amendment, CHANGELOG with a migration note. yagel and zhk are unaffected (they call `clojure.Dialect()` only). Fingerprints of stock dialects change; the fingerprint is an in-process cache key only.
