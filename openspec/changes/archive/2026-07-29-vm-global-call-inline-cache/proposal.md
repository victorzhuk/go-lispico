# vm-global-call-inline-cache

## Why

Each fib recursion resolves the global `fib` twice through
`resolveGlobalValue` (site-snapshot load + guards; 7.3% cum CPU at HEAD,
4.2% after the governance-tax spikes) and then dispatches a generic `CALL`
that type-switches the callee and builds a frame. The callee at such a site
is almost always the same `*Closure` from call to call, yet nothing on the
call path exploits that.

This is the classic interpreter inline cache. CPython 3.11+ caches
`LOAD_GLOBAL`/`CALL` targets per site behind dict version tags (PEP 659,
"up to 50%" aggregate); LuaJIT-Remake's interpreter tier uses one cached
entry per call site, overwritten on miss, and its authors rank call IC among
the most important interpreter optimizations. Lispico already has the
substrate: the resolved-globals site table with `{env, cell, gen, ver}`
snapshots — the cache exists for the *value read* but not for the *call*.

Known hazard, addressed here rather than inherited: creating a *function
cell* for a name today does not bump `Env.NameGen` (the NameGen func-cell
gap), which is exactly the class of invalidation a callee cache must
observe. The IC's guard must therefore be cell-version-based (bumped on
every write to the specific cell it caches), not name-generation-based.

## What Changes

- A fused global-call instruction: `GET_GLOBAL site` followed by `CALL argc`
  at a compile-time-known site is emitted as one instruction carrying the
  site index and argc.
- Its dispatch consults the site's existing versioned snapshot; on a hit
  where the cached value is a `*Closure`, it pushes the closure frame
  directly — no callee push onto the value stack, no generic type switch.
  On a miss (version change, non-closure callee, tombstone) it re-resolves
  through today's exact path and falls back to generic apply semantics.
- The guard is the cell mutation version already maintained for versioned
  reads; no reliance on `Env.NameGen`, sidestepping the func-cell gap. The
  Lisp-2 head-resolution order (function cell first, value fallback) is
  preserved by caching only what today's head resolution would produce, and
  never caching a value-cell fallback (same rule the engine call cache
  follows).
- Chunk validation covers the new instruction's operands.

## Impact

- Affected specs: `bytecode-vm` (Resolved global bindings — call-site use;
  Bytecode VM robustness — validation).
- Affected code: `core/compiler/compiler.go`, `core/vm/opcode.go`,
  `core/vm/vm.go`, `core/vm/chunk.go`.
- Expected: fib −4-8% (two global resolutions + one generic dispatch saved
  per recursion, ×2 sites); Rule minor; no allocation change.
- Risk: rebind/hot-reload correctness — pinned by the same redefinition
  scenarios the resolved-globals requirement already carries; the fused op
  must observe a mid-argument rebind exactly as the frozen-at-head-
  resolution semantics dictate today (arguments are evaluated after the
  head freeze in the current sequence — the fused form keeps that order).
