# Dialect truthiness axis

## Why

Common Lisp treats only `nil` as false; the current interpreter treats both `nil` and `false` as falsy (Clojure-style). This is a semantic axis a Dialect must be able to set — it changes the outcome of every conditional. It is the smallest, lowest-risk axis and lands independently of the namespace axis.

## What Changes

- A truthiness setting is added to the Dialect: `nil`-only falsy, or `nil`+`false` falsy.
- The conditional special forms — `if`, `when`, `unless`, `cond`, `and`, `or`, `not` — consult a single truthiness hook derived from the Engine's Dialect rather than a hardcoded rule.
- The identity Dialect keeps `nil`+`false` falsy, so existing behavior is unchanged until a Dialect opts into `nil`-only.

## Impact

- Affected specs: `dialect`.
- Affected code: `core/eval.go` (the conditional forms and the truthiness hook).
- No change for existing embedders on the default Dialect; a Dialect setting `nil`-only truthiness diverges only on `false`.
