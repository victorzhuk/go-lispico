# bytecode-vm Specification

## Purpose

The bytecode VM is an experimental, opt-in evaluator (`runtime.WithBytecode()`)
that compiles forms to bytecode chunks and executes them on a stack machine, with
optional on-disk caching of compiled bytecode. It currently executes a subset of
the language; the tree-walking evaluator is the supported default.

## Requirements

### Requirement: Bytecode VM execution

The bytecode VM SHALL be an experimental, opt-in evaluator enabled by
`runtime.WithBytecode()`. It executes a subset of the language and is not a
drop-in replacement for the tree-walking evaluator, which remains the supported
default. Host code that needs the full language, correct iteration, error
handling, macros, or concurrent evaluation MUST use the default evaluator.

#### Scenario: Opt-in selection

- **WHEN** an engine is constructed without `runtime.WithBytecode()`
- **THEN** the tree-walking evaluator SHALL be used

#### Scenario: Supported subset executes

- **WHEN** the VM evaluates literals, global/local references, `if`, `when`, `unless`, `let`, `let*`, `do`, `def`, `quote`, `set!`, a non-variadic `fn`, or a function application over registered `GoFunc`s
- **THEN** the result SHALL match the tree-walking evaluator

#### Scenario: Unsupported forms are outside the contract

- **WHEN** the VM evaluates `loop`/`recur`, `cond`, `and`, `or`, `not`, `try`/`catch`/`throw`, `defn`, `defmacro`, `quasiquote`, a variadic `fn`, or any macro
- **THEN** the VM SHALL NOT be relied upon to match the tree-walking evaluator; these forms are outside the supported subset

#### Scenario: Bytecode cache hit

- **WHEN** `runtime.WithBytecodeCache(dir)` is set and the same source content is compiled twice
- **THEN** the second load SHALL read cached bytecode, gated by an on-disk `cacheVersion`, without recompiling

#### Scenario: Single-evaluator concurrency is unsupported

- **WHEN** two goroutines call `Eval` on the same bytecode-backed engine concurrently
- **THEN** behavior is undefined; the bytecode evaluator holds shared mutable state and is not safe for concurrent use
