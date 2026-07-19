# Design — stdlib-lazy-materialization

## Blueprint (verified in goja source)

goja: package-level `sync.Once` templates (`createObjectTemplate` → name →
`func(r *Runtime) Value`), per-Runtime thin objects, properties materialized on
first `getOwnPropStr` from the template, cached per-Runtime thereafter.
Runtime construction executes no stdlib code. Measured effect: ~2.5 µs
`goja.New()` vs lispico's 74–120 µs engine+stdlib.

Lispico's translation: the template's payload is not a JS property factory but
either (a) a Go builtin `Value` (shared, immutable by convention) or (b) a
compiled artifact from `stdlib-startup-cache` (chunk tree to execute once into
this engine to produce the defined value/macro).

## Template layer on the root env

The root `Env` gains an optional fallback consulted **only on a miss** of the
normal lookup:

```
lookup(name): vars hit → done (steady state, site-cached)
              vars miss + template has name → materialize under env write lock
                → re-lookup (now a hit)
              else → undefined
```

Materialization under the env's write lock gives at-most-once per name per
engine and thread safety for free (concurrent first-touch of one name: one
materializes, others block then hit). Deadlock guard: materializing a pure-Lisp
definition executes its chunk, which may itself miss other names —
materialization must therefore run WITHOUT holding the lock across execution:
reserve the name (placeholder cell + in-progress flag) under the lock, execute
outside it, publish under the lock. A recursive first-touch of the same name
from its own definition (pathological) resolves via the reservation, matching
eager-load order effects. This reservation dance is the riskiest part of the
design — it gets dedicated concurrency tests.

## What forces materialization

Every observation surface, enumerated and tested:

- value-cell `Get`/`GetCanonical` and function-cell `GetFunc*` (Lisp-2)
- macro-table lookup during `MacroExpand`/compilation — a template entry
  flagged as a macro materializes when the expander asks
- VM site publication (`CellLocal` on the root env) — goes through the same
  miss path
- enumeration surfaces: plugin binding listing, REPL completion, anything
  iterating root bindings — these force the full template (documented cost:
  first enumeration ≈ today's eager load)
- `UnloadPlugin`: removes the template layer AND all names it materialized
  (the existing unload bookkeeping tracks plugin-owned names; template names
  join it at materialization)
- shadowing: user `def` of a stdlib name writes the real cell; the template is
  never consulted again for that name (normal lookup hits first). `Delete` of
  a shadowing def must NOT resurrect the template silently if eager load would
  have left the stdlib binding — parity question pinned by a crossval cell
  (eager: delete removes the stdlib binding → undefined; lazy must tombstone
  the template entry too).

## Canonical flags and dialect surface

Eager load marks canonical operators at registration. Materialization must set
the identical flags (canonical, Lisp-2 func-cell mirroring) — the native-op
goldset/crossval cells run with lazy on and off to prove it. The template is
keyed by dialect fingerprint, sharing `stdlib-startup-cache`'s key.

## Trigger

Implement only if, after `stdlib-startup-cache` lands: warm startup remains
above target (GopherLua's 70 µs comfortably beaten but the per-request
embedder profile still shows env population dominating), or the article's
startup row still trails goja by an order of magnitude AND that gap is judged
worth the miss-path complexity. Otherwise archive unimplemented with the
measured numbers as the record.

## Rejected

- Copy-on-write shared root env (fork a populated env per engine): breaks the
  ownership model (engines mutate their root freely — canonical flags,
  `Bind`, `set!`), and CoW envs poison the versioned-cell fast path.
- Lazy Go-builtin registration only (skip pure-Lisp defs): builtins are the
  cheap half; the win is skipping execution of the pure-Lisp layer.
