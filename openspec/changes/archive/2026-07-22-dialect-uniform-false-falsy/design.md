## Empirical basis

The nil-only axis was justified as CL nil-punning, but the value model does not
support it:

- `'()` reads as a distinct `List`, not `Nil`: `(type '())` → `:list`,
  `(nil? '())` → `false`, `(= '() nil)` → `false`. So the empty list is truthy
  under both axes — nil-punning of the empty list never happened.
- Across all 13 `Value` types, `!isNil` (nil-only) and `IsTruthy` (nil+false)
  differ on exactly one value: the concrete `Bool{false}`.

So the axis has one and only one observable effect — making `Bool{false}`
truthy — which is precisely the inversion. It is vestigial and buggy, not a
faithful-CL feature. Removing it loses no behavior the value model actually
provides.

## Why this over the alternatives

- **Predicates return the dialect's canonical false (nil under CL).** Faithful
  CL, but couples the shared stdlib to the running dialect, breaking the
  "single shared implementation" requirement. Worse, it splits the two false
  sources: `(if false …)` would still be `:yes` (the literal) while `(if (= 1
  2) …)` becomes `:no` (the predicate) — a more confusing inconsistency than
  the one it fixes.
- **Flip the default dialect to Clojure.** Sidesteps rather than fixes — CL
  stays internally inverted for anyone who opts into it, and it churns the whole
  documented surface (naming, reader, examples). The bug remains latent.
- **Document as a footgun.** Leaves a silent wrong-branch trap in the default
  config. Rejected.

Uniform truthiness fixes literal and computed `false` together, keeps stdlib
dialect-agnostic, and needs no new machinery — the `IsTruthy` it falls back to
already exists and is what every non-CL dialect uses.

## Smaller-diff alternative (rejected)

Neuter `isTruthy` (treat `Bool{false}` as falsy) but keep `NilOnlyFalsy()` as a
deprecated no-op, avoiding the public-API removal. Rejected: a dialect-builder
method that silently does nothing is a footgun, and for an alpha the clean
removal — which makes every caller a compile error — is safer than a silent
no-op. Fall back to this only if API stability outweighs cleanliness at review.

## VM parity

Both evaluators resolve truthiness from the dialect (`truthy` is the dialect's
`isTruthy`, resolved once per VM frame). Removing the axis makes the VM and
tree-walker uniform in lockstep; the crossval suite over predicate-driven
conditionals under CL is the parity guard.

## Migration

- `cl.Dialect()`: drop `.NilOnlyFalsy()`.
- `clojure.Dialect()`, identity: already nil+false falsy — behavior unchanged,
  no edit beyond the removed axis field.
- Any external custom dialect calling `.NilOnlyFalsy()` gets a compile error
  pointing at the exact call — the intended, visible migration signal.
