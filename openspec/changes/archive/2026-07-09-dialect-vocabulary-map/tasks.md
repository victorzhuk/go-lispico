# Tasks — Dialect vocabulary name map

## 1. Red

- [x] 1.1 At the `runtime` Engine seam, add a failing test: under a Dialect mapping `car` → the shared first-implementation, `(car '(1 2 3))` returns `1`, and the mapped-away original name behaves per the Dialect. Acceptance: red.
- [x] 1.2 Add a failing test that an empty-base Dialect whose vocabulary map omits a builtin makes that builtin uncallable. Acceptance: red.
- [x] 1.3 Add a failing test that an adapter (differing argument order/arity from the shared core) resolves to the shared implementation, not a duplicate. Acceptance: red.

## 2. Implement

- [x] 2.1 Ensure each covered operation has one shared implementation in the stdlib core; remove any name-driven duplication. Acceptance: one implementation per operation.
- [x] 2.2 Add the vocabulary name map to the Dialect and apply it during resolution to bind visible names to shared implementations. Acceptance: 1.1 green.
- [x] 2.3 Make empty-base vocabulary an allowlist. Acceptance: 1.2 green.
- [x] 2.4 Support thin adapters for semantics-differing names over the shared core. Acceptance: 1.3 green.

## 3. Verify

- [x] 3.1 Full suite green including stdlib tests. Acceptance: `go test ./...` passes.
- [x] 3.2 `openspec validate dialect-vocabulary-map --strict` passes.
