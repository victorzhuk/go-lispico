## Context

The dialect layer was rewritten across the seven archived changes of the 2026-09-26..2026-10-03 set. The rewrite landed the behavior the corpus asks for, and it introduced the process-wide `vocabShape` cache in `runtime/engine.go` (`fix(runtime): scope vocabulary pass to the operation's written names`). This change fixes what the release review of that delta found, before the release is cut.

## Decisions

- **Adapter values are per dialect.** `vocabShape` keeps only the sorted adapter names; `applyVocabulary` reads each adapter's value from the engine's own dialect through `Dialect.VocabEntry`. The fingerprint is deliberately value-blind — it identifies semantics, not object identity — so it may key a structural cache and must never key a value.
- **The shape cache stays fingerprint-keyed.** It exists so `New` and `Use` do not copy the vocabulary map; the change keeps that and bounds what an entry can retain to names.
- **Unknown base is a construction error.** `DialectSpec.Base` documents two values. A third accepted silently is an inconsistent mode: an empty table that `IsBaseEmpty` reports false and the empty-base allowlist filter ignores. Rejecting it makes this guard the same kind as every other one in the constructor.
- **The scaling test becomes deterministic.** A wall-clock ratio on a shared machine is not evidence: one test measured 7.25 under the parallel floor and 2.0 alone. Allocation counts are deterministic and catch the same regression, because a quadratic decode grows allocations per key.
- **Documents are corrected, not reworded.** Every drift fix replaces a stale number, tree entry, axis claim or link with the measured state; no document is restructured.
- **The two spec amendments travel as deltas.** `bytecode-vm` and `dialect` requirements are modified in this change's `specs/` and fold into the corpus at archive.

## Non-goals

- No new dialect axis, no cache eviction policy, no change to `Dialect.Fingerprint`.
- `docs/adr/0005-dialect-layer.md`'s Amendment block stays the record of the truthiness removal; only its stale body lines and the method it names are corrected.
- The historical `CHANGELOG.md` sections that quote the removed builder API stay as written: they are the record of earlier releases.
