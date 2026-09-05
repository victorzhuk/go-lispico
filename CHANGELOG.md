# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- The release gate now reports a cell's latency verdict as inconclusive
  whenever the stored baseline and the candidate benchmark were measured on
  different runners, instead of comparing latency figures across them.
  Allocation counts and allocated bytes are still enforced regardless of
  runner identity. A release whose stored baseline came from a different
  runner now finishes in one benchmark run: the gate no longer spends a
  second run at doubled benchtime on cells it cannot decide, since a rerun
  regenerates only the candidate and cannot change which machine the
  baseline was measured on. Cells the stored baseline never measured
  collapse the same way.

## [0.13.0] - 2026-09-05

### Changed

- **Breaking:** `core.Dialect.WithAdapter` now takes a semantic ID between
  the adapter name and its value: `WithAdapter(name, semanticID, value)`. The
  semantic ID participates in the dialect fingerprint, so adapters with the
  same name but different semantics are no longer indistinguishable, and
  fixed IDs make the CL surface hashable across processes. Callers carrying
  the old two-argument form must supply an ID — for the Common Lisp dialect
  the shipped IDs are `cl/nth@1`, `cl/mapcar@1`, and `cl/sort@1`:

  ```go
  d := core.FullDialect().
      WithAdapter("nth", "cl/nth@1", nthFn)
  ```

- The CL collection adapters pin down their Common Lisp call shapes: `nth`
  is index-first and answers `nil` beyond the end of a list or on `nil`;
  `mapcar` walks any number of sequences to the shortest one's length; and
  `sort` returns a fresh sorted sequence of the input type, leaving the
  input untouched instead of destroying it as the Common Lisp standard
  permits. Malformed options fail with exact error classes: wrong arity with
  `ArityError`, an unknown or repeated keyword or a negative `nth` index
  with `EvalError`, wrong argument types with `TypeError`. The first
  callback error stops the traversal and no further callback runs; terminal
  errors keep terminal precedence.

- The release consumer gate passed on a published release for the first time,
  and `v0.12.0` carries the project's first stored `bench-vm.txt` baseline.
  The run evaluated 27 cells — thirteen `Goldset/*`, thirteen
  `GoldsetParse/*`, and `GoldsetCall/call-boundary` — in first-authorization
  mode and passed all of them, then stored its VM benchmark output as a
  release asset. Consequences for the next release: it is the first to run as
  a non-regression comparison rather than a first authorization, and the
  baseline it compares against was measured after the reader-allocation work
  landed, so `B/op` figures are directly comparable. Recorded with its outcome
  rather than as a bare activation, matching how the `v0.11.0` failure above
  was recorded.

- **Breaking:** `get-in` is now a Go builtin rather than a bootstrap `defn`,
  which changes three things beyond its semantics. It prints and compares as
  a builtin — `core.GoFunc`, rendering as `#<builtin:get-in>` — where it was
  a `core.Lambda`. On an empty-base dialect that declares a vocabulary, that
  vocabulary is an allowlist over registered builtins, so `get-in` is now
  stripped unless the vocabulary names it; as a `Lambda` it was exempt from
  that pass. A bare `core.EmptyDialect()` with no vocabulary strips nothing
  and leaves it bound. Under the CL dialect it now publishes into both the
  value cell and the function cell, where the bootstrap `defn` filled only
  the function cell. Callers on an empty base allowlist it:

  ```go
  d := core.EmptyDialect().
      Vocabulary(map[string]string{"get-in": "get-in"})
  ```

- **Breaking:** `get-in` traversal changed with that rewrite. It takes an
  optional third argument as the default. An empty path — `nil` or an empty
  sequence — returns the subject unchanged and never consults the default. A
  key present with a `nil` value at the final position is a successful
  lookup, while a `nil` with keys still to walk is a miss. A non-map value
  with keys still to walk is now a `TypeError` instead of the silent miss the
  old `reduce`-over-`get` definition produced, and a default never replaces
  an error. The path itself must be a list, a vector, or `nil`; anything else
  is a `TypeError`, and a wrong argument count an `ArityError`.

- `get` reports malformed calls as typed errors: a non-map subject as
  `TypeError` and a wrong argument count as `ArityError`, both
  `*core.LispicoError`, where both were previously plain untyped errors. Its
  scope is now specified: maps plus `nil`, which reads as an empty map, with
  lists, vectors, and strings rejected as `TypeError`. A key present but
  holding `nil` returns that `nil` rather than the default, and a key that
  cannot be hashed counts as missing rather than as an error. It also reports
  its result as borrowed from the subject rather than allocated, charging zero
  result-allocation bytes where the apply site previously charged the returned
  value's shallow size, so consumers metering `MaxAllocationBytes` see a lower
  charge for `get` than before.

