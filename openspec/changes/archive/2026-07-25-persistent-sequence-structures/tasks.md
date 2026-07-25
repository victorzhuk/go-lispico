## 1. Pin current behavior (before any code)

- [x] 1.1 Property test comparing every sequence operation (`cons`, `conj`,
      `concat`, `reverse`, `rest`, `nth`, `count`, `=`, `String`) against a
      slice-backed reference over random operation sequences — the oracle every
      later task must keep green.
- [x] 1.2 Failing test at the repro size: accumulation of 100,000 elements under
      default resource limits, both execution modes.
- [x] 1.3 Benchstat baseline: small-N construction, `let`/`fn` binding carriers,
      reader output, index reads at 32/1k/100k, accumulation shapes.

## 2. Representation

- [x] 2.1 `List` shared-tail backing behind the existing value type, flat form at
      or below threshold, `count` cached per node.
- [x] 2.2 `Vector` bit-partitioned trie with tail buffer, flat form at or below
      threshold.
- [x] 2.3 Promotion is invisible: equality, iteration order, printing, and
      immutability identical at both representations (extend 1.1 to assert across
      the threshold).
- [x] 2.4 Depth-bounded construction and collection-length limits keep their
      current semantics on shared results.

## 3. Charging

- [x] 3.1 Incremental charge for structurally derived results in
      `plugins/stdlib/collections.go` and the VM make-ops; deep charge retained
      for fresh construction (`list`, `vector`, `range`, `json/decode`).
- [x] 3.2 Retained accounting is unchanged by the representation swap — env
      bindings keep `ValueShallowBytes` (`core/env.go:177`) and the compile-time
      chunk-constant sites keep `ValueDeepBytes`. Pin both: `RetainedUsage` and
      `compiledChunkBytes` return identical totals for the same fixtures before
      and after the swap, including when the value is shared.
- [x] 3.3 Monotonicity test: repeated cons-onto-same-base in a loop charges per
      new node and stays bounded; no path charges shared substructure twice, and
      none charges it zero times at creation.

## 4. Verify

- [x] 4.1 `go test ./...` and `-race` green; crossval parity suite green on both
      dialects.
- [x] 4.2 1.2 passes at N = 100,000 with headroom under the default ledger.
- [x] 4.3 Gold set green in both modes including new accumulation cells; perfgate
      non-regression verdict against the stored VM baseline.
- [x] 4.4 Benchstat against 1.3: no regression on small-N and binding-form cells;
      accumulation cells improved asymptotically.
- [x] 4.5 Update `ARCHITECTURE.md` (value representation), `CHANGELOG.md`, and the
      metering docs with the charging rule.
