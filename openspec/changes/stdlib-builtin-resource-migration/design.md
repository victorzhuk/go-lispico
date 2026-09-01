## Context

The core accounting foundation deliberately proves the primitive without making
every semantic change wait for a whole-stdlib audit. This successor runs after
`get-in`, CL adapters, and nil boundaries reach their final shapes, avoiding
inventory churn and duplicated kernel migrations.

## Goals / Non-Goals

**Goals:**

- Prove every active Builtin has complete work and result ownership.
- Keep callback evaluation owned by evaluator re-entry while budgeting separate
  extraction, copying, traversal, scheduling, and result construction.
- Eliminate unclassified core-owned opaque work and false borrowed allocation.
- Preserve operation values, typed errors, callback counts, and Terminal
  precedence from predecessor changes.

**Non-Goals:**

- Change collection, lookup, CL, nil, or typed-error semantics.
- Meter blocking I/O or arbitrary methods implemented by trusted host Go values.
- Require equal Reduction counters across Evaluator and VM.

## Decisions

### Freeze the final dispatch/work surface

Every active name appears exactly once below. The implementation stores this
table as test data and fails when registration adds or removes a name without an
inventory amendment.

| Work family | Final active names | Required disposition |
| --- | --- | --- |
| Variadic numeric/comparison | `+`, `-`, `*`, `/`, `max`, `min`, `=`, `<`, `>`, `<=`, `>=` | Budget argument traversal; make core-owned deep equality interruptible; mark host `Value.Equals` as trusted boundary |
| Fixed numeric | `mod`, `quot`, `pow`, `sqrt`, `abs`, `floor`, `ceil`, `zero?`, `pos?`, `neg?` | Bounded dispatch |
| Type/conversion | `type`, `nil?`, `bool?`, `int?`, `float?`, `string?`, `keyword?`, `symbol?`, `list?`, `vector?`, `map?`, `fn?`, `macro?`, `str->keyword`, `keyword->str`, `int->float`, `float->int` | Bounded dispatch; host `Value.Type` is a trusted boundary |
| Collection/lookup | `list`, `concat`, `reverse`, `vector`, `hash-map`, `first`, `rest`, `last`, `nth`, `count`, `cons`, `conj`, `empty?`, `get`, `get-in`, `assoc`, `keys`, `vals`, `contains?`, `merge`, `dissoc`, `sort`, `range` | Budget each uninterrupted traversal/build phase; replace flatten/sort/hash helpers or impose reviewed deterministic bounds |
| Callback/re-entry | `map`, `filter`, `reduce`, `apply`, `assert` | Re-entry owns callback evaluation; budget extraction, alignment, copying, formatting, and result construction separately |
| String/format | `str`, `format`, `string/join`, `string/split`, `string/trim`, `string/upper`, `string/lower`, `string/replace`, `string/contains?`, `string/starts-with?`, `string/ends-with?`, `string/length`, `string/lines`, `string->int`, `string->float` | Replace, chunk, or deterministically bound scans, parsing, Unicode conversion, formatting, and construction; host value formatting is a trusted boundary |
| CL adapters | CL `nth`, `mapcar`, `sort` | Verify predecessor budget ownership over shared kernels; do not add a second charge |

### Freeze result ownership classes and branches

The executable inventory records every successful return branch, not merely one
row per function. These are the closed classes:

| Result class | Required charge | Representative/final branches |
| --- | --- | --- |
| Computed scalar or shared singleton | Central shallow fallback unless already preboxed/accounted | numeric/comparison results, predicates, counts/lengths, parsed numbers, `assert` success |
| Wholly borrowed or already callback-accounted | Explicit zero-byte callee charge | `first`, `last`, successful/default `nth`, `get`, `get-in`, empty-path subject, List-tail `rest`, direct conversion views, final `reduce`/`apply` callback result |
| Fresh container over existing/already-accounted elements | Charge newly allocated container/storage, not borrowed or callback-accounted payload again | `concat`, `reverse`, vector `rest`, `keys`, `vals`, `sort`, `map`, `filter`, CL `mapcar`/`sort` |
| Incremental persistent result | Charge only copied/new nodes and newly owned payload | `cons`, `conj`, `assoc`, `dissoc`, shared-tail `concat` |
| Fresh deep result | Charge the deep result under the existing builder rule | `list`, `vector`, `hash-map`, `merge`, `range` |
| Mixed/conditional string result | Branch on actual ownership and charge only fresh bytes/storage | `str`, `format`, `string/join`, `split`, `trim`, case conversion, `replace`, `lines`, string/keyword views |

Before implementation edits, each return in the registered functions and
transitive helpers is mapped to one row. Mixed branches record which backing
bytes are shared and which are fresh. A static test rejects an unclassified
return or a name absent from the dispatch table.

### Treat opaque work honestly

Core-owned flattening, sorting, hashing/equality, formatting, string, Unicode,
and parse paths are rewritten as interruptible kernels or rejected before entry
by a deterministic bound. A pre/post context check is insufficient. Calls into
host-provided Go `Value` methods are recorded as trusted-host boundaries because
the runtime cannot preempt arbitrary Go extension code safely.

### Keep dynamic policy on the active evaluator

Shared kernels receive the GoFunc's active evaluator. Collection-length and
construction-depth checks query it directly; a child environment with no local
evaluator cannot silently fall back to defaults.

## Risks / Trade-offs

- [Risk] The inventory misses a helper-only loop or return → combine registration
  comparison with package call-graph/static checks and reviewed exceptions.
- [Risk] Callback work is charged twice → separate callback execution from
  copying/alignment/result phases in the inventory.
- [Risk] A zero-byte marker hides fresh backing storage → require branch-level
  ownership evidence, especially for strings and persistent collections.
- [Risk] Rewriting an opaque kernel changes behavior → retain predecessor
  goldens for values, stable ordering, typed errors, and callback counts.

## Migration Plan

Freeze executable inventories first, add failing family tests, then migrate one
family at a time without changing language semantics. Rollback is family-local;
the core accounting primitive remains because lookup and CL adapters already use
it.
