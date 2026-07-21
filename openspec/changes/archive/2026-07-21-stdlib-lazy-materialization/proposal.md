## Why

`stdlib-startup-cache` removes per-engine recompilation but still **executes** every stdlib definition into each new engine's root env at `Use()` time — the remaining startup floor is env population plus Go-builtin registration (~30–40 µs projected). goja starts a Runtime in ~2.5 µs because it executes nothing at construction: package-level `sync.Once` templates map each global name to a factory, and a new Runtime materializes a binding the first time something asks for it (verified directly in goja source: `objectTemplate`/`newTemplatedObject`, round-4 research). An engine-per-request embedder pays lispico's full population cost on every request; under the template model it pays only for the handful of names the request's script actually touches.

This is the phase-2 follow-up to `stdlib-startup-cache` and is **trigger-gated**: implement only if warm startup still misses its target (or the per-request embedder profile shows env population dominating) after that change lands.

## What Changes

- Plugin load MAY register a **binding template** instead of executing definitions: a process-level, dialect-fingerprint-keyed map from name to materializer (Go builtin value, or compiled artifact from the startup cache), installed on the engine as a fallback layer of the root env.
- First resolution of an unmaterialized name — value cell, function cell (Lisp-2), or macro-table lookup during compilation/expansion — materializes the binding into the engine's own root env, then proceeds exactly as if it had been eagerly loaded. Materialization is per-engine, thread-safe, at-most-once per name, and transitively triggers dependencies naturally (a materialized stdlib fn's body resolves its own callees on first run).
- Everything that enumerates or mutates bindings behaves as if fully loaded: plugin listing, `UnloadPlugin` (drops the template layer and materialized names), shadowing (`def` of a stdlib name wins over the template), `Stats()`/docs surfaces force what they report on.
- Observable behavior is byte-identical to eager loading — results, errors, macro expansion, canonical-operator status — verified by running goldset + crossval with lazy on and off.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement `Deferred plugin binding materialization` — first-use materialization equivalence, enumeration/unload/shadowing semantics, thread safety.

## Impact

- Code: `core/env.go` (template fallback layer on the root env's miss path), `runtime/plugin.go` (template registration), stdlib plugin load path; builds on `stdlib-startup-cache`'s artifacts as the materializer payload.
- Expected: engine startup approaches the goja class — construction plus first-touch cost only (~5–10 µs for a small script's working set); full-surface scripts converge to today's totals.
- Risks: miss-path complexity on the hottest lookup route (mitigated: the template layer is consulted only after a real miss, which is not the steady-state path; steady state is site-cached hits); canonical-operator flags must be set at materialization identically to eager load (crossval covers the native-op cells).
- Sequencing: strictly after `stdlib-startup-cache`, gated on its measured result; independent of the VM changes.
