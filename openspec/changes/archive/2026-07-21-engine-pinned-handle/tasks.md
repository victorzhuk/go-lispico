## 1. API

- [x] 1.1 `PinnedFn` + `(*Fn).Pin()`: private VM created at pin time; `Call` routes through the shared boundary helper minus pool Get/Put; godoc states the single-goroutine contract prominently.
- [x] 1.2 Incremental reset: assert post-call invariant (empty frames, base stack), clear deviations only; full `Reset` after an errored call.
- [x] 1.3 In-use CAS guard returning a typed error on concurrent entry; never panics.

## 2. Tests

- [x] 2.1 Semantic equivalence suite: rebind visibility, delete → undefined error, stats attribution, callback events, deadline firing, re-entrant budget sharing — same assertions as `Fn.Call`, run against `PinnedFn`.
- [x] 2.2 Misuse: two goroutines hammering one `PinnedFn` — typed error observed, no race reported, engine state intact afterwards.
- [x] 2.3 Error-path recovery: a call that throws, then a correct call — full reset restores a clean VM.
- [x] 2.4 Independence: two `PinnedFn`s from one `Fn` on two goroutines run concurrently without interference.

## 3. Verify

- [x] 3.1 `go test ./...`, `-race`, crossval green; `GOLDSET_MODE=vm` gate non-increasing.
- [ ] 3.2 Benchstat ≥6: pinned vs shared handle ns/op and allocs; bench repo gains a pinned row (like-for-like with GopherLua `CallByParam`); numbers recorded against the 84 ns floor.

  In-repo evidence recorded (Ryzen AI 9 HX 370, count=6): `BenchmarkEngine_PinnedFnCall` 132.2 ns ±5% vs `BenchmarkEngine_FuncCall` 133.2 ns ±10% (p=0.669); callback variant 237.2 ns vs 243.6 ns (p=0.937); allocs identical at 1 alloc/op, 32 B/op both. Statistically indistinguishable on an uncontended micro-bench — pool Get/Put + Reset is cheaper than the ~20–30 ns estimate; the pinned handle's value is the single-owner contract, not a measurable win at this call shape. External go-lispico-bench pinned row remains a follow-up (repo not vendored here; same as engine-func-handle 4.3).
