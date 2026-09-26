# Active change dependency order

This set addresses the review of the dialect layer (`core/dialect.go`, `cl/`,
`clojure/`, and the dialect paths in `runtime/` and the compiler) at commit
`1d36a9b`. Every change contains a proposal, design, capability deltas, and
unchecked implementation tasks. Testing mode is existing-service-strict. The
previously listed dependency chain is archived; the table below covers the
active set.

## Decisions (2026-09-26)

- Construction model: declarative `NewDialect(DialectSpec)`; the builders are
  removed in the same change, no deprecation window. The builder-hardening
  alternative was withdrawn.
- A renamed-away or removed form name is an ordinary symbol on every path.

## Changes and suggested merge order

| Order | Change | Accepted outcome | Capabilities |
| --- | --- | --- | --- |
| 1 | [dialect-value-integrity](dialect-value-integrity/proposal.md) | Accessors cannot mutate a shared dialect; fingerprints are collision-free and process-local | dialect |
| 2 | [dialect-registration-rules](dialect-registration-rules/proposal.md) | One registration rule set for eager and lazy paths; Lisp-2 bridge without vocabulary; host bindings survive; `Use` costs O(its own names) | dialect |
| 3 | [dialect-declarative-spec](dialect-declarative-spec/proposal.md) | Validated frozen construction from a plain-data spec; builders removed | dialect |
| 4 | [dialect-surface-name-resolution](dialect-surface-name-resolution/proposal.md) | One dispatch source; renamed-away names and `cond` bodies behave the same on both evaluators | dialect |
| 5 | [dialect-form-enumeration](dialect-form-enumeration/proposal.md) | `Dialect.Forms()` lists the resolved special forms | dialect |
| 6 | [cl-dialect-surface-cleanup](cl-dialect-surface-cleanup/proposal.md) | No identity vocabulary entries; single stock `DialectSpec`; `clSort` comparison allocations addressed if safe | dialect |
| 7 | [dialect-kernel-cleanup](dialect-kernel-cleanup/proposal.md) | Truthiness hook removed; no compiler nil panic; `core.IsCallable`; smaller cache key | dialect, bytecode-vm |

## Ordering notes

- 1 is small and independent of the construction model; it goes first
  because the vocabulary exposure lets a caller widen a policy allowlist.
- 2 precedes 6: dropping the CL identity entries is safest once eager and lazy
  registration share one rule set and agree on canonical flags.
- 1 and 3 both touch the fingerprint: 1 fixes the field encoding now, 3 moves
  the digest onto the resolved configuration and must keep 1's scenarios.
- 4 stores its reverse dispatch map in 3's frozen state; 5 reads 3's resolved
  table and returns `[]string` because an invalid Dialect can no longer exist.
- 6 keeps the CL definition as one `DialectSpec`, so it follows 3.
- 7 touches the compiler and `cl` imports that 4 and 6 also edit; land it last
  to avoid rebasing those.
