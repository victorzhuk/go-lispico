## 0. Gate

- [x] 0.1 Confirm the trigger post-`stdlib-startup-cache`: warm-startup measurement + per-request embedder profile show env population dominating. If not met, archive with numbers, stop here. — MET: stdlib-startup-cache's own measurement (archive/2026-07-20-stdlib-startup-cache/design.md, task 4.2): warm 107.5 µs / 754 allocs vs ≤ ~40 µs target, cost dominated by per-engine env population + Go-builtin registration.

## 1. Template layer

- [ ] 1.1 Process-level template registry keyed by dialect fingerprint: name → {Go value | compiled artifact | macro artifact}; built from `stdlib-startup-cache` artifacts + builtin registration at first load.
- [ ] 1.2 Root-env miss-path fallback: reserve-under-lock → execute outside lock → publish-under-lock; in-progress reservation resolves recursive first-touch.
- [ ] 1.3 Materialization sets canonical flags and Lisp-2 func-cell mirroring identically to eager load; materialized names join the plugin's unload bookkeeping.

## 2. Observation surfaces

- [ ] 2.1 Macro-table misses materialize macro entries during expansion/compilation.
- [ ] 2.2 Enumeration surfaces (plugin listing, REPL completion) force the full template; documented cost.
- [ ] 2.3 `UnloadPlugin` removes template layer + materialized names; shadowing `def` wins permanently; `Delete`-after-shadow parity pinned by a crossval cell against eager behavior.

## 3. Equivalence and concurrency tests

- [ ] 3.1 Goldset + crossval full suites with lazy on and off — byte-identical results, both dialects, native-op cells included.
- [ ] 3.2 Concurrency: N goroutines first-touching the same name / disjoint names / a name whose definition touches other unmaterialized names — at-most-once materialization, no deadlock, `-race` clean.
- [ ] 3.3 Re-entrancy: materialization triggered from inside a VM run (site publication miss) and from `MacroExpand`.

## 4. Verify

- [ ] 4.1 `go test ./...`, `-race` green; `GOLDSET_MODE=vm` and `eval` gates non-increasing.
- [ ] 4.2 Benchstat ≥6: engine construction + first-touch working set (small script), full-surface script convergence, article startup row; targets: construction ≤10 µs, small-script first-eval total beating GopherLua's 70 µs comfortably.
