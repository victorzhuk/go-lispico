# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `runtime.Engine.Func(name)` returns a reusable `*runtime.Fn` handle for
  repeated calls to a resolved function binding.
- `runtime.ResourceLimits` now includes `MaxRetainedBytesPerEnv` and
  `MaxRetainedSlotsPerEnv`: per-`Env` retained-state capacity ceilings
  (default 32 MiB / 100,000 slots), enforcing owned-capacity accounting
  on every env created through the engine (ADR 0012).
- `(*Env).RetainedUsage() (bytes, slots int64)` returns the env's current
  retained backing usage for embedder ledger settlement.
- `(*Env).Rebuild()` compacts a scope in place — fresh maps, live cell
  pointers preserved, tombstoned cells dropped — the only path that
  releases dead backing (ADR 0012).
- `runtime.ResourceLimits` now includes `MaxCacheBytes` (default 64 MiB)
  and `MaxCacheNodes` (default 1,000,000), and `EngineStats.Cache` reports
  chunk-cache entries, bytes, nodes, and epoch.
- `Engine.LoadScope(ctx, source, bindings) (Value, *Env, error)` evaluates
  source with bindings and returns the child scope so the embedder owns
  its lifecycle: usage probe, rebuild, or retirement.

### Changed

- **Breaking:** Removed `core.Dialect.NilOnlyFalsy()`. All dialects now treat
  `nil` and `false` as falsy, including the Common Lisp dialect.

- `runtime.New()` now defaults to bytecode VM execution, with form-by-form fallback to
  the tree-walking evaluator for unsupported forms (for example `defmacro` nested in a
  body, `unquote-splicing`).
- Added `runtime.WithTreeWalker()` as the documented rollback path from the VM-default
  behavior; VM/tree-walker mode options are last-wins.
- `core.AdoptEvalState` now returns a lightweight context wrapper that materializes
  the re-entrant evaluation state on first use instead of allocating it eagerly: a
  `GoFunc` that never re-enters the evaluator pays one small allocation instead of
  the state + derived-context pair. Re-entrant resource budgets (structural depth,
  deadline) are shared with the enclosing run exactly as before, and a context
  retained past its call stays snapshot-consistent — it never observes a recycled
  pooled VM.
- `runtime.WithResourceLimits` now also bounds per-evaluation reductions
  (`MaxReductions`, default `10_000_000`) and cumulative shallow allocation
  (`MaxAllocationBytes`, default `64 MiB`). Reader output, macro expansion,
  compiler emission, evaluator work, VM make-ops, and `GoFunc` dispatch/results
  all charge the same evaluation ledger, and limit breaches remain terminal
  `ResourceLimitError`s.
- The per-engine bytecode chunk cache now evicts deterministically by LRU across
  entry, deep-byte, and expanded-node ceilings. Chunks larger than any cache
  ceiling run uncached without failing evaluation.

## [0.8.0] - 2026-07-19

### Changed

- Canonical arithmetic and comparison operators (`+ - * / < > <= >= =`) now
  compile to native VM opcodes under any dialect, not only dialects using
  identity names. Under a Lisp-2 dialect the function cell tracks canonical
  status (cleared on `defun` or any rebind), so the VM keeps tree-walker
  parity when an operator is redefined.
- `HashMap` storage is now a sorted small-array form (≤8 keys) that promotes
  to a hashed form on the 9th distinct key, replacing a double Go-map layout;
  keys hash on bit patterns instead of formatted strings. Numeric-key
  iteration order is now bit-pattern rather than lexicographic-on-string; NaN
  keys canonicalize to one bucket and `+0.0`/`-0.0` fold to one key, matching
  `Float.Equals`.
- `OpGetGlobal` resolves through a binding cell cached on the chunk instead of
  walking the scope chain on every read.
- The VM validates a chunk's structure (constant indices, jump/loop/handler
  targets, sub-chunk references, local slots) once at load instead of
  checking on every instruction; the never-panics contract is unchanged.
- Small integers (`-128..1023`) and booleans reuse preboxed shared values
  instead of allocating on every arithmetic/comparison result.
- Cancellation checks in both evaluators now run on a batched countdown
  budget plus loop-back-jump/call boundaries instead of every
  instruction/node; the Engine's default deadline is carried as a
  precomputed instant instead of a `context.WithTimeout` per `Eval`/`Call`,
  so no timer or derived context is allocated per call.
- **Breaking:** A `GoFunc` invoked during evaluation now receives the
  caller's context unwrapped, rather than one bound to the Engine's deadline.
  It observes the Engine deadline only as the error the evaluator returns
  once the call completes, not as a context cancellation; a `GoFunc`
  blocking on external work (a network call, a file read) is bounded by the
  caller's context, not interrupted mid-call by the Engine deadline (amends
  ADR 0010).
- `vm.apply` enters the call protocol directly instead of synthesizing a
  per-call wrapper chunk; call observability (timing, events, per-function
  counts) goes lazy when no callback is registered.

## [0.7.0] - 2026-07-18

