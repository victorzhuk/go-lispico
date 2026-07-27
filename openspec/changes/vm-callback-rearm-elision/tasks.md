# Tasks — vm-callback-rearm-elision

## 1. Pin the baseline

- [ ] 1.1 Interleaved baseline (one session, `-count=10`): Callback, Rule,
      Call (control), `-benchmem`; `RearmReentrantEvalState` and
      `reentrantCtx` profile shares on Callback (18.2% / 21.6% cum at HEAD
      2026-07-27).

## 2. Delta rearm

- [ ] 2.1 Wrapper remembers its last-armed configuration (limits, timeout,
      meter-snapshot identity, seed baseline). Enumerate EVERY field the
      full rearm writes (core/eval.go:465-482) and classify: config
      (compare, skip when equal), per-run seed (write only when differing
      from baseline), generation (always).
- [ ] 2.2 Same-config fast rearm: generation stamp + differing seeds only.
      Config comparison itself must be cheaper than the stores it saves —
      plain loads against the remembered values.
- [ ] 2.3 Zero-seed baseline: top-level boundary calls seed zeros; skip
      those stores when the VM's counters are at zero. Non-zero seeds
      (nested/reentrant shapes) take the full write.
- [ ] 2.4 Field-by-field invalidation test: change each config field
      between two calls (limits via options, timeout, meter attach/detach)
      and assert the wrapper serves the new value on the next dispatch.

## 3. Verify

- [ ] 3.1 Reentrant suite green under `-race`, including the hostile
      retained-ctx tests — the generation guard must behave identically.
- [ ] 3.2 Crossval + full floor: build, vet, lint, full suite, goldset
      both modes non-increasing.
- [ ] 3.3 Interleaved benchstat vs 1.1: Callback −15% or better
      standalone; Call control flat; Rule non-regressing.
