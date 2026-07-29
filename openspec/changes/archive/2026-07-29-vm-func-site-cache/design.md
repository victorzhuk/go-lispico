# Design — vm-func-site-cache

## Constraints carried from the inline-cache rejection

`vm-global-call-inline-cache` was fully built and rejected at its perf gate
(+4.15% fib against a −4% target). Its post-mortem
(openspec/changes/archive/2026-07-29-vm-global-call-inline-cache/design.md)
attributes the loss to dispatch restructure — new opcodes, a two-phase
freeze, extra state threaded through the hot switch — on a VM whose generic
path was already cheap. The same session's same-chunk frame-sync drop
(vm-call-frame-fast-path task 2.1) confirmed the pattern: extra branches in
hot dispatch cases cost more than the work they elide.

This change therefore:

1. Adds **zero** opcodes and **zero** branches to dispatch cases that do not
   already resolve a name. The only code change inside `run` is replacing a
   `GetFuncCanonical` call with a resolver call in `OpGetFunc` and
   `OpFreezeNativeFunc` (plus the `fo.Func` branch of
   `dispatchFusedNativeOp`, which is already out-of-line).
2. Reuses the exact `resolveGlobalValue` shape — the one site-cache form
   with a proven negative-cost record in this loop.
3. Rejects itself at the gate if fib does not clear −10%: unlike the IC,
   the baseline here pays a lock + map walk per resolution, so if replacing
   that with three atomic loads does not show up, the premise was wrong and
   the change must not merge on hope.

## Why the value-namespace guard set transfers

`resolveGlobalValue` serves a hit only when all of
{`entry.env == env`, `entry.gen == env.NameGen()`,
`entry.ver == cell.Version()`} hold, publishes only cells owned by
`vm.globals` that are live at publish time, and on a version mismatch falls
back to a locked `ReadCell` of the remembered cell without republishing.
The function namespace satisfies the same invariants:

- **Redefinition keeps cell identity.** `SetFunc`/`SetFuncCanonical` mutate
  the existing `*Cell` in place under the env write lock and bump
  `cell.version` (core/env.go:651-653, 686-688). A cached snapshot can
  never serve a stale value or a stale canonical flag: the version check
  fails and the locked re-read of the same cell returns the live pair.
  `defun` rebinding a canonical operator (canonical→false) and the engine's
  canonical bridge re-marking it (false→true) are both plain version bumps.
- **Deletion/unload/hot-reload tombstone in place.** Same contract the
  runtime call cache documents (runtime/call_cache.go): these paths null
  the cell's value under the write lock and bump its version. The version
  check fails, the locked re-read sees not-live, and the resolver falls
  through to the `GetFuncCanonical` walk, reporting undefined exactly as an
  uncached read would.
- **Rebuild is the only cell-identity drop, and it bumps NameGen.**
  `Env.Rebuild` (core/env.go:844-900) moves live cells — identity
  preserved — and drops only tombstoned ones, then bumps `newNameGen`. A
  site entry holding a dropped cell re-resolves.

  Correction from implementation (verified by
  `TestVM_FuncSite_RebuildDropThenRebindReturnsFreshValueWithoutRepublish`):
  it re-resolves through the *version* arm, not the generation arm. The
  delete that precedes `Rebuild` tombstones the cell and bumps its
  version, so `ver != entry.ver` matches first; that arm does a locked
  `ReadCell` of the remembered (now dead) cell, finds it not live, and
  falls through to the `GetFuncCanonical` walk, which returns the fresh
  cell's value. It returns before reaching the publish block, so the stale
  entry is left in place and the site never re-warms — every later
  execution of that site repeats the walk. The answer is correct; the site
  is permanently deoptimized after a delete+`Rebuild`+rebind cycle. This
  is pre-existing behavior of the identical value-namespace arm in
  `resolveGlobalValue`, not something this change introduces, and the
  sequence is a rare hot-reload shape. Out of scope here; worth a separate
  change if hot-reload-heavy consumers show it.
- **The NameGen func-cell gap is not load-bearing here.** Creating a fresh
  function cell (first `SetFunc` of a name, lazy-layer materialization)
  does not bump `Env.NameGen` — the known gap. It cannot produce a stale
  serve: a fresh cell can only shadow a *parent* or *lazy* resolution, and
  the resolver never publishes those (root-env-local live cells only; every
  other resolution takes the walk each time). The one sequence that maps an
  old entry to a re-bound name — publish → tombstone → Rebuild drops the
  cell → fresh `SetFunc` recreates it — passes through Rebuild's NameGen
  bump, which already invalidates the entry. The gap stays a hazard only
  for caches that publish non-local resolutions; this design must not, and
  a test pins that a parent-resolved or lazy-resolved name is never
  published.
- **Lazy materialization is safe to publish through.** `FuncCellLocal`
  (core/env.go:565-581) already exists and mirrors `CellLocal` exactly: a
  locked probe of `e.funcs`, and on a miss one `LookupAndMaterialize`
  consult followed by a re-probe. The layer contract requires the
  materializer to install the binding into that same env before returning
  true, so the re-probe yields a genuinely local live cell — publishing it
  is the same operation the value namespace already performs through
  `CellLocal`. No new primitive is needed and the walk is not the
  materialization path.

## Site keying

`buildSites` currently assigns one entry per distinct constant index. The
function namespace must not share entries with value reads of the same
symbol — under Lisp-2 they resolve different cells. Entries are deduplicated
per (constant index, namespace); the `idx` instruction map is unchanged in
shape (instruction → entry), so `chunk.site(ip-1)` keying works untouched
for all site-bearing forms.

The fused path is where the CL win actually lives, and it is currently
excluded outright: `buildSites` skips any `OpFusedNativeOp` whose
`c.Fused[a].Func` is true (core/vm/chunk.go:212), so those instructions get
`idx[ip] == -1` and `chunk.site(ip-1)` returns nil. `fuseNativeOp` sets
`Func: c.dialect.IsLisp2()` (core/compiler/compiler.go:726), so under CL
every two-operand arithmetic and comparison head — `(< n 2)`, `(- n 1)`,
`(- n 2)` in fib — lands in exactly that excluded branch and resolves
through `GetFuncCanonical` on every execution. Removing the skip and
routing those instructions into the function-keyed bucket is a required
part of this change, not an optional extension; without it the resolver
would only serve the far rarer unfused `OpGetFunc`/`OpFreezeNativeFunc`
sites and the fib gate would not move.

## What is explicitly out of scope

- Fusing resolution into call dispatch (the rejected IC).
- Caching in the tree-walker: parity comes from the crossval suite, not
  from mirroring the mechanism.
- The `gate-corpus-cl-and-recursion` change adds CL rows to the release
  gate; this change's local fib evidence stands on the interleaved A/B
  until that lands.