### Changed

- `cond` clause shape is dialect-owned: each dialect decides how `cond` clauses
  are structured, rather than the kernel imposing a single shape.

### Fixed

- The bytecode VM now binds kernel `let` in parallel, matching the tree-walking
  evaluator — every binding init resolves in the scope enclosing the `let`,
  never in a sibling binding. `let*` stays sequential.
- Restored VM parity with the tree-walker for keyword application (`(:key m)`)
  and structural-depth limits.
- `when` and `unless` push `nil` on the skipped branch under the VM, matching
  the evaluator in expression position.
- `set!` targets its lexical owner under the VM (new `OpSetLexical`), so
  mutating a captured binding updates the owning frame, not a local slot.
- The `try`/`catch` error binding is scoped to handler entry, so it is not
  visible to the guarded body.
- Special-form arity and shape are validated before argument indexing, and
  malformed special forms now fail with a typed `*core.LispicoError` instead of
  a panic or an untyped error.
- `stdlib` `merge` builds its result through a mutable bulk-builder.
- The runtime skips its redundant Engine deadline timer when the caller's
  context deadline already governs the evaluation.

## [0.6.0] - 2026-07-11

### Added

- `cmd/lispico` interactive REPL binary with raw-mode line editing, multiline
  continuation, persistent history, `-dialect`/`-bytecode` flags, and file
  execution mode. Built on `golang.org/x/term`.

- `runtime.WithResourceLimits` sets reader nesting-depth, evaluator structural-depth, collection-length, and chunk-cache-size ceilings. Immutable after construction; conservative defaults ship. Deeply nested source/literals/quasiquote, oversized `range`, and unbounded chunk growth fail closed with `*core.LispicoError` (`Code: "ResourceLimitError"`) instead of crashing or exhausting memory. Structural depth is enforced at evaluation time in both evaluators so they agree (a dead-branch over-limit literal is not rejected).

### Changed

- **Breaking:** `compiler.MacroExpander` and `CompileExpanded` have been removed.
  The compiler now expands macros inline during compilation; callers that relied
  on the separate expansion entrypoint should use the normal `Compile` path.
- **Breaking:** Reader, evaluator, and VM errors are now typed as
  `*core.LispicoError` and carry the failing form's source position (`Line`,
  `Col`, `Source`) when available. Code that checks for specific error types
  should use `errors.As` with `*core.LispicoError`.
- `runtime.UnloadPlugin` and `runtime.ReloadPlugin` now clear a plugin's
  bindings from both the value cell and the function cell (Lisp-2), so
  previously registered functions are no longer callable after unload.
- `runtime` REPL input balancing now skips `;` comments outside strings,
  matching `readComment` behavior.
- The bytecode VM's `Engine.Call`/`Apply` path reuses pooled VM instances
  instead of allocating a fresh VM per call, matching the pooled `Eval` path;
  no per-call VM allocation remains on either path (completes ADR 0006).
- JSON decoding in the `data` plugin builds each decoded object's hash map in a
  single linear pass instead of repeated immutable `Assoc`, removing the
  quadratic rebuild cost on large objects.

### Fixed

- Bootstrap macros (`->`, `->>`, `as->`, `if-let`, `when-let`, `get-in`) now
  resolve correctly in head position under the Common Lisp Lisp-2 dialect.
- Invalid number literals such as `1.2.3` are rejected and report the token's
  source position.

## [0.5.0] - 2026-07-10

### Added

- `core.Dialect.Vocabulary(map[string]string)` and `core.Dialect.WithAdapter(name, fn)`:
  bind dialect-visible names to shared builtin implementations registered by
  plugins, with thin adapters for semantics-differing names. On an
  `EmptyDialect` the vocabulary is an allowlist: a builtin whose registered
  name is absent from the map is uncallable, and a builtin added later does
  not leak in. The identity Dialect is unchanged; existing embedders see no
  difference.
- `runtime.WithDialect(d core.Dialect)` — explicitly select the Lisp dialect
  for an Engine. The `clojure` and `cl` packages ship the two composed
  dialects: `WithDialect(clojure.Dialect())` selects the pre-flip Clojure
  surface; `WithDialect(cl.Dialect())` selects the Common Lisp surface.

### Changed

- **Breaking:** An Engine created via `runtime.New()` without `WithDialect`
  now runs the Common Lisp dialect. Embedders that need the prior Clojure
  surface select it explicitly with `WithDialect(clojure.Dialect())`.
- **Breaking:** `New(nil, WithBytecode())` (no `WithDialect`) now errors at
  construction because the bytecode VM requires an identity dialect. The CL
  default is non-identity. Pin to `WithDialect(clojure.Dialect())` to keep
  using the bytecode VM.

## [0.4.2] - 2026-07-09

### Added

- `core.DetachEvalState(ctx) context.Context`: returns ctx with a fresh
  `evalState` attached, preserving cancellation and any other context values.
  Embedders that start a new evaluation goroutine (e.g. a routine scheduler)
  call this so the goroutine owns its own depth counters and cannot race or
  trip `MaxDepth` against the caller.

