## 1. API

- [ ] 1.1 `PinnedFn` + `(*Fn).Pin()`: private VM created at pin time; `Call` routes through the shared boundary helper minus pool Get/Put; godoc states the single-goroutine contract prominently.
- [ ] 1.2 Incremental reset: assert post-call invariant (empty frames, base stack), clear deviations only; full `Reset` after an errored call.
- [ ] 1.3 In-use CAS guard returning a typed error on concurrent entry; never panics.

## 2. Tests

- [ ] 2.1 Semantic equivalence suite: rebind visibility, delete → undefined error, stats attribution, callback events, deadline firing, re-entrant budget sharing — same assertions as `Fn.Call`, run against `PinnedFn`.
- [ ] 2.2 Misuse: two goroutines hammering one `PinnedFn` — typed error observed, no race reported, engine state intact afterwards.
- [ ] 2.3 Error-path recovery: a call that throws, then a correct call — full reset restores a clean VM.
- [ ] 2.4 Independence: two `PinnedFn`s from one `Fn` on two goroutines run concurrently without interference.

## 3. Verify

- [ ] 3.1 `go test ./...`, `-race`, crossval green; `GOLDSET_MODE=vm` gate non-increasing.
- [ ] 3.2 Benchstat ≥6: pinned vs shared handle ns/op and allocs; bench repo gains a pinned row (like-for-like with GopherLua `CallByParam`); numbers recorded against the 84 ns floor.
