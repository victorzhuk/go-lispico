## 1. Pin current semantics (red for the new cases, green for existing)

- [x] 1.1 Tests pin `merge` behavior before touching the implementation: `(merge)` and all-nil arguments, right-most precedence on duplicate keys, input maps unchanged after the call, deterministic iteration of the result, and the existing type error for non-map arguments.

## 2. Rebuild on the bulk builder

- [x] 2.1 Rewrite `merge` to accumulate through the mutable bulk-builder escape hatch and return the finished map; all pinned tests stay green.

## 3. Prove the growth fix

- [x] 3.1 Benchmark `merge` over increasing map sizes recording `ns/op`, `B/op`, `allocs/op`; assert bytes and allocation growth is roughly linear, not quadratic.
- [x] 3.2 Record the before/after benchstat comparison in the change as Shared-path fix evidence, separate from any VM numbers.

## 4. Verify

- [x] 4.1 `go test ./plugins/stdlib/...` and full `go test ./...` green; `golangci-lint run` clean.
