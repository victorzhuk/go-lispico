## Why

`Engine.Call(ctx, name, args...)` re-resolves `name` on every invocation — an
env chain walk (`env.Get`, runtime/eval.go:659) plus a `sync.Map` stats
lookup (`counterFor`) — while `Fn`/`PinnedFn` resolve once and read a cell
pointer per call. Measured at dcbdf62: name-based `Call` 187ns vs `Fn.Call`
155ns vs `PinnedFn.Call` 152ns — the repeated resolution costs ~32ns/call,
17% of the row. GopherLua and goja embedders pre-resolve
(`L.GetGlobal`/`AssertFunction`) and so does the article harness for them;
lispico's handles exist (`Func`/`Pin`) but `Call(name)` — the API every
casual embedder and the harness actually uses — never benefits.

The fix is the obvious one: `Call` internally reuses the resolve-once
machinery it already has. cel-go's model (plan once, evaluate many) and the
handle APIs of both comparison engines all put resolution outside the hot
loop; this change puts it there for the name-based entry point too, without
asking the embedder to change code.

Expected effect: `Call(name)` converges to `Fn.Call` cost (~155ns at
dcbdf62), taking the article Call row to goja-beating territory with zero
harness changes, and shaving the same ~30ns off every name-based `Call` in
yagel. Combined with `vm-boundary-state-reuse` and
`compiler-constant-literal-folding`, the name-based Rule row inherits all
three wins.

## What Changes

- `Engine.Call` consults a per-engine name→handle cache (the existing `Fn`
  shape: resolved cell + stats counter) before walking the env. Miss →
  resolve as today and populate. Hit → proceed exactly as `Fn.Call`.
- Correctness across rebinding: a `Fn` holds a `*core.Cell`, and
  redefinition writes the cell in place, so a cached handle observes
  rebinding exactly as `Fn.Call` already does (pinned by the existing
  function-handles requirement). Deletion is likewise cell-mediated —
  `Env.Delete` (core/env.go:816) tombstones in place and `Env.Set`
  (core/env.go:360) reuses the existing map entry — so delete, redefine,
  `UnloadPlugin` (which only calls `rootEnv.Delete`, runtime/plugin.go:56)
  and hot-reload (`MergeInto` into the existing root env, runtime/watch.go:139)
  all resolve correctly through the cached cell with no explicit hook.
  The one operation that changes name→cell mapping is `Env.Rebuild`
  (core/env.go:844), which drops tombstoned map entries so a later `Set`
  allocates a fresh cell. Entries SHALL therefore be guarded by the
  environment's name generation (`Env.NameGen`, bumped by `Rebuild` at
  core/env.go:893), which covers every caller — including an embedder
  calling `RootEnv().Rebuild()` directly, which no engine-side hook can
  observe. Fail-closed: a generation or environment mismatch drops the
  entry and re-resolves.
- Lisp-2 dialects resolve and cache the function cell first, falling back to
  the value cell — the order `resolveHead` already uses for a form's head
  (core/eval.go:625-644), since `Call(name)` is a head-position call made from
  the host. This closes a documented gap: `Call` previously used `Env.Get`
  (value cell only), leaving a `defun`-bound name unreachable from the host
  under the default CL dialect. `Func(name)` keeps its narrower
  function-cell-only resolution, so `Call` reaches a superset of what `Func`
  does.

## Capabilities

### Modified Capabilities

- `runtime-api`: adds a requirement that repeated name-based boundary calls
  amortize name resolution, with rebind/delete/reload semantics identical to
  fresh resolution.

## Impact

- Code: `runtime/eval.go` (`Call` path), a small copy-on-write cache
  (`atomic.Pointer` to an immutable map) on `engineImpl`, a hygiene entry
  drop in the plugin-unload path, tests.
- Risk — stale handles: the dangerous case is not rebinding or deletion
  (both cell-mediated, already correct) but a *new cell for a live name*,
  which only `Env.Rebuild` produces — it drops tombstoned map entries, so a
  subsequent `Set` allocates a fresh cell and a handle cached beforehand
  resolves the orphan, reporting "undefined function" for a name that
  exists. The name-generation guard covers this for every caller; each
  operation still gets a scenario, and the delete-then-redefine scenario
  must force a `Rebuild` between the two steps or it passes vacuously
  against an unguarded cache. Fail-closed re-resolution keeps any missed
  case a performance bug, never a correctness bug.
- Unbounded growth: names are engine-vocabulary-scoped; a pathological
  embedder calling unbounded distinct names could grow the cache — bounded
  by the same ceiling discipline as the chunk cache (entry cap, coarse LRU
  or full flush on overflow; distinct names in real embedders are few).
