## 0. Enforce prerequisites

- [x] 0.1 Verify `stdlib-nil-lookup-semantics` is implemented, validated, and archived into the canonical specs; verify `get-in` is no longer a reusable source entry before deleting any cache path.

## 1. Preserve cache-independent behavior

- [x] 1.1 Move eager/lazy stdlib value, Dialect isolation, concurrent construction, macro redefinition, and unload/reload assertions out of cache-stat tests; verify they pass without inspecting global cache state.
- [x] 1.2 Add a static inventory proving no stdlib bootstrap entry is reusable after `get-in` becomes a Builtin and verify the process cache receives no calls across eager/lazy startup.

## 2. Remove reusable bootstrap routing

- [x] 2.1 Remove the bootstrap entry `reusable` field and parameter; change `Env.RegisterSource` to `(name, source string)` and `LazyLayer.RegisterSource` to `(env, name, source)`, then use compiler failures to update every implementation and caller.
- [x] 2.2 Remove the cache-only `EvalStdlibBootstrap` interface/method and artifact compile/replay path; retain `core.BootstrapDefiner.DefineBootstrap` and add compile-time assertions for the core and bytecode evaluator implementations.
- [x] 2.3 Delete the bootstrap artifact map, cache key/fingerprint, bounds, locks, disable/reset hooks, statistics, and cache-only tests; verify static search finds no `reusable` field/parameter, `EvalStdlibBootstrap`, artifact/stat/control symbol, or process-cache branch.
- [x] 2.4 Verify immutable lazy source/name templates may still be shared but every compiled artifact, evaluated definition, Macro/Lambda, cell, and defining environment is Engine/environment-owned.

## 3. Rebaseline startup evidence

- [x] 3.1 Remove cache enable/disable controls from startup benchmarks, record eager/lazy construction baselines without process reuse, and verify no startup regression claim relies on deleted cache state.
- [x] 3.2 Update README/cache documentation and the exported lazy-layer migration note; verify only the per-Engine compiled-chunk cache is described.

## 4. Verify lifecycle and concurrency

- [x] 4.1 Run startup, lazy materialization, Dialect, macro, unload/reload, per-Engine cache, and concurrent Engine construction tests under normal and `-race` modes; verify behavior remains correct without global artifacts.
- [x] 4.2 Run `go test ./...`, `go test -race ./core/... ./plugins/stdlib/... ./runtime/...`, `go vet ./...`, and `golangci-lint run`; verify every command exits successfully.