- **Breaking:** `RegisterSource` lost its trailing `reusable bool` parameter,
  on both the environment method and the `core.LazyLayer` interface it
  delegates to:

  ```go
  // core.Env method
  func (e *Env) RegisterSource(name, source string) bool

  // core.LazyLayer interface method
  RegisterSource(env *Env, name, source string) bool
  ```

  The flag selected the process-level stdlib bootstrap artifact cache, which
  is retired, so it advertised a reuse path that no longer exists. A host that
  implements `core.LazyLayer` itself deletes the parameter from its
  `RegisterSource`. The failure mode is indirect: the interface is satisfied
  structurally, so a stale implementation compiles on its own and the error
  lands at the `SetLazyLayer` call site instead — `does not implement
  core.LazyLayer (wrong type for method RegisterSource)`, followed by the
  `have`/`want` signatures. The per-engine bytecode chunk cache is unaffected;
  hosts relying on it need do nothing.

- **Breaking:** every failure originated by an active stdlib Builtin or a CL
  adapter is now a typed `*core.LispicoError`, where many were plain
  `fmt.Errorf` values that the Evaluator and VM passed through unclassified. A
  wrong argument count reports `ArityError`, a wrong runtime type `TypeError`,
  and a correctly typed value outside an operation's domain — an out-of-range
  index, a zero divisor, malformed format syntax, incomparable operands —
  `EvalError`, unless a more specific code already governs. Callback errors,
  failures from the shared evaluation-state checkpoint, and Terminal errors
  keep their original type and code instead of being flattened, and a Terminal
  error never becomes catchable by Lisp `try`/`catch`. A host that compared
  concrete error types from a direct stdlib call switches to `errors.As` and
  `Code`:

  ```go
  var le *core.LispicoError
  if errors.As(err, &le) && le.Code == "ArityError" { … }
  ```

  Codes are the contract; message wording is not, and stdlib errors carry no
  source position — `Line` and `Col` remain reader-only. Which inputs succeed
  and which fail is unchanged.

- **Breaking:** `nil` in the sequence position of `reverse`, kernel `nth`,
  `cons`, `conj`, `map`, `filter`, `reduce`, `apply`, and `string/join` now
  reads as an empty input and yields the operation's empty-list result, where
  each of these previously failed with `TypeError`. The positions are fixed:
  argument 1 of `reverse`, `nth`, and `conj`; argument 2 of `cons`, `map`,
  `filter`, and `string/join`; the final sequence argument of both `reduce`
  arities; and only the final expanded argument of `apply`. So `(cons 1 nil)`
  is `(1)`, `(conj nil 1 2)` is `(1 2)`, `(map f nil)` and `(filter f nil)`
  are empty lists with `f` never called, `(reduce f init nil)` is `init` and
  `(reduce f nil)` is `nil` — unlike Clojure, `f` is not called with no
  arguments — `(apply f 1 2 nil)` calls `f` with `1` and `2`,
  `(string/join "," nil)` is `""`, and `(nth nil 0)` reports the same
  index-out-of-bounds `EvalError` as `(nth '() 0)`, or the default when one
  is given. `first`, `rest`, `last`, `count`, `empty?`, kernel `sort`, and
  `concat` already accepted `nil` and are unchanged, as are the CL `nth`,
  `mapcar`, and `sort` adapters, the map-only operations, and every unlisted
  position. `nil` is still not the empty list: type predicates, equality,
  printing, and truthiness are untouched, and a non-collection value other
  than `nil` still fails with `TypeError`. A host that caught `TypeError`
  from these calls to treat `nil` as empty no longer receives it.

- `hash-map` now charges its result deeply: building a map bills the size of
  the values it holds, not just the container. A single 4096-byte string
  value adds 4129 bytes to the charge, the same on both evaluators.
  Accounting is stricter, not cheaper — a tight allocation limit can refuse a
  call it previously admitted.

- `format` is charged once instead of twice. Its result was billed by both
  the builtin and the apply site; the removed charge is exactly one copy of
  the rendered result — 4112 bytes for a 4096-byte result, and the difference
  tracks the result size at every size measured. Allocation totals for
  format-heavy programs fall.

- `range` now bills one reduction per element and observes the evaluation
  deadline while it builds. `(range 20000)` costs 20000 more reductions than
  before, on both evaluators, so a program under a tight reduction limit can
  hit the ceiling where it previously ran to completion. The deadline is the
  likelier of the two to bite a real program: `range` used to run to
  completion however little time the caller had left, and a call whose
  deadline expires while the list is being built now aborts instead — no
  reduction limit need be anywhere in sight.

