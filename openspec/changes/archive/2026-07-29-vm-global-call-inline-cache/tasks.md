# Tasks — vm-global-call-inline-cache

## 1. Pin the baseline and the semantics

- [x] 1.1 Interleaved baseline (one session, `-count=10`): fib, Rule,
      goldset both modes, `-benchmem`; `resolveGlobalValue` CPU share on
      fib. Untouched-row control.
- [x] 1.2 Write the semantic pin FIRST: crossval tests for (a) callee
      rebound between two calls of the same site — second call sees the new
      binding; (b) callee rebound *by an argument expression* of the call
      itself — the call uses the binding frozen at head resolution, exactly
      as today's sequence does (document the current behavior with a test
      against the tree-walker before touching the compiler); (c) tombstoned
      then redefined callee; (d) non-closure callee at a cached site
      (keyword, GoFunc) falls back correctly.

## 2. Implement

- [x] 2.1 Fused opcode carrying {site index, argc}; compiler emits it for
      `GET_GLOBAL` immediately followed by `CALL` where the global is the
      callee (not an argument). Tail-call variant included
      (`GET_GLOBAL`+`TAILCALL`) — fib's shape is non-tail but the pattern
      is the same and loop-heavy code benefits.
- [x] 2.2 Dispatch: read the site's versioned snapshot (the existing
      lock-free hit path); hit + `*Closure` → arity-check and push the
      frame directly (skip the value-stack callee push and the apply type
      switch); anything else → exact current resolution + generic call.
- [x] 2.3 Guard audit: enumerate every mutation that can change what the
      site would resolve to — `Set`, `Delete`+tombstone reuse, `Rebuild`
      remap, hot-reload `MergeInto`, function-cell creation (the NameGen
      gap) — and show each one either bumps the cell version the snapshot
      carries or invalidates the site. Add the missing bump/invalidation if
      any path escapes (this closes the func-cell-gap hazard for this
      cache; do not widen `NameGen` semantics here).
- [x] 2.4 `Chunk.Validate` cases for the new opcode(s): site index, argc
      vs stack depth, non-fall-through rules.
- [x] 2.5 Namespace-key the site table: value-position `OpGetGlobal f` and
      a Lisp-2 fused function-head `(f ...)` share one `siteCache` keyed by
      symbol constant, so a value-cell entry can be served to a function
      head (and a function-cell entry published by `resolveFusedFuncCall`
      can be served to a value read). Key sites by {symbol, namespace} and
      pin both orders with distinct value/function cells.

## 3. Verify

- [x] 3.1 Crossval `TestVMVsTreeWalker` + the 1.2 pins green.
- [x] 3.2 Hot-reload + UnloadPlugin scenarios from the resolved-globals
      requirement re-run against fused sites.
- [ ] 3.3 Full floor: build, vet, lint, full suite, `-race`, goldset both
      modes non-increasing.
- [ ] 3.4 Interleaved benchstat vs 1.1: fib −4% or better, no regressions;
      record hit-rate instrumentation numbers (temporary counter, removed
      before merge) in design.md.
