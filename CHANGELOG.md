# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Breaking:** vector `[...]` and map `{...}` literals now evaluate their
  elements, so `(let [x 99] [1 x])` yields `[1 99]`. Use `quote` to keep a
  literal unevaluated.
- Evaluation errors are now `*core.LispicoError` values carrying a `Code`, so
  `errors.As` and `errors.Is` work against them.
- The bytecode VM (`runtime.WithBytecode()`) is an opt-in optimizer for a
  documented subset of forms; forms it cannot compile fall back to the
  tree-walking evaluator instead of failing.

### Removed

- `runtime.WithBytecodeCache` — the on-disk bytecode cache was never used on the
  evaluation path and has been removed.

### Fixed

- Concurrent `Eval` on a single engine no longer shares call/loop/macro depth
  state across goroutines; `recur` outside a loop is detected reliably and macro
  expansion is race-free.
- The bytecode VM no longer panics on an empty-body function such as `((fn []))`.
- `WithTimeout` now bounds `Eval` and `EvalWithBindings`, not only `Call`.
- `Watch` stops when the context passed to it is cancelled.

### Security

- The `lio` file sandbox resolves symlinks before enforcing its root, closing an
  escape that allowed reads and writes outside the sandbox via an intermediate
  symlink.
- HTTP responses in the `net` plugin are read under a size cap to prevent
  memory exhaustion.

## [0.1.0] - 2026-07-04

### Added

- Core Lisp interpreter with zero external dependencies: 13 value types,
  22 special forms, lexical scoping, and immutable data structures.
- Tree-walking evaluator with explicit `loop`/`recur` tail-call optimization
  and configurable max eval depth.
- Bytecode compiler and stack-based VM covering the same 22 special forms,
  enabled with `runtime.WithBytecode()`; on-disk compilation cache via
  `runtime.WithBytecodeCache(dir)`.
- Go embedding API (`runtime.Engine`): `Eval`, `EvalFile`, `Call`, `Bind`,
  plugin loading with `Use`/`UnloadPlugin`/`ReloadPlugin`, REPL, runtime
  stats, and eval/plugin-call event callbacks.
- Hot reload: watch a directory and re-evaluate changed `.lisp` files.
- Plugins: `stdlib` (arithmetic, collections, strings), `llm` (LLM API
  bindings), `agent` (agent orchestration), `lio` (sandboxed file I/O and
  environment), `net` (HTTP client), `exec` (shell execution and crypto),
  `data` (JSON), `fsm` (finite state machines).

[unreleased]: https://github.com/victorzhuk/go-lispico/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/victorzhuk/go-lispico/releases/tag/v0.1.0