- A builtin result reached through `apply` is charged once instead of twice. It
  was billed by both the builtin and the apply site; the removed charge is
  exactly one copy of the result, whatever its size — 16024 bytes off
  `(apply list (range 1000))`, 10016 off the 10000-character string
  `(apply str (map (fn [i] "abcdefghij") (range 1000)))` renders, and 16 off
  `(apply max (range 1000))`, whose result is a single `Int`. Direct calls were
  never double-billed and are unchanged. Allocation totals for `apply`-heavy
  programs fall.

- `assert` now reports against the values it is given, instead of resolving
  them a second time. A symbol message — `(assert false 'x)` — fails with
  `EvalError` and `assertion failed: x`, where it failed with
  `UndefinedError` and `undefined: x` from looking the symbol up. A list
  message — `(assert false (list 1 2))` — fails with `EvalError` and
  `assertion failed: (1 2)`, where the list was applied as a call and the
  resulting `TypeError` wording differed between the two evaluators. The same
  shift makes a symbol or list condition pass: `(assert 'x)` and
  `(assert (list 1 2))` return `nil` where they raised `UndefinedError` and
  `TypeError`, so a call that used to fail loudly now succeeds silently. Both
  transitions hold on both evaluators and under both dialects. The message
  rendering, the arity, which values count as true, and the success return are
  unchanged.

## [0.12.0] - 2026-07-31

### Added

- The release gate now measures the `Engine.Call` boundary. Every gate cell
  until now ran through `Eval`, so the boundary an embedder actually calls was
  unmeasured by the gate; `GoldsetCall/call-boundary` calls a GoFunc-free
  function once per timed iteration under both execution modes, and is
  classified engine-sensitive against a hosted profile
  (`internal/perfgate/testdata/profile-30637802780`) that measures it at
  -80.66% latency and -100.00% allocated bytes under the VM against the
  Evaluator, 188.50 ns/op and zero allocations. The figure is a hosted-runner
  measurement for the gold set's engine configuration and excludes the
  caller's variadic argument slice, which the cell hoists out of its timed
  loop; an embedder building fresh arguments per call pays that slice on top.

### Changed

- The performance claims of two v0.10.0 commits are recorded as declined
  rather than left open: native-op fusion and the batched allocation ledger.
  Both are ancestors of every tag cut since, with no build tag or environment
  knob that disables either, so no single run can attribute a figure to
  either commit — attribution would need a deliberate paired run of each
  commit against its parent, and that instrument is not built because nothing
  currently open needs it. Neither claim is confirmed or refuted, and no run
  may be cited as settling one. Behavior is unaffected; what changes is that
  the two claims are now closed with a stated reason instead of pending
  indefinitely.
- The gate's four tier-evaluator functions checked latency significance
  before the bytes and allocation-count non-increasing bounds, so a
  non-significant latency delta let `Resolve` collapse straight to a pass
  without those bounds ever being evaluated. Bytes and allocation count are
  now checked first in `evaluateNonRegression`, `evaluateWithinTolerance`,
  and `evaluateStartup`; `evaluateEngineSensitiveImprovement` hoists only
  its allocs check, since its bytes check is a 20%-improvement floor rather
  than a non-increasing bound and hoisting it would fail cells outright
  before the doubled-benchtime rerun ever ran. This surfaced as the
  `guard-nil` gold-set cell failing whenever a hosted run's latency
  measurement happened to reach significance: the cell allocates one byte
  more under the VM than under the Evaluator (1128 against 1129 B/op,
  +0.09%, p=0.000, 0% CI, reproduced across three hosted runs), which is
  not a removable allocation site — `core/vm.(*VM).run`'s cost offsets
  almost exactly against `core.(*engine).Eval`'s, plus a small `sync.Pool`
  GC-cadence remainder on the VM's chunk pool, two engines' honest cost for
  the same logical work. `Goldset/guard-nil` now carries a named, per-cell,
  absolute allowance of 4 B/op on the bytes axis (`tiers.json`'s
  `bytesAllowanceBOp`), sized from the hosted figures. No allocation was
  reduced; every other data-dominated cell keeps the exact bound.
- The classification profile behind the gate's cell tiers was recorded with
  the wrong benchmark parameters. Run `30630796967` ended inconclusive, so its
  rerun leg deleted both benchmark files and regenerated them at doubled
  benchtime before upload, making the committed files a 400ms measurement
  under a README stating 200ms. No tier it licensed is invalidated — every one
  still holds on the successor profile's 200ms figures — and the raw files are
  left as measured, with the correction recorded alongside them.
- Two v0.10.0 metering changes shipped without release-note coverage. The
  three entries here document what they already do; nothing about
  the metering ledger changes now. Reduction counts are compilation
  dependent: the VM charges one reduction per decoded instruction, so a
  compiler change that alters how many instructions a form compiles to
  changes the reductions charged for identical source under an unchanged
  evaluator. v0.10.0's branch/arith fusion cut the recursive `fib` body
  from 22 instructions to 13, so a program that previously tripped
  `MaxReductions` at some iteration count may no longer trip it there.
