## Why

`builtin-resource-accounting` adds the correct batched work and borrowed-result
primitives, but applying them to every existing Builtin would turn a core
foundation into a repository-wide prerequisite. The final stdlib surface still
needs a separate completeness pass after lookup, CL adapter, and nil semantics
settle.

Blocked by: `stdlib-nil-sequence-semantics`.

## What Changes

- Freeze the final active stdlib and CL-adapter vocabulary as an executable
  work/result inventory.
- Assign exactly one owner to every scalable uninterrupted phase and callback
  phase, including transitive helpers and opaque library calls.
- Migrate scalable core-owned work to `core.BuiltinWorkBudget`; rewrite or bound
  opaque phases and mark trusted host `Value` methods explicitly.
- Classify every successful result branch as scalar/singleton, wholly borrowed,
  fresh, incremental persistent, mixed, or callback-produced and charge it once.
- Make collection/depth helpers consume the active evaluator rather than
  `env.Evaluator()` and add nested-scope regressions.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: require complete Builtin work and result ownership across the
  final active stdlib surface.

## Impact

- Affects all active stdlib GoFuncs, shared collection kernels, CL adapters,
  transitive helpers, static inventory checks, and resource tests.
- Adds `internal/inventory`, the repository-owned executable work/result
  inventory and its reconcilers, and the source checks that hold the code and
  the rows together. Those checks live in `plugins/stdlib` and sweep `cl`,
  `internal/collections` and `plugins/stdlib`; `core` and `plugins/json` are
  deliberately outside the migration and outside the sweep.
- Touches `core`: deep equality becomes genuinely interruptible rather than
  checked only before and after, because a check around opaque work does not
  establish compliance. `core` keeps its zero external imports.
- Also closes an overflow in the allocation and reduction counters
  (`core/metering.go`), where a refused charge near the int64 ceiling left the
  counter able to admit the next one, and stops `EvalMeter.Snapshot` publishing
  a wrapped total while charges are in flight. Both predate this change.
- Does not change valid language semantics established by the predecessor
  changes. It does change what a given resource limit admits. A differential
  across both dialects and both execution modes measured the direction: almost
  every difference is an expression that failed at base and succeeds here, the
  rest fail on both sides with a different limit binding, and no case observes
  a ceiling earlier than at base.
  - Charges that were false disappear. A result billed twice is now billed
    once, so `(apply str …)` over 100k elements charges 5,800,509 bytes rather
    than 6,800,525.
  - A literal `format` width or precision that `fmt` itself refuses is no
    longer pre-charged as though it would be honoured. `(format "%67108864d" 1)`
    charged 67,109,067 bytes and failed at base under the default ceiling; it
    charges 224 here and returns `"%!(NOVERB)%!(EXTRA int64=1)"`, which is what
    `fmt` renders either way — given enough budget base rendered the same
    string, so what changed is base's refusal, not the rendering. Every literal
    width in that band gains a value and a type where base had an error.
  - Where a dropped allocation charge lets a different ceiling bind first, the
    terminal error's text changes with it — an allocation-limit message becomes
    a reduction-limit message, and the expression still fails. Both stay
    terminal and uncatchable.
  - One operation bills more, not less: `string/split` now charges the fresh
    `String` header it allocates per part.
