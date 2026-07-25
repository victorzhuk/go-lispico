## 1. Pin current behavior (before any code)

- [ ] 1.1 Property test comparing every sequence operation (`cons`, `conj`,
      `concat`, `reverse`, `rest`, `nth`, `count`, `=`, `String`) against a
      slice-backed reference over random operation sequences — the oracle every
      later task must keep green.
- [ ] 1.2 Failing test at the repro size: accumulation of 100,000 elements under
      default resource limits, both execution modes.
- [ ] 1.3 Benchstat baseline: small-N construction, `let`/`fn` binding carriers,
      reader output, index reads at 32/1k/100k, accumulation shapes.

## 2. Representation

- [ ] 2.1 `List` shared-tail backing behind the existing value type, flat form at
      or below threshold, `count` cached per node.
- [ ] 2.2 `Vector` bit-partitioned trie with tail buffer, flat form at or below
      threshold.
- [ ] 2.3 Promotion is invisible: equality, iteration order, printing, and
      immutability identical at both representations (extend 1.1 to assert across
      the threshold).
- [ ] 2.4 Depth-bounded construction and collection-length limits keep their
      current semantics on shared results.

## 3. Charging

- [ ] 3.1 Incremental charge for structurally derived results in
      `plugins/stdlib/collections.go` and the VM make-ops; deep charge retained
      for fresh construction (`list`, `vector`, `range`, `json/decode`).
- [ ] 3.2 Retained accounting keeps `ValueDeepBytes` — assert `RetainedUsage`
      still reflects what a binding holds alive when the value is shared.
- [ ] 3.3 Monotonicity test: repeated cons-onto-same-base in a loop charges per
      new node and stays bounded; no path charges shared substructure twice, and
      none charges it zero times at creation.

## 4. Verify

- [ ] 4.1 `go test ./...` and `-race` green; crossval parity suite green on both
      dialects.
- [ ] 4.2 1.2 passes at N = 100,000 with headroom under the default ledger.
- [ ] 4.3 Gold set green in both modes including new accumulation cells; perfgate
      non-regression verdict against the stored VM baseline.
- [ ] 4.4 Benchstat against 1.3: no regression on small-N and binding-form cells;
      accumulation cells improved asymptotically.
- [ ] 4.5 Update `ARCHITECTURE.md` (value representation), `CHANGELOG.md`, and the
      metering docs with the charging rule.
