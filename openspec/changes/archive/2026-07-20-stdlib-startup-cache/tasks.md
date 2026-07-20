## 1. Profile

- [x] 1.1 CPU+alloc profile of a bytecode-startup loop; attribute the 120 µs across read / macro-expand / compile+validate / env population / registration; record in this change's design notes and pick the reuse boundary.

## 2. Process-level artifact reuse

- [x] 2.1 Package-level bounded artifact map keyed (dialectFP, sourceHash), populated on first plugin load, applied only on the plugin-load path.
- [x] 2.2 Verify the reproducible-expansion constraint for stdlib's pure-Lisp forms (expansion depends only on dialect + stdlib source); exclude any violating form from reuse.
- [x] 2.3 Per-engine shallow chunk-tree copy with fresh site tables; shared immutable slices (`Code`, `Constants`, `LocalNames`, `Captured`); recursive `SubChunks` copy.
- [x] 2.4 Test hook to clear/disable the process cache so suites can compare reuse-on vs reuse-off.

## 3. Isolation tests

- [x] 3.1 Two engines, same dialect: `def` in A invisible in B; stdlib functions in both work; `-race` clean under concurrent construction.
- [x] 3.2 `UnloadPlugin` in one engine leaves the other intact.
- [x] 3.3 Different dialects (CL vs Clojure) get distinct artifacts; goldset + crossval identical with reuse on and off.
- [x] 3.4 Cache ceiling: constructing engines across many synthetic dialect fingerprints stays bounded.

## 4. Verify

- [x] 4.1 `go test ./...`, `-race`, crossval, both goldset modes green.
- [x] 4.2 Bench evidence (benchstat ≥6): startup ns/op and allocs under the bytecode default, first-engine vs warm-process; target ≤ ~40 µs warm.