## [0.4.1] - 2026-07-08

### Fixed

- Harden `evalState` depth counters with `atomic.Int64`. The ctx-scoped
  `evalState` introduced in v0.3.0 gives concurrent top-level `Eval` calls
  independent counters; the atomic conversion closes the remaining race when
  the same `context.Context` is reused across goroutines.

## [0.4.0] - 2026-07-06

### Added

- Optional exception-class slot in `catch` clauses: `(try ... (catch Exception e handler))`.
  The class symbol is accepted and ignored (no type dispatch); the binding and handler
  follow. Backfills the entry missing from the v0.4.0 tag (commit 78d46c3).

## [0.3.0] - 2026-07-04

### Added

- Comparison and equality builtins in stdlib: `=` is structural equality via
  `Equals` (so `(= 1 1.0)` is false); `<`, `>`, `<=`, `>=` are variadic
  monotonic chains over numbers, comparing int pairs exactly and mixing int
  and float by the same promotion arithmetic uses.
- Collection builtins in stdlib: `contains?` (map key presence), `merge`
  (later maps win, nil skipped), `dissoc`, `sort` (stable natural ordering of
  numbers, strings, or keywords), and `range`.

### Changed

- The world-touching plugins — `llm`, `agent`, `lio`, `net`, `exec` — are
  frozen: security and correctness fixes only. Hosts are expected to register
  their own IO surface (see `docs/adr/0004-kernel-first-mission.md`).

## [0.2.0] - 2026-07-04

### Changed

- **Breaking:** vector `[...]` and map `{...}` literals now evaluate their
  elements, so `(let [x 99] [1 x])` yields `[1 99]`. Use `quote` to keep a
  literal unevaluated.
- Evaluation errors are now `*core.LispicoError` values carrying a `Code`, so
  `errors.As` and `errors.Is` work against them.
- The bytecode VM became selectable through `runtime.WithBytecode()` for a
  documented subset of forms; forms it cannot compile fall back to the
  tree-walking evaluator instead of failing.
- `io/env-set` writes to a per-plugin overlay instead of the process
  environment; `io/env-get` reads the overlay and falls through to the real
  environment. `exec/run` with `:inherit-env` no longer sees variables set by
  `io/env-set`.

### Removed

- `runtime.WithBytecodeCache` — the on-disk bytecode cache was never used on the
  evaluation path and has been removed.
- `runtime.WithHotReloadDir` — the option never started watching; call
  `Watch(ctx, dir)` to enable hot reload.

### Fixed

- Concurrent `Eval` on a single engine no longer shares call/loop/macro depth
  state across goroutines; `recur` outside a loop is detected reliably and macro
  expansion is race-free.
- The bytecode VM no longer panics on an empty-body function such as `((fn []))`.
- `WithTimeout` now bounds `Eval` and `EvalWithBindings`, not only `Call`.
- `Watch` stops when the context passed to it is cancelled.

### Security

- The `lio` file sandbox resolves symlinks before enforcing its root, closing an
  escape that allowed reads and writes outside the sandbox through a symlink.
- HTTP responses in the `net` plugin are read under a size cap to prevent
  memory exhaustion.

## [0.1.0] - 2026-07-04

### Added

- Core Lisp interpreter with zero external dependencies: 13 value types,
  22 special forms, lexical scoping, and immutable data structures.
- Tree-walking evaluator with explicit `loop`/`recur` tail-call optimization
  and configurable max eval depth.
- Bytecode compiler and stack-based VM covering the same 22 special forms,
  selectable with `runtime.WithBytecode()`; on-disk compilation cache via
  `runtime.WithBytecodeCache(dir)`.
- Go embedding API (`runtime.Engine`): `Eval`, `EvalFile`, `Call`, `Bind`,
  plugin loading with `Use`/`UnloadPlugin`/`ReloadPlugin`, REPL, runtime
  stats, and eval/plugin-call event callbacks.
- Hot reload: watch a directory and re-evaluate changed `.lisp` files.
- Plugins: `stdlib` (arithmetic, collections, strings), `llm` (LLM API
  bindings), `agent` (agent orchestration), `lio` (sandboxed file I/O and
  environment), `net` (HTTP client), `exec` (shell execution and crypto),
  `data` (JSON), `fsm` (finite state machines).

[unreleased]: https://github.com/victorzhuk/go-lispico/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/victorzhuk/go-lispico/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/victorzhuk/go-lispico/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/victorzhuk/go-lispico/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/victorzhuk/go-lispico/compare/v0.4.2...v0.5.0
[0.4.2]: https://github.com/victorzhuk/go-lispico/compare/v0.4.1...v0.4.2
[0.4.1]: https://github.com/victorzhuk/go-lispico/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/victorzhuk/go-lispico/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/victorzhuk/go-lispico/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/victorzhuk/go-lispico/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/victorzhuk/go-lispico/releases/tag/v0.1.0
