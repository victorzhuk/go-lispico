# Design — Dialect vocabulary name map

## Context

Special-form dispatch became per-Engine in the dispatch slice; vocabulary — the builtins plugins register — is the other half of what a Dialect controls. Builtins are `GoFunc` values registered into the environment. A Dialect must present them under dialect-specific names over one shared set of implementations.

## Decisions

- **One shared implementation core.** Each operation (`first`/`rest`/`map`/`filter`/`reduce`/…) has exactly one implementation in the shared builtin core. Dialects never carry their own copy.
- **Vocabulary is a name map.** The Dialect maps a dialect-visible name to a shared implementation. Resolution binds the visible name to that implementation in the Engine's environment. `car` → the first-implementation, `cdr` → the rest-implementation.
- **Adapters only where semantics differ.** When a dialect's function differs from the shared core by argument order, arity, or multi-list handling (e.g. CL `mapcar`), a thin adapter wraps the shared implementation. The adapter is the exception, not the rule; a plain rename is the default.
- **Restriction applies to vocabulary.** An empty-base Dialect's vocabulary map is an allowlist. A builtin whose name is not in the map is uncallable under that Dialect, and a builtin added to the shared core later does not appear unless the map adds it — the same fail-closed guarantee dispatch has for special forms.

## Risks

- The set of CL functions needing real adapters (rather than renames) is not fully enumerated; it is a flagged gap in the PRD. This slice ships the mechanism and the renames it can prove; adapter breadth is filled when the CL dialect is assembled.

## Out of scope

Assembling the CL/Clojure vocabularies in full and flipping the default — that is `dialect-common-lisp-default`. This slice delivers the mapping mechanism and its restriction guarantee.
