## 1. Linear json/decode (PERF-1)

- [ ] 1.1 Rewrite `fromJSONValue`'s object case in `plugins/data/plugin.go` to build one `HashMap` with in-place `Set` and return it once, instead of folding `Assoc` per key.
- [ ] 1.2 If a small shared builder helper around `HashMap.Set` reads cleaner, add it in `core` (kept internal / build-before-share); otherwise inline. Leave `Assoc`/`Dissoc` unchanged.

## 2. Verify

- [ ] 2.1 Round-trip test: encode→decode equals original; whole-number JSON decodes as `Int`.
- [ ] 2.2 Scaling test: a several-thousand-key object decodes far faster than the prior quadratic path (assert against a generous linear-ish bound, not a brittle absolute time).
- [ ] 2.3 Confirm immutability: the built map is only `Set` before return; no exposed map is mutated. `go test ./...` green.
