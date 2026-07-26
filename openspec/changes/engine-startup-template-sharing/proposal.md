## Why

Engine startup is 39.6µs / 41.4KB / 286 allocs on the article harness
(`New` + `Use(stdlib)` + one eval + `Close`) against goja's 2.5µs. The gap is
not evaluation — it is per-engine reconstruction of state that is identical
for every engine of the same dialect. Measured split at dcbdf62:

| Stage | Cost | What it rebuilds |
| --- | --- | --- |
| `New` + dialect | 3.5µs / 42 allocs | delta chain, vocab map, `resolve()`, SHA-256 fingerprint |
| `Use(stdlib.New())` | +17µs / +202 allocs | every builtin `GoFunc` closure, template overwrite |
| first `Eval` + `Close` | +19µs / +42 allocs | first-touch materialization, compile, cold VM pool |

The allocation profile is unambiguous: `stdlibLazyMaterializer.RegisterValue`
alone is 26.9% of all startup allocations, and the seven stdlib `register*`
functions with their helpers account for over half. `stdlib-lazy-materialization`
deferred the *env installation* of bindings to first touch — but the template
those bindings come from is rebuilt from scratch on every `Use`:
`stdlib.Plugin.Init` (plugins/stdlib/plugin.go:25-41) unconditionally re-runs
all `register*` functions, reconstructing dozens of `GoFunc` closures, and
`putEntry` (lazy_template.go:121-133) overwrites the process-level template
layer that a previous engine already populated with identical content.
Likewise `cl.Dialect()`/`clojure.Dialect()` rebuild the full delta chain and
vocabulary map per call with zero memoization (cl/cl.go:25-49,
core/dialect.go:393-398), and `Dialect.Fingerprint()` re-hashes per engine.

The external precedent is uniform. goja starts in 2.5µs because globals come
from package-level lazily-instantiated templates shared by all Runtimes;
starlark-go freezes module state after init so it is shared lock-free across
threads by construction; V8 ships one immutable snapshot that N isolates
overlay. The infrastructure on our side already exists — the process-level
template registry keyed by dialect fingerprint, and the process-level
plugin-chunk tier in the compiled-chunk cache — it is just fed redundantly
per engine.

Expected effect: `Use(stdlib)` drops to a fingerprint lookup (~17µs → well
under 1µs after the first engine in a process), dialect construction stops
re-hashing and re-copying (memoized stock dialects), and the startup row
lands under ~10µs — from 1.7x behind GopherLua's per-state cost to ~7x ahead,
with goja's 2.5µs remaining ahead on account of its empty (stdlib-free)
global surface. For yagel this is the engine-per-task cost.

## What Changes

- **Template registration becomes at-most-once per (dialect fingerprint,
  plugin).** `Use` consults the process-level template registry first: when a
  completed layer for the fingerprint+plugin already exists, the plugin's
  `Init` is not run at all and the engine attaches the existing layer.
  Completion is marked when the first `Init` finishes; concurrent first `Use`
  of the same layer is single-flighted. `GoFunc` values are immutable and
  engine-agnostic (established by the existing deferred-materialization
  requirement), so sharing the closures is sound.
- **Stock dialect constructors return memoized immutable values.**
  `cl.Dialect()` and `clojure.Dialect()` build once per process
  (`sync.Once`); `Dialect.resolve()` output and `Fingerprint()` are computed
  once and cached with the memoized dialect. Shared state is immutable
  post-construction; per-engine dispatch isolation is preserved because
  resolution output is read-only and engine redefinitions live in the
  engine's own environment, never in the dialect table.
- **First-eval residue gets profiled and trimmed within the existing
  process-chunk tier.** The ~19µs after `Use` is first-touch materialization
  + user-source compile + cold VM pool. Tasks include attributing this tail
  and wiring whichever dominant component the existing process-level tier
  already covers (plugin-source chunks are shared today; the classifier for
  reusing a user-source chunk requires macro-neutral source — only taken if
  the profile shows compile dominating and the condition can be stated
  fail-closed).
- Plugin unload, hot-reload, shadowing, deletion, enumeration semantics are
  unchanged — they operate on per-engine state (`newStdlibLazyEngineState`),
  which stays per-engine.

## Capabilities

### Modified Capabilities

- `runtime-api`: "Deferred plugin binding materialization" gains the
  at-most-once-per-fingerprint template-construction guarantee (plugin `Init`
  not re-run for an already-completed shared layer) and a startup-cost
  scenario.
- `dialect`: adds a requirement that stock dialect construction is memoized
  and shared state immutable, preserving per-engine dispatch isolation.

## Impact

- Code: `runtime/lazy_template.go` (layer completion + single-flight),
  `runtime/plugin.go` (`Use` short-circuit), `cl/cl.go`, `clojure/clojure.go`,
  `core/dialect.go` (memoized resolve/fingerprint), tests.
- Risk — layer lifetime: a completed shared layer must survive engines
  closing (process-level by design) yet respect `UnloadPlugin`, which is
  per-engine: unload removes the engine's attachment and materialized
  bindings, never the shared layer. Already the registry's shape; pinned by
  a scenario.
- Risk — plugin identity: sharing keys on (dialect fingerprint, plugin name).
  Two different plugin *values* with one name (a stdlib fork) would collide;
  the fingerprint key must include the plugin's identity/version from
  `Metadata()` or fail closed to per-engine registration when a mismatched
  re-registration is detected. Task-gated decision.
- Risk — `Init` side effects: stdlib's `Init` is registration-only. The
  short-circuit is scoped to plugins whose registration goes through the
  template registry (today: stdlib); plugins writing directly into the env
  keep per-engine `Init`. Fail-closed scoping.
- Interaction: `engine-named-call-handle-cache` and `vm-boundary-state-reuse`
  are independent; no ordering constraint. `compiler-constant-literal-folding`
  shares no files.
