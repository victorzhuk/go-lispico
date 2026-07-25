## 1. Pin the current cost

- [x] 1.1 Paired gold-set capture before any edit: `GOMAXPROCS=2`,
      `-benchtime=200ms`, `-count=10`, both `GOLDSET_MODE` values, quiet
      machine (`uptime`; an unrelated corpus server bursts on this box).
      `TMPDIR` outside `/tmp` — a quota failure at the link step reads like a
      test failure but is not one.
- [x] 1.2 Record allocs/op per fixture. That is the deterministic signal this
      change is judged on; timing here is too noisy for a few percent to mean
      anything.

## 2. The fix

- [x] 2.1 In `resolveHead` (`core/eval.go`), short-circuit a head naming a
      special form, returning "not a macro" without evaluating it. Use
      `e.forms`, the same table `evalList` dispatches on (`core/eval.go:471`)
      and that `core/eval.go:1147` already gates on.
- [x] 2.2 Place it AFTER the Lisp-2 function-cell lookup and BEFORE
      `e.Eval`. Order is the whole design: under Lisp-2 a user macro may
      shadow a special-form name through the function cell, and that lookup
      must keep winning. Note the existing `if sym, ok := head.(Symbol); ok
      && e.lisp2` folds two conditions into one — the symbol assertion is
      needed by both checks, the `lisp2` guard by only one.
- [x] 2.3 Comment the WHY, not the what: that special forms are dispatched
      by `evalList` and are never values, so evaluating one only builds an
      error to discard.

## 3. Prove it

- [x] 3.1 `TestEval_MacroExpand_UndefinedHead` green — a genuinely undefined
      head must still report unresolved. The short-circuit fires only for
      names in `e.forms`, which an undefined symbol is not.
- [x] 3.2 Add or extend a test covering a Lisp-2 function-cell macro bound
      under a special-form name, asserting the function cell still wins. If
      the crossval suite already covers this, say which case and skip.
- [x] 3.3 Crossval `TestVMVsTreeWalker` 218 passed — it covers CL and
      Clojure dialects including function-cell rebinds.

## 4. Measure

- [x] 4.1 Paired capture after, same parameters. Report allocs/op deltas per
      fixture.
- [x] 4.2 Expected shape, and the check that the fix hit what it claims:
      `guard-nil` and `rule-load` lose allocations in proportion to their
      special-form-headed top-level forms (2 and 11 per iteration);
      `safe-parse` and `registry-fold` improve slightly; `pipeline` does not
      move at all, having no special-form head. If `pipeline` moves, the fix
      is doing something other than advertised — report that rather than
      accepting the win.
- [x] 4.3 Report whether `guard-nil` and `rule-load` reach parity with the
      tree-walker or merely narrow the gap. Both are useful answers; the
      remaining gap is attributable to per-form VM setup cost, filed
      separately.

## 5. Verify

- [x] 5.1 `go build ./...`, `go vet ./...`, `gofmt -l .`, `make lint` clean.
- [x] 5.2 `go test ./... -count=1` — expect 2420 passed, 0 failed.
      `go test ./... -race` with `TMPDIR` set;
      `TestDecodeHashMap_Scaling` is a known pre-existing wall-clock flake
      under full-suite race load, filed separately — confirm it passes in
      isolation rather than treating it as yours.
- [x] 5.3 `go test ./internal/goldset/ -count=1` — 27 passed.