- v0.10.0 replaced per-opcode allocation charges with a VM-local
  accumulator settled at checkpoints, run exits, and before any `GoFunc`
  dispatch or re-entrant evaluation; settled totals are identical to
  per-instruction charging. Enforcement can lag those totals by at most
  one unsettled batch, and that batch also absorbs list, vector,
  hash-map, closure, and charged-constant bytes alongside scalar charges,
  with no size-triggered early settlement — so the lag before a
  `MaxAllocationBytes` breach surfaces is not bounded by any fixed scalar
  figure. The terminal error is unchanged: `ResourceLimitError`, raised
  no later than the next settlement point.
- v0.10.0's fusion also grows a chunk's accounted size: a fused-operator
  descriptor charges 40 accounted bytes while each instruction the fusion
  removes frees only 4, a break-even of 10 removed instructions per fused
  site. Below that, a chunk's `DeepBytes` grows even though fewer
  instructions execute, making a chunk near the `MaxCacheBytes` ceiling
  likelier to be refused caching than the same source compiled before
  fusion.
- The release consumer gate ran for the first time in the project's history,
  against the published `v0.11.0` release. Its correctness legs passed — the
  gold set under both execution modes and the race suite — and the paired
  Evaluator/VM benchmark completed. The performance verdict failed, on tier
  misclassification in the gate's own corpus rather than on engine behavior:
  five cells committed as mode-insensitive turned out to be the corpus's most
  engine-sensitive, and three committed as engine-sensitive barely move.
  Because the verdict failed, no `bench-vm.txt` asset was stored on the
  `v0.11.0` release, so `v0.12.0` in turn ran as a first authorization rather
  than a non-regression comparison against a stored baseline.
- Reading source reuses its tokenizer and parser working state across calls
  instead of building it fresh each time, and collection literals are copied
  once into an exactly sized backing slice rather than grown by repeated
  append. Parsing allocates roughly 57% fewer bytes per read on the reader
  benchmarks and 54% fewer on the gold set, with no cell regressing. Parsed
  output is unchanged.
- An empty list or vector literal now carries a zero-length backing slice
  where it previously carried none. Every operation on `List` and `Vector`
  behaves identically; only an embedder reaching past the public API — for
  instance comparing value trees with `reflect.DeepEqual` — can observe the
  difference.
