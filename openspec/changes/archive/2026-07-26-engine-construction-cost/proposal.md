## Why

Two changes landed today made plugin loading cheap: template construction is
now at-most-once per process, stock dialects are memoized, and a completed
layer is read rather than copied. `Use(stdlib)` fell from 244 allocations to 38.
What did not move is engine construction itself, and it is now the majority of
what a fresh engine costs.

`BenchmarkEngine_Creation` — `New` plus `Close`, no plugin, no evaluation — is
24 allocations and ~765ns. Attributed exactly (`GODEBUG=memprofilerate=1`,
per-allocation sampling):

| Site | share | allocs |
| --- | --- | --- |
| `newStdlibLazyEngineState` | 17.7% | 4 |
| `runtime.New` itself | 13.3% | 3 |
| `newBytecodeEvaluator` | 13.3% | 3 |
| `slog.NewTextHandler` + `slog.New` | 15.5% | 3.5 |
| `core.NewEnv` | 8.9% | 2 |
| `core.NewEvaluatorWithDialect` | 8.9% | 2 |
| `core.NewRegistry` | 8.9% | 2 |
| `newStats`, `SetLazyLayer`, `newStdlibLazyMaterializer` | 4.4% each | 1 each |

Two of these are pure waste rather than work an embedder asked for.

**The discard logger is rebuilt per engine.** When `New` is passed a nil logger
it runs `slog.New(slog.NewTextHandler(io.Discard, nil))` (`runtime/engine.go`),
constructing a handler and a logger whose entire job is to throw output away.
That is 3.5 allocations per engine for an object with no per-engine state and
no observable behavior.

**The lazy-materialization bookkeeping is built eagerly.** `installLazyLayer`
and its chain are 26.5% cumulative — `newStdlibLazyEngineState` alone eagerly
allocates three maps. An engine that never loads a template-routed plugin pays
for all three and uses none. The same nil-until-first-use treatment already
applied to `nameLocks` in `engine-startup-template-sharing` applies here.

Only the three maps are removable. The constructor's
`activeList.Store([]stdlibTemplateKey(nil))` costs zero marginal allocations —
a nil slice boxed into an interface points at `runtime.zerobase` — and it must
stay regardless: `activeKeys` type-asserts `Load().([]stdlibTemplateKey)`
without the comma-ok form, so a never-stored `atomic.Value` panics, and
`installLazyLayer` runs on every engine whether or not a plugin is ever loaded.
So the realistic saving is 3.5 (logger) + 3 (maps) = 6.5 of 24, landing near
17.5 allocations rather than the 16.5 a four-map reading would predict.

The `installLazyLayer` chain's remaining allocation is `core.Env.SetLazyLayer`
boxing its argument for an `atomic.Pointer` store. It lives in `core/` and is
out of scope here; task 5.2 names it so it is not silently dropped.

For yagel this is the per-task engine cost, paid on every construction whether
or not the task loads a plugin or evaluates anything.

## What Changes

- **A nil logger resolves to a shared discard logger** built once per process
  rather than per engine. Nothing observable changes: the logger discards in
  both cases and is not reachable through the public API. An explicitly passed
  logger is untouched.
- **Per-engine lazy-materialization state allocates on first use.** The maps
  behind `stdlibLazyEngineState` are created when something is first written to
  them instead of at construction, matching the `nameLocks` treatment already
  in that type. Every read path must tolerate a nil map — reads from a nil map
  are already legal in Go; only writes need the guard.
- **The remaining construction sites are measured, then left alone unless the
  profile justifies touching them.** `newBytecodeEvaluator`, `NewEnv`,
  `NewEvaluatorWithDialect`, and `NewRegistry` are each 2-3 allocations of
  genuine per-engine state. This change does not speculatively pool or share
  them; a task exists to report what they cost after the two fixes above and
  to say plainly whether more is worth doing.

## Capabilities

No capability changes. Engine construction produces an engine with identical
observable behavior — same evaluation results, same plugin semantics, same
logging behavior for both nil and explicit loggers. This is an internal
allocation change, verified by the existing suite rather than by new
requirements.

## Impact

- Code: `runtime/engine.go` (logger default), `runtime/lazy_template.go`
  (`newStdlibLazyEngineState` and every writer of its maps), tests.
- Risk — shared logger aliasing: a process-wide `*slog.Logger` is shared by
  every engine constructed with a nil logger. `slog.Logger` is safe for
  concurrent use and a discard handler holds no state, but this must be
  confirmed against the actual handler rather than assumed, and it must remain
  true that no code path mutates the engine's logger after construction.
- Risk — nil-map writes: converting eagerly built maps to lazy ones moves a
  class of bug from impossible to possible. Every write site must be found and
  guarded; a missed one panics on assignment to a nil map, which the project's
  no-panics invariant forbids.
- Risk — concurrency: `stdlibLazyEngineState` is written from concurrent
  first-touch paths under its own mutex. Lazy initialization must happen inside
  that mutex, not before it.
- Interaction: builds on `engine-startup-template-sharing` and
  `engine-attach-snapshot-sharing`, both archived 2026-07-26. Independent of
  `vm-boundary-state-reuse` and `compiler-constant-literal-folding`.

## Out of scope

Pooling or reusing whole engines across tasks. That changes the embedding
contract — an engine carries user definitions, so handing a used one to the
next task is a correctness question, not a performance one — and belongs to a
separate proposal if it is wanted at all.
