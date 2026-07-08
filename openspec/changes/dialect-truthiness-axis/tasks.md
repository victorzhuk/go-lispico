# Tasks — Dialect truthiness axis

## 1. Red

- [ ] 1.1 At the `runtime` Engine seam, add a failing test: an Engine whose Dialect sets `nil`-only truthiness evaluates `(if false :yes :no)` to `:yes`, while the default Dialect evaluates it to `:no`. Acceptance: red — the axis does not exist.
- [ ] 1.2 Add failing coverage that the same divergence holds through `when`, `unless`, `cond`, `and`, `or`, and `not`. Acceptance: red.

## 2. Implement

- [ ] 2.1 Add a truthiness setting to the Dialect (`nil`-only vs `nil`+`false`). Acceptance: the setting is resolvable from the Engine's Dialect.
- [ ] 2.2 Route the conditional special forms through a single truthiness hook derived from that setting. Acceptance: no conditional form hardcodes the falsy rule.
- [ ] 2.3 Keep the identity Dialect at `nil`+`false`. Acceptance: existing conditional tests stay green.

## 3. Verify

- [ ] 3.1 1.1 and 1.2 green; full suite green. Acceptance: `go test ./...` passes.
- [ ] 3.2 `openspec validate dialect-truthiness-axis --strict` passes.
