# Design — engine-reduction-and-allocation-metering

## Decisions

- D1: Reductions are charged at the eval-step boundary, with a per-evaluator
  step definition (trampoline iteration + form dispatch vs instruction
  decode). Counter equality across evaluators is explicitly NOT a
  requirement — the same form compiles to a different step count by
  construction. Parity contract: same configured limits, same adversarial
  program → both evaluators terminate with the same terminal error class.
  The earlier "evaluators agree on counter values" scenario was
  unimplementable and is dropped.
- D2: Counting is free on the hot path. Both evaluators already decrement a
  128-step budget per step for batched cancellation; the reduction counter
  advances by the consumed budget at each poll boundary and at eval end.
  Ceiling enforcement at poll boundaries gives ±128 precision on a 10M
  default — irrelevant slack, zero added per-step cost. The 128 interval
  already satisfies "context observed within 1,024 reductions"; no
  `defaultReductionCtxCheckInterval` constant is introduced.
- D3: Allocation accounting uses a fixed per-type size table (documented in
  ADR 0011): deterministic across architectures and Go versions, leaning
  conservative (over-count small allocations). `unsafe.Sizeof` is rejected —
  platform-dependent ledgers break reproducibility, which yagel's
  deterministic-ledger posture requires.
- D4: There is no constructor chokepoint to instrument — `List`/`Vector` are
  composite literals, and core constructors have no context. Charging
  therefore happens at the enumerated eval-side sites (VM make-ops,
  tree-walker literal construction, compiler emit, GoFunc result shallow
  size, reader bridge). GoFunc result charging is shallow by design: values
  built incrementally in Lisp get charged incrementally at make-sites; a
  value materialized inside Go (e.g. a large string) is charged by its
  shallow size — which for strings and flat collections is the dominant
  term. Documented approximation.
- D5: The reader charges without a context: it accumulates node and byte
  counts into the existing reader state (alongside depth tracking) and the
  engine transfers the totals into the evaluation ledger right after `Read`
  returns, before the first form runs. Reader depth limits are unchanged.
- D6: Ceiling breach raises `CodeResourceLimit`, terminal per
  `eval-noncatchable-terminal-errors` (dependency). No new error code —
  yagel already branches on `CodeResourceLimit`; the message distinguishes
  reductions vs allocation.
- D7: Defaults are identical to yagel ADR 0105's per-evaluation values so
  consumer and embedder share one contract. Zero means default, not
  unlimited — matching every existing `ResourceLimits` field.

## Risks / Trade-offs

- Shallow GoFunc charging under-counts deeply nested values materialized in
  Go; mitigated by conservative table values and by per-env retained ceilings
  (`retained-state-owned-capacity-accounting`) bounding what survives.
- Poll-boundary enforcement means up to 127 uncharged steps before a trip;
  documented, negligible against 10M defaults.
- Goldset risk: the flush-at-poll design adds two integer adds per 128 steps;
  gate must stay non-increasing, verified with `GOLDSET_MODE=vm
  BenchmarkGoldset` before/after (lesson from the resolved-bindings change:
  verify the gate, not just fib).

## Migration Plan

1. Add the two `ResourceLimits` fields with defaults resolved at `New`.
2. Extend `evalState` with reduction + allocation counters and limits; add
   the VM frame-local mirror + flush at existing sync points.
3. Wire reduction flush into both poll paths; wire ceiling checks.
4. Wire allocation charges site by site: VM make-ops → tree-walker literals
   → compiler emit → GoFunc apply sites → reader bridge. Red tests alongside
   each site.
5. Crossval terminal-class parity; `-race`; goldset gate.

## Open Questions

None blocking. Exact size-table values are fixed during implementation against
the real struct layouts and recorded in ADR 0011; they must be conservative
and stable once published.
