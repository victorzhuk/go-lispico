# Dialect reader feature flags

## Why

Textbook Common Lisp uses `#'fn` and `#(...)`, and does not use `[..]`/`{..}` literals; the current Clojure-style surface uses the bracket literals and no `#'`. For a Dialect to parse code written in its own idiom, the reader must vary per Dialect. This slice adds a bounded set of reader feature flags — enough for CL and Clojure fidelity — without a full extensible readtable.

## What Changes

- The Dialect carries a small reader-options set: enable/disable `[..]` and `{..}` literals, enable `#'` function-reference syntax, enable `#(...)` vector syntax.
- The reader honors the running Dialect's flags when tokenizing and parsing.
- The identity Dialect enables the current flags (`[..]`/`{..}` on, `#'`/`#(...)` off), preserving today's parsing.

Reader macros / a user-extensible readtable remain out of scope. What `#'` *means* once read is defined by the namespace-axis slice; this slice only makes it parse.

## Impact

- Affected specs: `dialect`.
- Affected code: `core/reader.go` (tokenizer and parser honor flags).
- No change for existing embedders on the default Dialect.
