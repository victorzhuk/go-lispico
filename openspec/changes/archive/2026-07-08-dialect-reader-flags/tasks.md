# Tasks — Dialect reader feature flags

## 1. Red

- [x] 1.1 At the `runtime` Engine seam, add a failing test: under a Dialect with `#'` enabled, `#'foo` parses to the function-reference form; under the default Dialect it does not. Acceptance: red.
- [x] 1.2 Add a failing test that `#(...)` parses to a vector form when the flag is on. Acceptance: red.
- [x] 1.3 Add a failing test that with bracket literals disabled, `[1 2]` does not read as a vector literal, and with them enabled it does. Acceptance: red.

## 2. Implement

- [x] 2.1 Add reader-options to the Dialect: `[..]`/`{..}` literals, `#'`, `#(...)`. Acceptance: resolvable from the Engine's Dialect.
- [x] 2.2 Thread the flags into the tokenizer and parser so reading honors the running Dialect. Acceptance: 1.1, 1.2, 1.3 green.
- [x] 2.3 Keep the identity Dialect at current flags (brackets on, `#'`/`#(...)` off). Acceptance: existing `reader_test.go` green.

## 3. Verify

- [x] 3.1 Full suite green. Acceptance: `go test ./...` passes.
- [x] 3.2 `openspec validate dialect-reader-flags --strict` passes.
