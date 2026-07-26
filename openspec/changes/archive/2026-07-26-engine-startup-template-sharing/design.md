## Context

Startup cost decomposes as 3.5µs engine+dialect / 17µs `Use(stdlib)` / 19µs
first-eval+Close. The middle band is pure redundancy: identical `GoFunc`
closures rebuilt and re-written over an already-correct process template.
The first band contains per-engine dialect reconstruction (delta chain,
vocab map, `resolve()`, SHA-256). The infrastructure to share both exists;
nothing consults it before rebuilding.

## Decision 1: completed-layer short-circuit in Use

`stdlibLazyTemplateRegistry` already keys layers by
`{dialectFP, pluginName}`. Add a `complete` flag set when the first `Init`
returns without error, guarded by a per-key `sync.Once`-equivalent
(single-flight): the first `Use` in a process runs `Init` and completes the
layer; every later `Use` with the same key attaches without running `Init`.

Failure handling: an `Init` error leaves the layer incomplete and propagates
to that `Use` caller; the next `Use` retries construction. No partially
complete layer is ever attached.

Scope guard: the short-circuit applies only to plugins that register through
the template registry (stdlib's path). A plugin whose `Init` writes directly
into the engine env cannot be skipped — detected by the existing registration
route, fail-closed to per-engine `Init`.

Identity: `Metadata()` already carries name+version; the layer key gains the
version so a process hosting two builds of one plugin name fails closed
(distinct layers) instead of serving the wrong closures.

## Decision 2: memoized stock dialects

`cl.Dialect()`/`clojure.Dialect()` become `sync.Once`-backed package
singletons returning the same immutable value. `resolve()` and
`Fingerprint()` cache their outputs on first computation (the memoized
constructor forces both eagerly, so the shared value is deep-immutable
before it escapes).

Isolation audit (required, not assumed): the resolved form table and vocab
map must never be written after construction. Engine-level operator
redefinition already goes through env cells
(`engine-preserve-operator-redefinition`), not the dialect table; the audit
task greps every write site of the resolved map and pins it with a
mutation-attempt test. The dialect spec's "Per-Engine dispatch isolation"
requirement stays satisfied because isolation was only ever observable
through divergent *dialects*, which still get distinct resolutions.

A user-constructed custom dialect (delta chain built by hand) is untouched —
memoization covers the stock constructors only; `Fingerprint()` caching
inside a `Dialect` value benefits custom dialects on their engine's second
use of the same value.

## Decision 3: first-eval tail — attribute before touching

The 19µs tail mixes: first-touch materialization of the names the source
uses (`+` here), tokenizer+compile of the user source, cache admission,
cold `vm.New` on first pool Get, and `Close`. Which dominates is unmeasured;
the tasks profile it first. Expected outcomes:

- If compile dominates: the process-level chunk tier already shares
  plugin-source chunks whose expansion is dialect-determined. A user-source
  chunk is shareable under the same condition — the source's expansion uses
  no engine-defined macros. A fresh engine that has defined no macros
  satisfies this trivially (macro epoch at initial state); that is exactly
  the startup shape. Fail-closed classifier: reuse only at initial macro
  epoch under an identical fingerprint.
- If materialization dominates: batch the env writes for the names one
  source touches, or pre-materialize the canonical operator set eagerly from
  the shared template (cheap once closures are shared).
- If `vm.New` shows up: pre-size on engine construction instead of first
  call.

Each is small; none is speculative until the profile says which pays.

## Startup target

`New + Use(stdlib) + eval "(+ 1 2)" + Close` on the article harness under
10µs for the second-and-later engine in a process. First engine in a process
keeps today's cost (it builds the shared state). GopherLua comparison point:
70.8µs; goja's 2.5µs stays ahead — its global surface is lazy-materialized
JS builtins with no stdlib equivalent loaded.

## Alternatives rejected

- **Serialize/snapshot the root env (Janet image model):** pays
  deserialization per engine; our template is already live Go objects —
  sharing beats replaying.
- **Copy-on-write whole-env clone:** the lazy materializer already gives
  finer-grained first-touch semantics; a COW env adds aliasing complexity
  for no additional win.
- **Making all plugins shareable:** frozen plugins (`llm`, `net`, ...) have
  I/O and state; only registration-pure plugins qualify. Scope stays stdlib
  (+ `json` if its registration is template-routed) with the fail-closed
  route detection.
