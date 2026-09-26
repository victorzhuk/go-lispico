## Context

Evidence gathered for this change:

- go-lispico production builds two dialects (`cl/cl.go`, `clojure/clojure.go`), both behind `sync.OnceValue` + `Memoized()`.
- yagel: `clojure.Dialect()` at `internal/core/engine.go:512`, `internal/cli/rules_check.go:28`, and for reading at `internal/rules/check.go`, `internal/core/batch_cell.go`. Restriction is done outside the dialect (static form checks, curated child-env bindings). Its `openspec/specs/workflow/spec.md` lists `EmptyDialect` + `Vocabulary` allowlist as candidate hardening.
- zhk: `clojure.Dialect()` at `internal/engine/engine.go:575`. Its former workflow dialect (removed in `f5452cd`) was `EmptyDialect()` + `Add(f, f)` per form in a loop + one `Vocabulary(map)` + `Memoized()`, and it stored `Fingerprint()` in resume pins.

Immutability is required regardless of API shape: the fingerprint keys the process-wide stdlib template registry, stock dialects are shared across engines and goroutines, and ADR 0005 fixes the dialect for an Engine's lifetime as a policy boundary. The question here is only how the value is constructed.

## Goals / Non-Goals

**Goals:** one validated construction step that returns a frozen value or an error; a semantic fingerprint; a data shape that matches how embedders already hold their dialect definitions.

**Non-Goals:** new dialect axes; Lisp-side dialect selection; changing vocabulary application in `runtime`.

## Decisions

### Specification shape (sketch, final names at planning)

```go
type DialectSpec struct {
    Base       Base              // BaseFull or BaseEmpty
    Forms      map[string]string // visible name -> kernel form; added on top of Base
    Hide       []string          // kernel names removed from Base
    Lisp2      bool
    NoBrackets bool
    FunctionRef, ReaderVector, FlatCond bool
    Vocab      map[string]string // visible -> canonical builtin
    Adapters   map[string]Adapter
}

type Adapter struct {
    ID    string
    Value Value
}
```

Rename `a → b` is `Forms{b: a}` plus `Hide{a}`. A name present in both `Vocab` and `Adapters` is a construction error, which removes the ordering question entirely.

### Frozen value

`Dialect` holds a pointer to an immutable resolved state (table, fingerprint, vocabulary entries, axes). Copies are one word; no cache field, no invalidation. The zero value (nil state) is the identity dialect: full kernel, no delta, Lisp-1, no vocabulary, so `core.Dialect{}` stays usable wherever a default is needed. `NewDialect` copies the caller's maps and slices, so later caller mutation cannot reach the dialect.

### Semantic fingerprint

The digest covers base, axes and the sorted resolved form table (visible → canonical), the sorted vocabulary and the adapter IDs. Two specs with the same dispatch produce the same digest.

### Why not functional options

Ordered options (`NewDialect(Full, Lisp2(), Add(...))`) would give the same freeze point, but embedders hold their definitions as slices and maps, not as option lists. A spec takes that data directly and can be compared, logged and validated as a whole.

## Risks

- Test churn is large (about 58 construction sites plus chains); it lands in this change because the builders are removed, not deprecated.
- Any caller that relied on history-sensitive fingerprints: none known.
