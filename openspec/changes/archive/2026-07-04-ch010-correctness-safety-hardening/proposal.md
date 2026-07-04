# Change Proposal: Correctness and safety hardening

## Why

A documentation-versus-code audit of the alpha found the codebase promises
guarantees it does not hold, and ships three defects reachable from ordinary use.
The project positions itself as an embeddable scripting kernel; such a kernel must
not panic on valid-looking input, must not let a script escape its sandbox, and
must not advertise options that do nothing.

Three critical defects:

- **The bytecode cache is dead code.** `WithBytecodeCache(dir)` has no runtime
  effect — nothing on the evaluation path calls `BytecodeCache.Load`/`Store`. The
  whole type (gob registration, versioning, atomic rename) and its nine tests are
  unreachable, and the one end-to-end file-load benchmark shows the "cached" path
  slower than the tree-walker. The archived ch009 marked the cache-hit path
  complete; it was not.
- **The VM panics on an empty-body function.** `((fn []))` — and `defn` with an
  empty body — compiles to a bare `OpReturn` and underflows the VM stack:
  `index out of range [-1]`. The tree-walker returns a clean arity error. This
  falsifies the "no panics" guarantee from ordinary source, no corruption required.
- **The file sandbox can be escaped by a symlink.** `withinRoot` compares an
  unresolved path string and never calls `filepath.EvalSymlinks`, so an
  intermediate symlink inside the root resolves out of it at the OS layer —
  arbitrary read/write outside the sandbox. `DenyPattern` is bypassable the same way.

Beyond the criticals, several headline guarantees are false: concurrent evaluation
on one engine races and leaks `recur`/depth state across goroutines; nearly every
eval error is an untyped `fmt.Errorf` despite the documented `*LispicoError`-with-
`Code` contract; `WithTimeout` is never applied to `Eval`; `WithHotReloadDir` is a
no-op; `Watch` ignores its `ctx`. And a core semantics gap: vector/map literals do
not evaluate their elements, contradicting quasiquote and every mainstream Lisp.

## What Changes

Work is sequenced criticals-first. The load-bearing decisions are recorded in
`docs/adr/0001`–`0003` and `CONTEXT.md`.

- **Criticals.** Remove the dead `BytecodeCache` (ADR-0002); fix the empty-body VM
  panic and its `defn` desugaring; resolve symlinks before the sandbox root and
  deny-pattern checks.
- **VM parity and safety (ADR-0002).** Forms the VM does not compile (`defmacro`
  nested in a body, `unquote-splicing`) return a typed error and defer to the
  tree-walker instead of failing opaquely or panicking; `throw` binds the same
  runtime value under both evaluators; VM stack indexing is bounds-checked.
- **Concurrency (ADR-0003).** Macro-expansion depth, call depth, and the
  `recur`/loop counter move to per-evaluation state, so concurrent `Eval` on one
  engine is correct and race-free.
- **Error contract.** Eval-time errors become `*LispicoError` carrying a `Code`
  and, where available, a source position, so `errors.As` works.
- **Literal semantics (ADR-0001).** Vector `[...]` and map `{...}` literals
  evaluate their elements; quasiquote gains the missing map case.
- **Runtime API honesty.** `WithTimeout` applies to `Eval`; `WithHotReloadDir`
  either starts watching or is removed; `Watch` honors its `ctx`; the reversed
  `Engine.Eval` doc comment is corrected.
- **Targeted robustness.** Cap the `net` response-body read; `Wait()` on a failed
  `exec/pipe` stage; isolate or document `io/env-*` process-global mutation.
- **Docs.** ARCHITECTURE.md, CLAUDE.md, and README are brought in line with what
  the code guarantees.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: reframed from a claimed full-language evaluator to an opt-in
  hot-loop optimizer that is a documented subset — errors and defers on
  unsupported forms, never panics — with the unreachable on-disk cache removed.
- `core-engine`: gains enforceable requirements for concurrent-evaluation safety,
  the typed-error contract, and literal-element evaluation (the placeholder spec
  is replaced with real requirements).
- `runtime-api`: options that were no-ops (`WithTimeout` on `Eval`,
  `WithHotReloadDir`, `Watch` ctx) now behave as documented or are removed.
- `io-plugin`: the sandbox becomes a real security boundary that resolves symlinks
  before enforcement.

## Impact

- **Code:** `core/eval.go` (per-evaluation depth/loop state, literal element
  evaluation, quasiquote map case, typed errors), `core/error.go` (eval-time
  position), `core/vm/vm.go` + `core/compiler/compiler.go` (empty-body guard,
  unsupported-form error+defer, throw parity, bounds checks), `core/vm/cache*.go`
  (removal), `runtime/engine.go` + `runtime/eval.go` + `runtime/watch.go` (timeout
  wiring, hot-reload option, Watch ctx, doc fix), `plugins/lio/sandbox.go` (symlink
  resolution), `plugins/net/response.go`, `plugins/exec/exec.go`,
  `plugins/lio/environment.go`, plus tests.
- **Public API:** `WithBytecodeCache` is removed (was inert); `WithHotReloadDir` is
  removed or made functional; no other signatures change.
- **Behavior (breaking):** literal-element evaluation (ADR-0001) — scripts relying
  on `[...]`/`{...}` holding unevaluated symbols must use `quote` or explicit
  constructors; eval errors are now typed `*LispicoError`.
- **Dependencies:** none. Core stays zero-dependency.

## Non-goals

Deferred to a follow-up cleanup change: the dead-code sweep (`tailCall`,
`tokenHash`/`tokenAt`, `VM.reset`, unused symbol grammar), relocating the REPL out
of the embeddable package, backfilling or removing the boilerplate openspec stubs,
dropping the `Name()`==namespace convention doc, demoting the unwired `agent`/`fsm`
plugins to experimental, and making `HashMap` string/iteration order deterministic.