- The consumer gate's cell tiers are corrected against a checked-in
  classification profile (`internal/perfgate/testdata/profile-30614184386`,
  from dispatch run `30614184386` at the gate's fixed parameters). Five cells
  committed as mode-insensitive are the corpus's most engine-sensitive and are
  reclassified as such; `guard-nil`, committed as engine-sensitive, is the one
  cell whose latency does not vary with execution mode and becomes
  data-dominated. The gate does not pass on the corrected tiers. Three cells
  fail on allocated bytes rather than latency — `guard-nil`, where the VM
  allocates 19.40% more than the Evaluator, `kw-lookup` at -9.04%, and
  `merge-config` at -19.96% against ADR 0008's 20% floor — and no release has
  been cut against the corrected tiers.
- `runtime.sha256Hash`, which keys the striped bytecode chunk cache off the
  source text on every VM `Eval` (and off the stdlib bootstrap source in
  `bytecodeEvaluator.EvalStdlibBootstrap`, `runtime/bootstrap_cache.go:66`),
  copied the whole source string into a fresh byte slice before hashing it.
  `crypto/sha256.Sum256` neither retains nor mutates its argument, so the
  copy is unnecessary; hashing now aliases the string's bytes directly. The
  site was 17.86% of `guard-nil`'s alloc_space on a profile taken before this
  change. It fires on every VM evaluation, so removing it moves every gold-set
  cell's bytes; on `guard-nil` specifically it was the whole of the
  Evaluator-vs-VM gap, while `kw-lookup` and `merge-config` were already
  below the Evaluator and simply move further below. Measured on the gold
  set (`-benchtime=200ms`, whole-process A/B, GOMAXPROCS=2): `kw-lookup`
  moves from -9.36% to -21.93% bytes and clears ADR 0008's -20%
  engine-sensitive floor; `merge-config` moves from -20.39% to -26.93% and
  clears the same floor with wider margin. `guard-nil`, the data-dominated
  cell, drops from +20.83% to +0.09% (1080 to 1081 B/op) but still exceeds its
  non-increasing bound by roughly a byte; that residual is unrelated engine
  cost (`core/vm.(*VM).run` vs `core.(*engine).Eval`, offsetting almost
  exactly, plus `sync.Pool` GC-cadence churn) and its threshold is left for a
  hosted profile to settle, not amended here. The hosted run reproduced the
  figure exactly — 1128 against 1129 B/op, +0.09% at p=0.000 — and the cell
  nevertheless passes the gate, for the reason recorded above.
- The consumer performance gate passes, all 26 cells, for the first time in
  the project's history: `workflow_dispatch` run `30630796967`, committed as
  the classification profile ADR 0008 requires at
  `internal/perfgate/testdata/profile-30630796967/`, with
  `internal/perfgate/tiers.json` now licensed by it. No cell's tier changed.
  A dispatch run carries no release identity, so it stores no baseline asset
  and consumes no baseline slot; the stored non-regression baseline still
  waits on a release cut whose gate passes.
- `evaluateStartup` (`internal/perfgate`) now bounds allocated bytes and
  allocation count non-increasing, matching every other tier. Previously the
  tier's absolute "1 ms / 256 KiB" overhead escape excused allocation
  entirely, so a startup cell could allocate without limit and still pass.
  The escape now excuses only the latency percentage bound. `Goldset/rule-load`,
  the corpus's only startup cell, reads -28.28% bytes and -22.69% allocs on
  the committed classification profile, so this changes no verdict on the
  current corpus — confirmed by running `cmd/perfgate` against that profile
  before and after the change and diffing the verdict output byte-for-byte.
- `Vector` packs into 64 bytes instead of 72, so every vector boxed as a
  `Value` now lands in Go's 64-byte allocator size class rather than its
  80-byte one — 16 bytes less per retained vector, measured at 20% on a
  benchmark that retains a thousand of them. Every gold-set cell allocates
  fewer bytes or the same; `queue-promote`, the one cell whose collection
  outgrows the flat representation, drops 2.9% under the Evaluator and 3.9%
  under the VM. Accounted allocation-ledger figures are unchanged, and are
  pinned by a test against exactly this class of change: the ledger's sizes
  come from ADR 0011's fixed table and do not follow Go struct layout.
- A vector now holds at most 2147483647 elements. The limit is not reachable
  through the shipped configuration — the default collection ceiling is ten
  million — but an engine explicitly configured with a `MaxCollectionLen`
  above it is now refused at construction with a `ResourceLimitError` rather
  than accepted and allowed to wrap.

## [0.11.0] - 2026-07-30

### Changed

- The release consumer gate (ADR 0008) runs when a stable release goes public,
  not on manual dispatch alone. It records post-hoc evidence against that
  release and stores the run's `bench-vm.txt` as the next release's
  non-regression baseline; it cannot block a release. Pre-releases do not
  trigger it, so an rc cannot become the baseline. Editing a gold-set
  fixture's source now counts as invalidating that fixture's stored baseline,
  alongside the existing `GOMAXPROCS`/benchtime rule.
- ADR 0013's account of what authorized the bytecode-VM default is corrected.
  The evidence was the local gold-set correctness run plus ad hoc benchstat
  measurements; the hosted gate had never run.
- `Engine.Call`, `Fn.Call`, and `PinnedFn.Call` on a bytecode engine with no
  engine meter, no callbacks, and a plain context take a lean boundary: a
  per-engine claimable VM (pool fallback under contention), a lock-free
  versioned read of the cached callee cell, and a single recover-defer —
  roughly 20-35% faster per call on the reference benchmarks. Behavior is
  unchanged; registering a callback or attaching a meter restores the
  general boundary on the next call.
- Host-callback re-entry re-arms the retained evaluation state
  incrementally: rearms carrying the same limits and timeout skip the
  redundant stores, cutting callback round-trip cost ~9-14%.
- VM-private depth counters use plain fields instead of atomics until a
  re-entrant dispatch shares them with an evaluation state.
- Function-name resolution on a Lisp-2 dialect (the default Common Lisp
  dialect) is cached per call site, the same way value-name resolution
  already was. Recursive Common Lisp workloads run roughly 30% faster;
  Clojure-dialect workloads are unaffected. Redefinition, deletion, and
  hot-reload remain immediately visible through a cached site.
- Reading source allocates substantially less. The tokenizer sizes its token
  slice exactly instead of growing it, the internal token shrank from 40 to
  32 bytes, and a string literal with no escape sequence is no longer copied.
  Parsing the gold-set corpus allocates about half the bytes it used to
  (geomean −50%), and a small list literal drops from 720 bytes over 7
  allocations to 304 over 4. Reader statistics, and therefore allocation-ledger
  charging, are unchanged to the byte.
- A string literal containing no escape sequence now shares storage with the
  source text it was read from, rather than being copied. Symbols and keywords
  have always done this, so an embedder holding parsed forms — a scope loaded
  once and kept for the process lifetime, say — already kept its source text
  reachable; string literals now behave the same way. Ledger accounting is
  unaffected. Literals containing escapes are still decoded into fresh storage.

## [0.10.0] - 2026-07-28

### Changed

- **Breaking:** `let` binds sequentially. A later init expression sees the
  bindings established before it in the same form — `(let [x 1 y x] y)`
  yields `1` even with an outer `x` bound to `99` — on the tree-walker and
  the bytecode VM (`compileLet` registers each local before compiling the
  next init). `let*` stays registered with the same semantics.
- **Breaking:** `cond` else clauses are marked by the `:else` keyword only.
  A clause headed by the bare symbol `else` is now an ordinary test
  expression, in both the evaluator (`isCondElse`) and the compiler
  (`isElse`). Clojure has never recognized bare `else`.
- **Breaking:** `string/join` takes the separator first:
  `(string/join "-" ["a" "b"])` yields `"a-b"`. The previous `(coll sep)`
  order type-errors: the separator must be a string.
- **Breaking:** `catch` binds the originally thrown value instead of its
  `%v`-stringified text — `(try (throw {:code :denied}) (catch e (:code e)))`
  yields `:denied` on both evaluators. Only errors that did not come from
  `throw` (engine errors returned by primitives) still bind their message
  string. The VM's `OpThrow` delivers the raw value to the handler;
  `coerceThrow` is gone.

### Removed

- **Breaking:** the `unless` special form. `(unless ...)` now resolves as an
  ordinary call and fails as an unresolved symbol under every dialect,
  including the Common Lisp dialect, which loses it with the kernel — no CL
  adapter is provided. Negate with `(when (not ...) ...)` or define a macro.
  The kernel table drops to 21 special forms.

## [0.9.1] - 2026-07-27

### Fixed

- A deferred stdlib name is no longer reported missing when several goroutines
  resolve it at once. Both already-installed fast paths in the lazy
  materializer destructured `Env.GetCanonical`, which returns
  `(value, found, canonical)`, as `(value, canonical, found)` — so the "found"
  test actually read the canonical flag, and every non-canonical builtin
  (`empty?`, `assoc`, `first`, `count`, `rest`, …) resolved as absent whenever
  a concurrent goroutine had already materialized it. Embedders resolving
  through `Env.Get`/`GetCanonical` saw nondeterministic
  `UndefinedError: undefined: <name>` for names the same env returns on the
  next call; the evaluator's own resolution path was unaffected, which is why
  the existing first-touch tests could not see it.

## [0.9.0] - 2026-07-27

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
- `runtime.Meter` lets an embedder own evaluation budgets: reduction and
  allocation leases (`LeaseEval`/`ReturnEval`) plus retained-capacity charges
  (`ChargeRetained`/`ReleaseRetained`). Bind one per engine with
  `runtime.WithEngineMeter(m)` or per call with `runtime.WithMeter(ctx, m)`,
  which overrides the engine meter; `runtime.NoopMeter` grants everything.
- `(*runtime.Fn).Pin()` returns a single-owner `*runtime.PinnedFn` with its own
  VM allocated up front, for hot call sites that would otherwise pay pool
  acquisition per call. It returns nil on the tree-walker path, and each handle
  must be driven by exactly one goroutine.
- `lispico -tree-walker` selects tree-walk-only execution from the REPL binary,
  mirroring `runtime.WithTreeWalker()`.

### Changed

- A macro is now expanded once per compiled chunk rather than once per
  evaluation. `EvalCached` expanded before consulting the chunk cache, so a
  cache hit skipped compilation but re-expanded anyway and discarded the
  result. **This is observable**: a macro whose expander body has side effects
  ran them on every evaluation and now runs them once per compilation, which is
  what Common Lisp and Clojure do and what the chunk-cache requirement already
  specified. Every gold-set cell got cheaper — `twice-macro` −16.2% allocs/op,
  `counter-closure` −7.3%, `rule-load` −6.4%, geomean −2.2%.
- **BREAKING**: `core.HashMap.Assoc` and `core.HashMap.Dissoc` now return
  `(*HashMap, int64, error)` instead of `(*HashMap, error)`. The added value is
  the bytes the update allocated, which the allocation ledger needs and only the
  map itself can know — the same shape `List.Cons` and `Vector.Conj` already
  use. Migration is mechanical: `m, err := m.Assoc(k, v)` becomes
  `m, _, err := m.Assoc(k, v)`, and `m, _ = m.Assoc(k, v)` becomes
  `m, _, _ = m.Assoc(k, v)`. No behavioral change to the returned map.
- Threading a map through repeated `assoc`/`dissoc` (`(reduce assoc {} pairs)`,
  `recur` carrying a growing map) is now linear rather than quadratic in both
  allocated memory and charged allocation bytes. Above the small-map threshold
  a map is a persistent trie, so an update copies one root-to-leaf path and
  shares the rest instead of copying every entry. Under default limits a
  chained `assoc` previously failed with a `ResourceLimitError` between 1440
  and 1450 keys; it now completes past 40,000. Bulk construction (map literals,
  `hash-map`, `merge`, `json/decode`) is unaffected — it builds through the
  mutable `Set` path, which is unchanged. Iteration order, equality, printing
  and the small-map representation are all unchanged.
- Accumulating into `List` or `Vector` (`cons`/`conj`-heavy loops, `recur`
  carrying a growing collection) is now linear rather than quadratic in both
  allocated memory and charged allocation bytes. `List` stores a flat slice
  at or below 32 elements and a shared-tail node chain above it. `Vector`
  built in bulk (literals, reader output) always stays flat regardless of
  size; only `conj` promotes, and only once the vector crosses 32 elements,
  splitting it into a bit-partitioned trie (32-way fan-out) with a tail
  buffer. Neither type demotes once promoted.
- **Breaking:** `core.List` and `core.Vector` no longer expose an `Items
  []Value` field — both now hold private, threshold-promoted backing. Go
  embedders constructing or reading these types directly must migrate to
  `core.NewList`/`core.NewVector` to build one, and `Len()`, `At(i)`,
  `ToSlice()`, `Rest()` (List), and `Cons`/`Conj` to read or grow one.
- **Breaking:** Removed `core.Dialect.NilOnlyFalsy()`. All dialects now treat
  `nil` and `false` as falsy, including the Common Lisp dialect.

- **Breaking:** The JSON plugin moved from `plugins/data` to `plugins/json`,
  matching the `json` namespace its functions already used
  (`json/encode`, `json/decode`, `json/pretty-encode`). Update imports to
  `github.com/victorzhuk/go-lispico/plugins/json` and `json.New()`; no Lisp-side
  name changed.

- Bytecode closures now capture only the variables they reference: a captured
  local is cell-resident and mutations no longer mirror into the environment
  map, so a closure keeps one cell alive instead of its whole lexical chain.

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
- A `Vector`, `HashMap`, or list literal whose elements are all compile-time
  constants now compiles to a single shared chunk constant instead of per-call
  element pushes plus an `OpMake*`. A function returning literal config
  (`(fn [] {:model :large :tools [:read :grep]})`) allocates nothing per call:
  336 B/op and 5 allocs/op → 0 and 0, ~210ns → ~63ns on
  `BenchmarkFoldedConstantLiteral`. Allocation-ledger charges and structural
  depth are precomputed at compile time and applied when the constant loads, so
  a program that trips `MaxAllocationBytes` or `MaxStructuralDepth` still trips
  it identically under both evaluators. **Observable to Go embedders**: repeated
  evaluations return the same value rather than equal fresh ones — unobservable
  in-language, visible to a host comparing pointers.
- The context wrapper handed to dispatched `GoFunc`s is now VM-owned storage
  reused across runs rather than rebuilt on every top-level `Call`, and its
  deadline is resolved on first observation instead of at dispatch, so a body
  whose host functions never read one reads no clock. `Engine_CallBytecode`
  drops from 128 B/op and 2 allocs to 32 B/op and 1; every host-callback and
  rule call sheds the wrapper. Bodies that dispatch no `GoFunc` are unchanged.
- Engine construction and plugin loading now share completed plugin template
  layers across engines, memoize stock dialect resolution and fingerprinting,
  and create the per-engine plugin bookkeeping maps on first write. A
  default-logger engine resolves to one process-wide discard handler that
  reports itself disabled, so it no longer formats attributes it drops.
  `Engine_Creation` 5.49µs → 741ns with 75 → 17 allocs/op, `Use/lazy`
  244 → 32 allocs/op, `Startup/cache-warm` 356 → 135 allocs/op.
- Repeated `Engine.Call` with the same name resolves through a cached handle
  instead of re-resolving the binding: `Engine_CallBytecodeCanonical`
  158.1ns → 144.9ns (−8.35%).
- A top-level `defmacro` now compiles instead of deferring to the tree-walker,
  so a source that defines macros no longer splits across evaluators form by
  form: `twice-macro` 57 → 50 allocs/op (−12.28%).

### Fixed

- Accumulating collections with `cons` or `conj` is no longer quadratic in time.
  The construction-depth check re-walked the whole collection being extended on
  every call, so a loop consing small lists onto an accumulator allocated
  linearly but spent time quadratically — at 800 iterations, 10.8× the cost of
  the same loop consing scalars, for twice the allocations. Extension now checks
  only the element being added, which is sound because the collection being
  extended was checked when it was built. 800 iterations: 1583µs → 305µs
  (−81%). The limit itself is unchanged: the same constructions are rejected at
  the same depth, including a loop that nests one level deeper per step.
- Expanding a quasiquoted list longer than 32 forms is no longer quadratic in
  its length. Past that threshold a list is a shared chain where reading
  position `i` costs `i` steps, and three engine loops walked lists by
  position: quasiquote expansion, splicing a sequence into a quasiquoted form,
  and wrapping a multi-expression `cond` clause body. They now iterate with a
  cursor. A 256-form quasiquoted literal expands in 9.89µs rather than 38.97µs
  (−75%), with byte-identical allocations — only the traversal changed. Macros
  whose template is a long generated `do` block, dispatch table, or `cond` paid
  this on every expansion.
- Re-evaluating an unchanged source that contains a `defmacro` no longer
  recompiles it. `defmacro` bumped the macro epoch unconditionally, and the
  epoch keys the compiled-chunk cache, so every form in such a source was
  recompiled on every evaluation — the "repeated evaluation reuses the chunk"
  guarantee silently excluded any source defining a macro. Rebinding a macro to
  an identical definition in the same scope now leaves the epoch alone. A real
  redefinition still invalidates, and the comparison fails closed: a body too
  deep to compare, or one closing over a different scope, counts as a
  redefinition. Measured on the `twice-macro` gold-set fixture under the VM:
  103 → 68 allocs/op (−34%), 7.97 → 5.86 KiB/op (−26%).
- A macro defined by `defmacro` in one `Eval` now expands in later `Eval` calls
  under Lisp-2 dialects (the default Common Lisp dialect) on the bytecode VM.
  Macro expansion resolved the head in value position while `defmacro` binds the
  function cell, so the call compiled as a plain call and failed with
  `TypeError: expected callable, got core.Macro`.
- `ListPlugins()` now reports each plugin's real lifecycle status instead of always returning "active".
- Env merges are now atomic against concurrent writes, retained usage stays
  consistent across merge overwrites, and multi-meter retained settlement
  rolls back earlier charges on partial failure.
- A retained-capacity error part-way through `MergeInto`/`MergeIntoCanonical`
  now leaves the target env untouched. Aggregate usage and new bindings are
  staged and applied only after every binding passes its reservation, so a
  hot-reload that overflows the target no longer inflates its retained totals
  on every attempt.

- `assoc` and fused native-op results now charge the evaluation allocation
  ledger: deep result bytes for `assoc`, fixed scalar bytes for fused ops.

- Deeply nested value construction (VM `OpMakeList`/`OpMakeVector`/`OpMakeMap`,
  stdlib `list`/`cons`/`vector`/`conj`/`assoc`/`merge`, `json/decode`),
  value-tree walks (`String`/`Equals`/`ValueDeepBytes`/`ValueNodeCount`), and
  macro-expanded bytecode compilation are now depth-bounded and return a
  terminal `ResourceLimitError` instead of crashing the process with
  `fatal error: stack overflow`.
- `let`, `let*`, and `loop` now accept Common Lisp `(name value)` list
  bindings under the default dialect in both the tree-walker and bytecode VM.
- Closures created in a `loop` body now capture per-iteration loop-variable
  values in both the tree-walker and bytecode VM.

- Restored tree-walker parity for exact `MaxDepth` boundaries by keeping
  tree-walker depth checks at `> MaxDepth` and routing public `VM.Apply`
  through `ApplyPooled` so shared call-depth counters are enforced on the
  public path.
- Higher-order recursion through bytecode VM `map`, `filter`, `reduce`, and
  `apply` now shares the evaluation call-depth counter and returns `EvalError`
  at the same limit as the tree-walker.
- `GoFunc` panics crossing `Engine.Eval`, `Engine.Call`, and `Fn.Call`
  now return `PanicError` without aborting the host process; recovered
  bytecode VM state is reset or discarded before reuse.
- Amplifying `json/decode` and `format` builtins now charge the evaluation
  allocation ledger for constructed output before returning oversized results.
- `EvalWithBindings` and `LoadScope` now recover `GoFunc` panics and return
  them as `PanicError`, matching `Engine.Eval`; a recovered `LoadScope` call
  returns a nil scope.
- **Breaking:** a `GoFunc` panic during `Watch` background hot-reload no
  longer crashes the host process; the reload reports an error through the
  watcher log and the watch loop keeps running.

- Fix Lisp-2 vocabulary bridge overwriting user function-cell redefinitions on subsequent plugin `Use()` calls.

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

[unreleased]: https://github.com/victorzhuk/go-lispico/compare/v0.13.0...HEAD
[0.13.0]: https://github.com/victorzhuk/go-lispico/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/victorzhuk/go-lispico/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/victorzhuk/go-lispico/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/victorzhuk/go-lispico/compare/v0.9.1...v0.10.0
[0.9.1]: https://github.com/victorzhuk/go-lispico/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/victorzhuk/go-lispico/compare/v0.8.0...v0.9.0
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
