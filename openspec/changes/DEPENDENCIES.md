# Dialect change record

This set addressed the review of the dialect layer (`core/dialect.go`, `cl/`,
`clojure/`, and the dialect paths in `runtime/` and the compiler) at commit
`1d36a9b`. All seven changes are complete and archived; no change is active.
They landed in the order listed below. Testing mode was
existing-service-strict.

## Decisions (2026-09-26)

- Construction model: declarative `NewDialect(DialectSpec)`; the builders are
  removed in the same change, no deprecation window. The builder-hardening
  alternative was withdrawn.
- A renamed-away or removed form name is an ordinary symbol on every path.

## Archived changes

| Change | Accepted outcome | Capabilities |
| --- | --- | --- |
| [dialect-value-integrity](archive/2026-09-26-dialect-value-integrity/proposal.md) | Accessors cannot mutate a shared dialect; fingerprints are collision-free and process-local | dialect |
| [dialect-declarative-spec](archive/2026-09-26-dialect-declarative-spec/proposal.md) | Validated frozen construction from a plain-data spec; builders removed | dialect |
| [dialect-registration-rules](archive/2026-09-26-dialect-registration-rules/proposal.md) | One registration rule set for eager and lazy paths; Lisp-2 bridge without vocabulary; host bindings survive; `Use` costs O(its own names) | dialect |
| [dialect-surface-name-resolution](archive/2026-09-27-dialect-surface-name-resolution/proposal.md) | One dispatch source; renamed-away names and `cond` bodies behave the same on both evaluators | dialect |
| [dialect-form-enumeration](archive/2026-10-02-dialect-form-enumeration/proposal.md) | `Dialect.Forms()` lists the resolved special forms | dialect |
| [cl-dialect-surface-cleanup](archive/2026-10-03-cl-dialect-surface-cleanup/proposal.md) | No identity vocabulary entries; single stock `DialectSpec`; `clSort` comparison allocations addressed if safe | dialect |
| [dialect-kernel-cleanup](archive/2026-10-03-dialect-kernel-cleanup/proposal.md) | Truthiness hook removed; no compiler nil panic; `core.IsCallable`; smaller cache key | dialect, bytecode-vm |

## Ordering notes (as landed)

- 1 was small and independent of the construction model; it went first
  because the vocabulary exposure let a caller widen a policy allowlist.
- 2 (declarative spec) was applied before 3 (registration rules), so 3's tests
  built dialects with `NewDialect`.
- 3 preceded 6: dropping the CL identity entries was safest once eager and
  lazy registration shared one rule set and agreed on canonical flags.
- 1 and 2 both touched the fingerprint: 1 fixed the field encoding, 2 moved
  the digest onto the resolved configuration and kept 1's scenarios.
- 4 stored its reverse dispatch map in 2's frozen state; 5 read 2's resolved
  table and returned `[]string` because an invalid Dialect can no longer
  exist.
- 6 kept the CL definition as one `DialectSpec`, so it followed 2.
- 7 touched the compiler and `cl` imports that 4 and 6 also edited; it landed
  last to avoid rebasing those.
