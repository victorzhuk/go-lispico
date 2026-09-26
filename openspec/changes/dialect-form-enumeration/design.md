## Context

The resolved `name → formFn` table (base, delta, Lisp-2 intrinsics) is built once per Dialect (by `NewDialect` after `dialect-declarative-spec`). The names are the table's keys.

## Decisions

- **Source of truth is the resolved table**, not `ops`, so renames, removals and axis intrinsics are reflected exactly as dispatch sees them.
- **Return `[]string`**: after `dialect-declarative-spec` an invalid Dialect cannot be constructed, so there is no resolution error to report.
- **Fresh sorted slice per call.** The memoized table is shared process-wide, so it is never handed out; callers are static checkers, not hot paths.
- Vocabulary names are out of scope: builtins come from plugins at engine construction, and `Vocab()` already exposes the dialect's own map.
