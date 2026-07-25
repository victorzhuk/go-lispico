## 1. Pin current behavior first

- [ ] 1.1 Record the current inline verdicts as a baseline:
      `go build -gcflags='-m -m' ./core/vm/ 2>&1 | rg 'cannot inline native'`.
      Expect nativeAdd 174, nativeSub 418, nativeMul 176, nativeDiv 513,
      nativeOrder 307, nativeEq 185, all against budget 80.
- [ ] 1.2 Check whether any existing benchmark exercises the mixed int/float
      path and the N-ary path (`(+ a b c)`, `(< a b c)`). If none does, add
      one BEFORE touching the fast path, so a regression there cannot hide
      behind an improvement in the int path.
- [ ] 1.3 Capture a benchstat baseline for `Fibonacci_VM`,
      `ArithmeticLoop_VM`, `SimpleArithmetic_VM`, `Let_VM`, `TailCall_VM`
      and the goldset `loop-sum` cell. Quiet machine; `TMPDIR` outside
      `/tmp` (see `docs/profiling-baseline.md` — a quota failure at the link
      step reads like a test failure but is not one).

## 2. Fast paths

- [ ] 2.1 Add two-argument both-`Int` helpers for add, sub, mul, and the
      comparisons (lt, gt, le, ge, eq), using `core.BoxInt`
      (`core/types.go:108`) and `core.BoxBool` (`core/types.go:66`) rather
      than constructing values directly.
- [ ] 2.2 Add `nativeInt2(op, a, b) (core.Value, bool)` switching on opcode
      and returning `handled == false` for anything uncovered, so the gate
      lives in one place and unhandled operators fall through unchanged.
- [ ] 2.3 Gate it in `execNativeFastFused` (`core/vm/vm.go:1110`) ahead of
      the `execNative` call. Charge `MeterScalarBytes` and replace the stack
      exactly as the existing path does — a cheaper result must not become a
      cheaper charge.
- [ ] 2.4 `nativeDiv` LAST and separately. Mirror its current
      division-by-zero and int-vs-float result behavior exactly, or leave it
      out. Dropping it is an acceptable outcome; guessing at its semantics
      is not.

## 3. Prove it

- [ ] 3.1 Inlining confirmed:
      `go build -gcflags='-m' ./core/vm/ 2>&1 | rg 'can inline .*Int2'`
      lists every new helper. Record the output.
- [ ] 3.2 Differential test per operator: fast path and general path agree
      across a wide `int64` range including `math.MaxInt64`,
      `math.MinInt64`, and values straddling the preboxed `[-128, 1023]`
      boundary where `BoxInt` changes behavior. Overflow must wrap
      identically — do not "fix" it.
- [ ] 3.3 Mixed-type and N-ary shapes still route through the general path
      and still return identical results and identical error text.

## 4. Verify

- [ ] 4.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint` clean.
- [ ] 4.2 `go test ./...` — expect 2416 passed, 0 failed, 18 packages.
      `go test ./... -race` with `TMPDIR` outside `/tmp`; note that
      `TestDecodeHashMap_Scaling` is a known pre-existing wall-clock flake
      under full-suite race load, filed separately — if it fails, confirm it
      passes in isolation rather than treating it as yours.
- [ ] 4.3 Crossval `TestVMVsTreeWalker` 218 passed; goldset 27 passed. The
      tree-walker must be unaffected — it does not use these opcodes, so any
      movement in `_TreeWalker` cells is noise or a bug.
- [ ] 4.4 Benchstat against 1.3. Report the actual deltas rather than
      claiming a win: arithmetic-heavy VM cells should improve, mixed-type
      and N-ary cells must not regress. Timing variance above ~15% means
      re-measure; allocs/op and B/op are the deterministic signal.
- [ ] 4.5 If the measured win is negligible, say so plainly and recommend
      reverting. An inlinable helper that does not move any benchmark is not
      worth the extra code path.
