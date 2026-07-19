# Design — engine-func-handle

## Per-call cost inventory (VM boundary, 260 ns today)

| Work | ~ns | Fate |
|---|---|---|
| `time.Now` ×2 (deadline start, event duration) | ~40 | lazy deadline + observed-only timing → 0 |
| `env.Get(name)` map walk under `RWMutex` | ~35 | handle caches the cell → locked cell read ~15 (or snapshot read with env-cell-versioned-reads) |
| `stats.countPluginCall` `sync.Map` by name | ~25 | handle caches `*atomic.Int64` → ~3 |
| ctx `select`, `callbacksActive` load, deadline math | ~10 | kept (contract) |
| pool Get/Put + Reset + apply + run of the body | ~150 | kept |

Floor after this change ≈ 120–150 ns for a two-arg arithmetic body. GopherLua's
84 ns is not reachable without dropping the ctx check, deadline enforcement, or
stats — explicit non-goal.

## API shape

```go
type Fn struct{ ... } // runtime package, opaque

func (e *engineImpl) Func(name string) (*Fn, error)
func (f *Fn) Call(ctx context.Context, args ...core.Value) (core.Value, error)
```

`Fn` captures: the resolved root-env `*core.Cell`, the engine (evaluators,
config timeout, callback gate), and the name's stats counter. Interface
addition to `Engine` — additive, alpha-stage API.

Decisions:

- **Cell, not value.** Caching the resolved value would freeze the binding and
  diverge from `Engine.Call` semantics on rebind. The cell read per call is a
  locked `ReadCell` (~15 ns); if the boundary later needs more, the
  env-cell-versioned-reads snapshot mechanism applies to handles too — not in
  scope here.
- **Undefined at `Func` time errors immediately** — embedder typo surfaces at
  wiring time, matching `AssertFunction`'s failure mode.
- **Deleted after resolution**: tombstoned cell (`v == nil`) at `Fn.Call` →
  the exact `undefined function: <name>` error `Engine.Call` returns.
- **Concurrent use allowed**: the handle is immutable after construction; all
  mutable state it touches (cell, counter, VM pool) is already
  concurrency-safe. No per-goroutine handle requirement — lispico keeps its
  concurrency posture (unlike `LState`).
- **Non-root scopes out of scope**: `Func` resolves in the root env only, same
  as `Engine.Call`.

## Lazy deadline arming

Today `evalDeadline(ctx, start)` needs `start := time.Now()` at every boundary
entry. Move the arming into the VM checkpoint:

- Engine passes the configured timeout (duration) and the caller ctx's
  deadline instant (if any) to the VM instead of a precomputed instant.
- First `pollCancel` with an unarmed deadline: `now := time.Now()`;
  `bound := now.Add(timeout)`; apply the ADR 0010 suppression rule (caller
  deadline at-or-earlier → engine bound not set); arm.
- Subsequent polls compare as today.

Consequences: a call shorter than the first budget window never reads the
clock; the engine bound starts up to one budget window late (sub-µs on a 30 s
default — no observable contract change; ADR 0010 wording already says
"bounded-interval checks"). The suppression comparison at arm time is
equivalent to today's entry-time comparison because both compare the caller's
fixed instant against `now + timeout` — `now` only moved later, which can only
keep the caller's earlier deadline governing.

Tree-walker boundary (`WithTreeWalker()` engines) keeps eager arming — its
`evalState` plumbing already exists and that path is not the parity target;
`Engine.Call` on the tree path simply keeps today's behavior.

## Timing only when observed

`start`/`time.Since` reads move inside the `callbacksActive.Load()` gate.
`Stats()` accuracy (call counts) never needed the clock; `EvalEvent`/
`PluginCallEvent` durations exist only when a callback consumes them.

## Engine.Call refactor

`Engine.Call` becomes: entry ctx check → resolve (as today) → shared
`callBoundary(fn, counter, args)` used by both `Call` and `Fn.Call`. One
implementation, two resolution strategies.
