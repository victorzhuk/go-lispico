# Tasks — vm-callback-rearm-elision

## 1. Pin the baseline

- [x] 1.1 Interleaved baseline (one session, `-count=10`): Callback, Rule,
      Call (control), `-benchmem`; `RearmReentrantEvalState` and
      `reentrantCtx` profile shares on Callback (18.2% / 21.6% cum at HEAD
      2026-07-27).

## 2. Delta rearm

- [x] 2.1 Wrapper remembers its last-armed configuration (limits, timeout,
      meter-snapshot identity, seed baseline). Enumerate EVERY field the
      full rearm writes (core/eval.go:465-482) and classify: config
      (compare, skip when equal), per-run seed (write only when differing
      from baseline), generation (always).
- [x] 2.2 Same-config fast rearm: generation stamp + differing seeds only.
      Config comparison itself must be cheaper than the stores it saves —
      plain loads against the remembered values.
- [x] 2.3 Zero-seed baseline: subsumed by the live-atomic comparison — a
      seed equal to the counter's live value (zero or not) skips the
      store, which covers the zero-seed top-level shape exactly.
- [x] 2.4 Field-by-field invalidation test: change each config field
      between two calls (limits via options, timeout, meter attach/detach)
      and assert the wrapper serves the new value on the next dispatch.
      (`TestRearmReentrantEvalState_FieldByFieldInvalidation`, plus
      same-config-identical and seed-residue-leak pins.)

## 3. Verify

- [x] 3.1 Reentrant suite green under `-race`, including the hostile
      retained-ctx tests — the generation guard must behave identically.
- [x] 3.2 Crossval + full floor: build, vet, lint, full suite, goldset
      both modes non-increasing (2026-07-29, composed run).
- [x] 3.3 Interleaved benchstat vs master (composed with the frame/boundary
      changes, n=8): CallBytecodeCallback −12.7%, FuncCallCallback −9.2%,
      PinnedFnCallCallback −14.4%; Call control improved by the sibling
      changes; goldset Rule row flat. The −15% standalone bar is close but
      final judgment belongs to the release-runner gate (local perfgate is
      not evaluable on this box).
