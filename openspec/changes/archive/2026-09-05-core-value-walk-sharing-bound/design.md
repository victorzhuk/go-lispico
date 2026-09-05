## Context

See `proposal.md` for motivation. Existing value walks are depth-bounded but traverse shared values once per reference. Existing host APIs cannot return errors, while evaluation and compilation must observe Terminal conditions during long walks.

## Goals / Non-Goals

**Goals:**

- Bound every value walk by work derived from the allocation ledger.
- Observe reduction exhaustion, absolute deadlines, and cancellation at most 127 completed work units after they become observable.
- Preserve exact behavior for values within both structural-depth and work bounds.
- Remove `unbounded-tracked` from the work inventory.

**Non-Goals:**

- Breaking existing `Value` method or metering function signatures.
- Changing container ordering or representation.
- Licensing performance tiers from developer-machine measurements.

## Decisions

### Work budget

Use a non-allocating traversal budget. One work unit represents `MeterValueSlotBytes` (16 bytes). The default ceiling is `DefaultMaxAllocationBytes / MeterValueSlotBytes` (4,194,304 units); evaluator-specific ceilings derive from its allocation limit. Synchronize with existing evaluation state every 128 units. Rendering reserves `ceil(bytes/16)` units before appending output, so repeated shared output cannot escape the bound.

Keep structural depth at `MaxStructuralDepth`. Do not add identity maps, reflection, unsafe keys, per-node atomics, or clock reads.

### Contextual and host surfaces

Add contextual entry points returning `(result, error)` for string rendering, deep bytes, node count, construction depth, and nested-element depth. Evaluation, stdlib, compiler, and runtime callers migrate to them. Contextual walks return the existing Terminal class with precedence: reduction resource limit, then absolute deadline, then cancellation.

Existing context-free host entry points keep their signatures. When their work ceiling is exceeded they degrade safely: rendering truncates, equality returns false, byte/node walks return capped partial values, and depth checks return their existing bounded answer.

### Caller ordering

Callers validate inputs, run contextual estimation/depth/size work, charge allocation, then publish results. A Terminal error wins over pending domain/type errors at final synchronization. No result, charge, formatted output, compiled chunk, or cache entry is published after a failed contextual walk.

### Inventory

Remove `unbounded-tracked` from declared dispositions and reconciliation logic. Reclassify scalable walk phases as `budgeted`. Reclassify opaque `fmt.Sprintf` render assembly as a bounded exception after successful contextual estimation and pre-charge, with a 67,108,864-byte ceiling. Remove stale owner tokens, including the archived format-mismatch owner.

### Measurement

Preserve the canonical before case: a ten-element list self-consed 26 times, depth 27, charged bytes 9,536, `ValueDeepBytes` 24,159,191,024, rendered length 1,476,395,007, render 14.69s, construction-depth progression 3.3µs/132µs/6.8ms/563ms/1.68s. Capture post-change data with the same fixture and parameters. Record deterministic ledger movement separately from Go benchmark allocation/latency. Do not overwrite hosted performance profiles.

Testing mode is existing-service strict TDD: sealed behavior tests precede production changes; targeted verification follows each chunk; repository, race, vet, lint, and gold-set floors run after integration.

## Risks / Trade-offs

- Exact output outside the work cap is intentionally replaced by bounded host degradation or contextual `CodeResourceLimit`.
- New contextual APIs widen internal call chains; every caller must migrate in one clean cutover.
- Work synchronization may move reduction accounting and deterministic gold cells; every movement requires before/after evidence.
- `fmt.Sprintf` remains non-interruptible internally, so pre-estimation and pre-charge must establish its bounded exception.

## Implementation plan

Dispatch order, one worktree per parallel shard. Testing mode: existing-service-strict TDD (user-approved). Tier: heavy. Lenses: spec, quality, perf (hot walk loops over shared data; no auth/IO/package moves).

Standing rules for every stage: native file tools; batch reads; never re-read; a sealed red test is read-only once written — later stages run it, never edit it; Conventional Commits, repo-configured identity only; worktree assertion required in every yield; clean cutover, no shims.

- **chunk-01-compatible-context-declarations** (FIELD-FIRST, parallel, shard `core`, enabling declarations): declare `ValueStringContext`, `ValueDeepBytesContext`, `ValueNodeCountContext`, `CheckConstructionDepthContext`, `CheckNestedElementDepthContext` as behavior-neutral wrappers; every existing host signature unchanged; no caller migration. redRun: `go test -timeout 2m ./core/... -run '^$'`; verify adds `go list -deps ./core/...` + lint.
- **chunk-02-before-measurement** (parallel, shard `goldset-measure`, task 1.2, NO-RED-WAIVER: measurement-only baseline): record the 10-scalar + 26-Cons fixture before figures (charged 9,536 B; ValueDeepBytes 24,159,191,024; String 1,476,395,007 chars / 14.69s; construction depth 1.677s) via constructor-only seeding.
- **chunk-03-occurrence-work-bound** (serial after chunk-01, sharedPkg `core`, tasks 1.1/2.1): RED first — seven walk-level tests (`TestValueWalk_WorkCap`, `_HostDegradation`, `_RenderReservation`, `_TerminalClasses`, `_TerminalPrecedence`, `_OrdinaryParity`, `_DepthBoundary`) sealed in `core/depth_walk_budget_test.go`; then the non-allocating occurrence budget (1 unit/occurrence, default 4,194,304, 128-unit sync, ceil(bytes/16) render reservation, saturating). Mid-walk Terminal proof: reduction remainder 192 units on the 352-visit fixture → `*core.LispicoError` `CodeResourceLimit` at the second sync; precedence reduction > `context.DeadlineExceeded` > `context.Canceled`; observable lag ≤ 127 units. Host rulings: capped partials exclude the refused occurrence; host depth checks keep their depth-only answer; `CodeResourceLimit` contextual-only.
- **chunk-04-contextual-caller-publication** (serial after chunk-03, sharedPkg `core`, tasks 2.2/2.3): RED — author and seal `TestValueWalk_CallerPublication` in `runtime/value_walk_publication_test.go` (tree+VM, throw/assert/REPL/compiler/VM/stdlib/json publication), run-only over chunk-03 seals; then migrate every active caller (stdlib collections/strings/control, compiler, runtime, json) to contextual APIs; validate → contextual walk → charge → publish; core stays zero-dep (`go list -deps ./core/...`).
- **chunk-05-inventory-retirement** (parallel, shard `inventory`, task 3.1): RED seals the inventory guards to reject any `unbounded-tracked` row/stale owner token; then reclassify rows budgeted, render assembly `bounded-exception` MaxWork 67108864, update `invOpaqueQualified`, delete the disposition and tracked-change-only machinery.
- **chunk-06-charge-and-goldset-settlement** (parallel, shard `goldset-settle`, task 3.2, NO-RED-WAIVER: measurement only): compare against chunk-02 baseline, update only deterministic charge cells that demonstrably move, paired goldset benchmarks (GOMAXPROCS=2, 200ms), no silent rebaseline.
- **chunk-07-repository-floor** (parallel, shard `floor`, task 3.3, NO-TESTER-WAIVER: command-only gate): the full floor below, retaining paired gold-set output.

Floor: make lint && make test && go test -race -count=1 -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make profile && GOMAXPROCS=2 GOLDSET_MODE=eval go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem && GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem

Risks (from the design packet): occurrence budgeting refuses large shared graphs by design (exactness only within both caps); render-reservation overflow handled saturating; budget bookkeeping must stay 0-alloc or the growth is removed; host `Value.String`/`Equals` remain non-preemptible trusted-host seams; frozen plugins keep their own host formatting.

Plan review: zarchitect, 4 rounds, verdict **pass** (blockers all repaired: red-ownership split, one-owner-per-seal, deterministic mid-walk seed, exact host-degradation rulings).

## Plan appendix

```json
{
  "v": 2,
  "change": "core-value-walk-sharing-bound",
  "baseSha": "6372c862856b6905964ecc94b8bb42049181b1c1",
  "generatedAt": "2026-09-04T19:28:11.436Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "chunks": [
    {
      "id": "chunk-01-compatible-context-declarations",
      "taskIds": [
        "1.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "core",
      "seam": "chunk-01-compatible-context-declarations",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "core/metering.go",
          "symbol": "ValueDeepBytes",
          "anchor": "func ValueDeepBytes(v Value) int64",
          "change": "declare ValueDeepBytesContext/ValueNodeCountContext as behavior-neutral (result, error) wrappers; host signatures unchanged"
        },
        {
          "task": "2.2",
          "file": "core/depth.go",
          "symbol": "checkDepthAt",
          "anchor": "func checkDepthAt(v Value, depth int, eval Evaluator) error",
          "change": "declare CheckConstructionDepthContext/CheckNestedElementDepthContext and an unexported context-bearing depth helper; no caller migration"
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "go-coder: add exactly `func ValueStringContext(ctx context.Context, v Value) (string, error)`, `func ValueDeepBytesContext(ctx context.Context, v Value) (int64, error)`, `func ValueNodeCountContext(ctx context.Context, v Value) (int, error)`, `func CheckConstructionDepthContext(ctx context.Context, v Value, env *Env) error`, and `func CheckNestedElementDepthContext(ctx context.Context, v Value, env *Env) error` as behavior-neutral wrappers over current host behavior; their initial error result is nil.",
        "go-coder: preserve exactly `Value.String() string`, `Value.Equals(Value) bool`, `ValueDeepBytes(Value) int64`, `ValueNodeCount(Value) int`, `ValueDepthExceeds(Value, int) bool`, `CheckConstructionDepth(Value, *Env) error`, `CheckConstructionDepthWith(Value, Evaluator) error`, `CheckNestedElementDepth(Value, *Env) error`, `CheckNestedElementDepthWith(Value, Evaluator) error`, and `EqualsBounded(Value, Value, *BuiltinWorkBudget) (bool, error)`.",
        "go-coder: where later callers already hold an Evaluator rather than an Env, plan an unexported context-bearing helper; do not add another exported walk API. Do not thread compiler/VM/throw/assert/REPL callers or alter behavior in this chunk."
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m ./core/... -run '^$'",
      "verify": "go test -timeout 2m ./core/... -run '^$' && go list -deps ./core/... && golangci-lint run ./core/...",
      "coder": "go-coder"
    },
    {
      "id": "chunk-02-before-measurement",
      "taskIds": [
        "1.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "goldset-measure",
      "seam": "chunk-02-before-measurement",
      "pkgDirs": [
        "internal/goldset"
      ],
      "pkgs": [
        "./internal/goldset"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "internal/goldset/goldset.go",
          "symbol": "Fixtures",
          "anchor": "func Fixtures",
          "change": "record before-baseline artifact for the 10-scalar + 26-Cons shared fixture using constructor-only seeding; no production change"
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "Record the 26-cons shared fixture before values in a checked-in benchmark/result artifact: 10-element base, 26 `List.Cons` operations, structural depth 27, 1,040 charged bytes, ValueDeepBytes 24,159,191,024, String length 1,476,395,007 and 14.69s, construction-depth 1.677s; these current figures are recorded at `openspec/changes/core-value-walk-sharing-bound/proposal.md:15-22` and `internal/inventory/work_data.go:497-520`.",
        "Use the same constructor-only fixture for before and after; do not seed `List.flat`, `List.shared`, `listNode`, counters, or evalState fields directly."
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m ./internal/goldset/... -run '^TestGoldset$'",
      "verify": "go test -timeout 2m ./internal/goldset/... -run '^TestGoldset$'",
      "coder": "coder"
    },
    {
      "id": "chunk-03-occurrence-work-bound",
      "taskIds": [
        "2.1"
      ],
      "prev": "chunk-01-compatible-context-declarations",
      "sharedPkg": "core",
      "parallel": false,
      "shard": "",
      "seam": "chunk-03-occurrence-work-bound",
      "pkgDirs": [
        "core",
        "plugins/stdlib",
        "plugins/json",
        "runtime"
      ],
      "pkgs": [
        "./core",
        "./plugins/stdlib",
        "./plugins/json",
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "core/depth_test.go",
          "symbol": "TestSharedListWalkBoundedWork",
          "anchor": "TestSharedListDepthIsNotChainLength",
          "change": "Add regression: a 10-element scalar base returned through exactly 5 `List.Cons(previous)` calls (depth 6, 5,767,168-cons measurement omitted; logical visits cap-bound) must complete each walk (boundedString, boundedDeepBytes, boundedNodeCount, checkDepthAt) within a stated work ceiling, not grow with references. Add context/deadline/budget observability assertions mid-walk. The 26-cons fixture is reserved for chunk-02/chunk-06 measurement only."
        },
        {
          "task": "1.1",
          "file": "plugins/stdlib/strings_budget_test.go",
          "symbol": "TestStrings_ToStringWalkIsUnboundedTracked",
          "anchor": "func TestStrings_ToStringWalkIsUnboundedTracked",
          "change": "Keep existing test; extend with a shared-structure regression that proves the walk completes in bounded time/work once task 2.1 lands, converting from unbounded-tracked assertion to bounded-proof assertion."
        },
        {
          "task": "1.1",
          "file": "plugins/stdlib/strings_budget_test.go",
          "symbol": "TestStrings_FormatEstimatorWalkIsUnboundedTracked",
          "anchor": "func TestStrings_FormatEstimatorWalkIsUnboundedTracked",
          "change": "Keep existing test; extend with a shared-structure regression for formatStringBytes and estimateFormatValueBytes walk, converting from unbounded-tracked assertion to bounded-proof."
        },
        {
          "task": "1.1",
          "file": "plugins/stdlib/strings_budget_test.go",
          "symbol": "TestStrings_ToAnyRenderIsUnboundedTracked",
          "anchor": "func TestStrings_ToAnyRenderIsUnboundedTracked",
          "change": "Keep existing test; extend with shared-structure regression for toAny v.String render walk, converting from unbounded-tracked to bounded-proof."
        },
        {
          "task": "2.1",
          "file": "core/depth.go",
          "symbol": "boundedDeepBytes",
          "anchor": "func boundedDeepBytes(v Value, depth int) int64",
          "change": "Add non-allocating occurrence/work budget: one work unit per 16 bytes (MeterValueSlotBytes), default 4,194,304 units, evaluator-derived lower ceiling. Synchronize with evalState every 128 units. Do not add identity maps, visited-node sets, or per-node atomics. Same pattern for boundedString (141), boundedNodeCount (300), boundedEquals (190), and constructionDepthExceeded (69)."
        },
        {
          "task": "2.1",
          "file": "core/depth.go",
          "symbol": "boundedString",
          "anchor": "func boundedString(v Value, depth int) string",
          "change": "Add non-allocating work budget with ceil(renderedBytes/16) render reservation before appending output. Stop with truncation marker when budget exhausted."
        },
        {
          "task": "2.1",
          "file": "core/depth.go",
          "symbol": "boundedNodeCount",
          "anchor": "func boundedNodeCount(v Value, depth int) int",
          "change": "Add non-allocating work budget. One unit per node visit, synchronize every 128 units, return capped partial count when budget exhausted."
        },
        {
          "task": "2.1",
          "file": "core/depth.go",
          "symbol": "constructionDepthExceeded",
          "anchor": "func constructionDepthExceeded(v Value, depth, limit int) bool",
          "change": "Add non-allocating work budget. A wide, shallow, heavily shared structure must terminate within the stated ceiling. Synchronize every 128 units with evalState."
        },
        {
          "task": "2.1",
          "file": "core/depth.go",
          "symbol": "boundedEquals",
          "anchor": "func boundedEquals(a, b Value, depth int) bool",
          "change": "Add non-allocating work budget. One unit per compared node pair, synchronize every 128 units. Return false when budget exhausted."
        },
        {
          "task": "2.1",
          "file": "core/metering.go",
          "symbol": "ValueDeepBytes",
          "anchor": "func ValueDeepBytes(v Value) int64",
          "change": "Preserve existing context-free signature. Add a separate contextual entry point returning (int64, error) that delegates to the bounded walk with non-allocating work budget. Document exact, separately named contextual identifier in design before any RED task names it."
        },
        {
          "task": "2.1",
          "file": "core/metering.go",
          "symbol": "ValueNodeCount",
          "anchor": "func ValueNodeCount(v Value) int",
          "change": "Preserve existing context-free signature. Add a separate contextual entry point returning (int, error) with non-allocating work budget."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "boundedEquals",
          "anchor": "func boundedEquals(a, b Value, depth int) bool",
          "change": "Explicit RED site: shared-work bounding for Equals under the non-allocating work budget. Seed through public NewList plus the 10-scalar base followed by exactly 5 returned-value `List.Cons(previous)` calls (`11 * 2^5 = 352` logical visits, deterministically above the 256-unit ceiling derived from MaxAllocationBytes 4,096). Use the same fixture for the contextual EqualsBounded Terminal-class assertion. Add a host-value trusted-boundary test asserting no step charge for host-owned Equals results. The 10-scalar + 26-Cons fixture remains reserved for chunk-02/chunk-06 measurement only; the 10-scalar + 19-Cons fixture is exclusively the context-free host-degradation fixture."
        },
        {
          "task": "1.1",
          "file": "core/depth_with_test.go",
          "symbol": "TestCheckNestedElementDepthWith_UsesEvaluator",
          "anchor": "func TestCheckNestedElementDepthWith_UsesEvaluator",
          "change": "Explicit RED site: nested-element depth under the shared/wide work cap. Assert contextual CheckNestedElementDepthWith returns Terminal at the work cap and the context-free host entry point returns its existing bounded answer."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "boundedString",
          "anchor": "func boundedString(v Value, depth int) string",
          "change": "Explicit RED site: context-free host degradation for String. When work ceiling is exceeded, host String SHALL return a truncation marker. Test with a shared structure whose charged allocation stays small while references double each step."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "boundedDeepBytes",
          "anchor": "func boundedDeepBytes(v Value, depth int) int64",
          "change": "Explicit RED site: context-free host degradation for deep bytes. When work ceiling is exceeded, host ValueDeepBytes SHALL return a capped partial value. Test boundary at 15/16/17-byte render-reservation (ceil(bytes/16) reservation before append)."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "boundedNodeCount",
          "anchor": "func boundedNodeCount(v Value, depth int) int",
          "change": "Explicit RED site: context-free host degradation for node count. When work ceiling is exceeded, host ValueNodeCount SHALL return a capped partial count."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "constructionDepthExceeded",
          "anchor": "func constructionDepthExceeded(v Value, depth, limit int) bool",
          "change": "Explicit RED site: context-free host degradation for construction depth. When work ceiling is exceeded, host CheckConstructionDepthWith SHALL return its existing bounded answer (not crash or return an arbitrary boolean)."
        },
        {
          "task": "1.1",
          "file": "core/depth.go",
          "symbol": "checkDepthAt",
          "anchor": "func checkDepthAt(v Value, depth int, eval Evaluator) error",
          "change": "Explicit RED site: context-free host degradation for nested-element depth. When work ceiling is exceeded, host CheckNestedElementDepthWith SHALL return its existing bounded answer."
        }
      ],
      "contract": {
        "states": [
          "logical occurrence count and hard work ceiling",
          "pending 128-unit synchronization batch and latched Terminal",
          "render-byte reservation",
          "exact, host-degraded, or Terminal-cleared result"
        ],
        "transitions": [
          {
            "input": "a core-owned walk visits one logical value occurrence",
            "state": "logical occurrence count and hard work ceiling",
            "effect": "set",
            "evidence": "The depth bound is not a work bound and shared references are visited once per occurrence (`specs/core-engine/spec.md:10-18`); no identity map is permitted (`design.md:19-25`)."
          },
          {
            "input": "a renderer knows a fragment byte length before append",
            "state": "render-byte reservation",
            "effect": "set",
            "evidence": "Reserve `(bytes + 15) / 16` with saturating overflow handling before append (`design.md:19-25`)."
          },
          {
            "input": "the next occurrence or render reservation would exceed max(1, effective MaxAllocationBytes/16)",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "Contextual APIs return `CodeResourceLimit`; unchanged host APIs use the sealed conservative degradation. The default is 67,108,864/16 = 4,194,304 units."
          },
          {
            "input": "a pending-batch check fires or a walk exits with a remainder",
            "state": "pending 128-unit synchronization batch and latched Terminal",
            "effect": "forced",
            "evidence": "Check budget/order at entry, synchronize before executing the unit that would make 128 since the last successful check, and assert 0–127 completed units after Terminal becomes observable. Charge reductions, then check absolute deadline, then cancellation through the existing budget order (`core/builtin_budget.go:25-64`)."
          },
          {
            "input": "a context-free host String or Equals walk reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "String returns its truncation marker and Equals returns false (`design.md:19-34`)."
          },
          {
            "input": "reduction exhaustion, expired absolute deadline, and cancellation are observable at one synchronization point",
            "state": "pending 128-unit synchronization batch and latched Terminal",
            "effect": "forced",
            "evidence": "Return reduction CodeResourceLimit before context.DeadlineExceeded before context.Canceled (`core/builtin_budget.go:49-64`; `design.md:27-34`)."
          },
          {
            "input": "a contextual walk returns Terminal while the caller holds a pending result, charge, output, chunk, cache entry, or non-Terminal error",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "clear",
            "evidence": "Terminal wins and nothing is published; required order is validate, contextual walk/estimate, charge, publish (`design.md:36-41`)."
          },
          {
            "input": "a context-free deep-byte or node-count walk reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "RULING: ValueDeepBytes and ValueNodeCount return the amount accrued at the capped point EXCLUDING the refused occurrence — a deterministic capped partial, never an error, never including the unit that would exceed. Exact expected values are computed from the sealed 19-Cons fixture (5,767,168 visits vs 4,194,304 cap) in the RED itself and frozen there."
          },
          {
            "input": "a context-free construction-depth or nested-element-depth check reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "RULING: CheckConstructionDepth, CheckConstructionDepthWith, CheckNestedElementDepth, CheckNestedElementDepthWith, and ValueDepthExceeds return exactly their existing depth-only answer (nil error / existing boolean) when the structure is within MaxStructuralDepth — no CodeResourceLimit from host entry points; CodeResourceLimit is returned ONLY by the contextual APIs. Work-ceiling exhaustion inside a host depth walk stops the walk and yields the depth answer computed from what was visited."
          }
        ],
        "forbidden": [
          "Identity maps/tables, representation-aware identity, sharing markers, shared-node no-ops, memoized heights, reflection, unsafe keys, per-node atomics, or per-node clock reads.",
          "Allocating traversal bookkeeping on any path.",
          "Returning an exact contextual result after either cap is exceeded.",
          "Completing more than 127 further units after a Terminal condition becomes observable.",
          "Appending output before `ceil(renderedBytes/16)` reservation succeeds.",
          "Changing any context-free host signature named in chunk 01.",
          "Starting production behavior before every chunk-03 RED task is sealed.",
          "A production coder editing sealed RED instead of returning AMEND."
        ],
        "seeding": [
          "Contextual low-cap fixture: `base := core.NewList(tenScalarValues)` followed by exactly 5 returned-value `List.Cons(previous)` calls. It has `11 * 2^5 = 352` logical visits, deterministically exceeding the 256-unit ceiling derived from MaxAllocationBytes 4,096 without large output.",
          "Context-free default-cap fixture: the same 10-scalar base followed by exactly 19 returned-value `List.Cons(previous)` calls. It has `11 * 2^19 = 5,767,168` logical visits, deterministically exceeding the default 4,194,304-unit ceiling without the 26-cons rendering size.",
          "Reserve the 10-scalar plus 26-Cons fixture exclusively for chunk-02/chunk-06 measurement; never use it in RED or host String degradation tests.",
          "Ordinary/depth fixtures use only public NewList/NewVector/NewHashMap plus Set/Assoc and one-element nesting at depths 1,024/1,025; never mutate private backing fields.",
          "Canceled via context.WithCancel, deadline via an already expired core.WithEvalDeadline with a live parent, and reductions via the real eval state/budget; never fake Terminal errors.",
          "Caller publication only through actual tree/VM Engine, GoFunc, json, compiler/cache, and REPL surfaces; never direct private field writes.",
          "MID-WALK TERMINAL SEED (deterministic, constructor-only): reduction exhaustion is the mid-walk proof — seed a real evaluator via core.WithEvalResourceLimits with an allocation remainder that covers fewer than the fixture's 352 logical units but more than one 128-unit batch (e.g. remaining budget = 192 units' worth of bytes); the walk charges reductions at each sync point, so *core.LispicoError CodeResourceLimit fires at the second sync, mid-walk, deterministically. Cancellation and deadline reuse the same in-walk sync points: context.WithCancel cancelled after the first sync via the walk's own callback-free design is replaced by asserting (a) an entry-cancelled and entry-expired context still stops within 127 completed units of the first sync and (b) the reduction seed exercises the identical mid-loop check site, so the check provably sits inside the walk loop, not at entry/exit only. No sleeps, no wall-clock dependence."
        ],
        "budgets": [
          "Occurrence cost: 1 unit per logical visit.",
          "Render reservation: ceil(rendered bytes/16) before append; 15/16/17 bytes reserve 1/1/2 units.",
          "Ceiling: max(1, effective allocation bytes/16), default 4,194,304 units, low-cap RED 256 units.",
          "Batch size 128; observable Terminal lag at most 127 completed units.",
          "Mid-walk proof budget: reduction remainder 192 units (1.5 batches) on the 352-visit fixture; observable lag asserted <= 127 units."
        ],
        "names": [
          "`func ValueStringContext(ctx context.Context, v Value) (string, error)`",
          "`func ValueDeepBytesContext(ctx context.Context, v Value) (int64, error)`",
          "`func ValueNodeCountContext(ctx context.Context, v Value) (int, error)`",
          "`func CheckConstructionDepthContext(ctx context.Context, v Value, env *Env) error`",
          "`func CheckNestedElementDepthContext(ctx context.Context, v Value, env *Env) error`",
          "Existing `func EqualsBounded(a, b Value, budget *BuiltinWorkBudget) (bool, error)`.",
          "Tests: `TestValueWalk_WorkCap`, `TestValueWalk_HostDegradation`, `TestValueWalk_RenderReservation`, `TestValueWalk_TerminalClasses`, `TestValueWalk_TerminalPrecedence`, `TestValueWalk_CallerPublication`, `TestValueWalk_OrdinaryParity`, `TestValueWalk_DepthBoundary`."
        ],
        "refusals": [
          "Contextual walks refuse at the work/structural cap with the exact Terminal and no result; callers refuse publication; hosts expose only documented bounded degradation."
        ]
      },
      "redTasks": [
        "go-test-writer: after chunk-01 declarations and chunk-02 baseline, create and seal the exact walk-level `TestValueWalk_*` tests (WorkCap, HostDegradation, RenderReservation, TerminalClasses, TerminalPrecedence, OrdinaryParity, DepthBoundary — NOT CallerPublication, which chunk-04 owns); their failure must be an assertion against current behavior, never an undefined-symbol compile failure.",
        "go-test-writer: cover String, EqualsBounded, ValueDeepBytesContext, ValueNodeCountContext, CheckConstructionDepthContext, and CheckNestedElementDepthContext at the 256-unit cap with only the 10-scalar plus 5-Cons fixture (`11 * 2^5 = 352` logical visits).",
        "go-test-writer: cover unchanged host degradation directly for String, Equals, ValueDeepBytes, ValueNodeCount, ValueDepthExceeds, CheckConstructionDepth/With, and CheckNestedElementDepth/With using only the 10-scalar plus 19-Cons fixture (`11 * 2^19 = 5,767,168` visits > 4,194,304 default); never use the 26-Cons measurement fixture here.",
        "go-test-writer: cover render reservations at 15/16/17 bytes (1/1/2 units), proving reservation precedes append.",
        "go-test-writer: cover context.Canceled, context.DeadlineExceeded, and *core.LispicoError CodeResourceLimit, combined precedence reduction > deadline > cancellation, and no more than 127 completed units after observability.",
        "go-test-writer: seal ONLY the walk-level tests (WorkCap, HostDegradation, RenderReservation, TerminalClasses, TerminalPrecedence, OrdinaryParity, DepthBoundary) in core/depth_walk_budget_test.go; the CallerPublication tests are authored and sealed by chunk-04, never here."
      ],
      "codeTasks": [
        "go-coder: only after every chunk-03 RED assertion is sealed, implement the five exact contextual APIs without changing any old signature; keep EqualsBounded's signature and apply the same hard occurrence ceiling through its supplied budget. Callers holding Evaluator use an unexported context-bearing depth helper, not another exported API.",
        "go-coder: add a scalar, non-allocating walk budget: one unit per logical occurrence, default 4,194,304, evaluator-derived lower ceiling, 128-unit batching, final flush on every exit, and no identity or memoization state.",
        "go-coder: reserve ceil(renderedBytes/16) before every append with overflow-safe arithmetic; 15/16/17 bytes consume 1/1/2 units and failed reservation publishes no bytes.",
        "go-coder: preserve exact traversal within both caps and implement the sealed context-free truncation/false/capped-partial/conservative-depth degradation outside the work cap. Never edit sealed RED; return AMEND on contradiction."
      ],
      "redTests": [
        "TestValueWalk_WorkCap",
        "TestValueWalk_HostDegradation",
        "TestValueWalk_RenderReservation",
        "TestValueWalk_TerminalClasses",
        "TestValueWalk_TerminalPrecedence",
        "TestValueWalk_OrdinaryParity",
        "TestValueWalk_DepthBoundary"
      ],
      "redRun": "go test -timeout 2m ./core/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... -run '^TestValueWalk_(WorkCap|HostDegradation|RenderReservation|TerminalClasses|TerminalPrecedence|OrdinaryParity|DepthBoundary)$'",
      "verify": "go test -timeout 2m ./core/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... -run '^TestValueWalk_(WorkCap|HostDegradation|RenderReservation|TerminalClasses|TerminalPrecedence|OrdinaryParity|DepthBoundary)$' && go vet ./core/... ./plugins/stdlib/... && golangci-lint run ./core/... ./plugins/stdlib/...",
      "coder": "go-coder"
    },
    {
      "id": "chunk-04-contextual-caller-publication",
      "taskIds": [
        "2.2",
        "2.3"
      ],
      "prev": "chunk-03-occurrence-work-bound",
      "sharedPkg": "core",
      "parallel": false,
      "shard": "",
      "seam": "chunk-04-contextual-caller-publication",
      "pkgDirs": [
        "core",
        "core/compiler",
        "plugins/stdlib",
        "plugins/json",
        "runtime"
      ],
      "pkgs": [
        "./core",
        "./core/compiler",
        "./plugins/stdlib",
        "./plugins/json",
        "./runtime"
      ],
      "sites": [
        {
          "task": "2.2",
          "file": "core/depth.go",
          "symbol": "checkDepthAt",
          "anchor": "func checkDepthAt(v Value, depth int, eval Evaluator) error",
          "change": "Preserve exported context-free signatures `CheckConstructionDepthWith(Value, Evaluator) error` and `CheckNestedElementDepthWith(Value, Evaluator) error` unchanged; they remain the ctx-free Evaluator entry points. Add exported `CheckConstructionDepthContext(ctx context.Context, v Value, env *Env) error` and `CheckNestedElementDepthContext(ctx context.Context, v Value, env *Env) error` for Env-context callers; add unexported context-bearing helpers (e.g. `checkConstructionDepthCtx`, `checkNestedElementDepthCtx`) for Evaluator callers that already hold a live context. The Env entry points delegate to the same underlying walk as the unexported Evaluator helpers. Context exhaustion is observable mid-walk. Terminal precedence: reduction CodeResourceLimit > context.DeadlineExceeded > context.Canceled. Max unobserved work: 127 completed units after observability."
        },
        {
          "task": "2.2",
          "file": "core/metering.go",
          "symbol": "ChargeEvalAllocBytes",
          "anchor": "func ChargeEvalAllocBytes(ctx context.Context, n int64) error",
          "change": "Walk functions use ctx to check evalState for reduction budget exhaustion via ChargeEvalReductions at sync points during traversal."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/collections.go",
          "symbol": "chargeCollectionResult",
          "anchor": "func chargeCollectionResult(ctx context.Context, eval core.Evaluator, name string, res core.Value, bytes int64) error",
          "change": "Pass ctx to the exported contextual `ValueDeepBytesContext` and `CheckConstructionDepthContext` entry points; never thread ctx through the preserved context-free `CheckConstructionDepthWith`. The callers at lines 43, 151, 178, 447, 558, 700, 867 already have ctx in scope."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/collections.go",
          "symbol": "chargeConsResult",
          "anchor": "func chargeConsResult(ctx context.Context, eval core.Evaluator, name string, res core.Value, bytes int64, newElems ...core.Value) error",
          "change": "Pass ctx to the exported contextual `CheckNestedElementDepthContext` entry point at line 1063; never modify the preserved context-free `CheckNestedElementDepthWith` signature."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/strings.go",
          "symbol": "toString",
          "anchor": "func toString",
          "change": "Pass ctx to the contextual boundedString call path for container rendering. toString is called from str and string/join builtins which have ctx."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/strings.go",
          "symbol": "toAny",
          "anchor": "func toAny",
          "change": "Pass ctx through to the contextual String rendering on non-scalar format arguments. toAny is called from format's render loop which has ctx."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/strings.go",
          "symbol": "estimateFormatAllocBytes",
          "anchor": "func estimateFormatAllocBytes(format string, args []core.Value) int64",
          "change": "Pass ctx to estimateFormatValueBytes and formatStringBytes, which reach the contextual ValueDeepBytes entry point. These run before the pre-charge and need context for mid-walk interruption."
        },
        {
          "task": "2.2",
          "file": "plugins/stdlib/control.go",
          "symbol": "registerControl",
          "anchor": "func (p *Plugin) registerControl",
          "change": "The assert builtin at line 31 passes an arbitrary core.Value to %.200v; the render walks via boundedString. Pass ctx through to the walk. ctx is already available in the GoFunc closure."
        },
        {
          "task": "2.2",
          "file": "core/compiler/compiler.go",
          "symbol": "compile",
          "anchor": "c.nodeCount += core.ValueNodeCount(form)",
          "change": "Pass context to the contextual ValueNodeCount entry point. The compiler has a context from the Engine."
        },
        {
          "task": "2.2",
          "file": "core/compiler/compiler.go",
          "symbol": "chunkDeepBytes",
          "anchor": "bytes += core.ValueDeepBytes(constant)",
          "change": "Pass context to the contextual ValueDeepBytes entry point in the chunk deep-bytes computation."
        },
        {
          "task": "2.2",
          "file": "runtime/eval.go",
          "symbol": "chunkDeepBytes",
          "anchor": "bytes += core.ValueDeepBytes(c)",
          "change": "Pass context to the contextual ValueDeepBytes entry point in runtime's chunk deep-bytes computation."
        },
        {
          "task": "2.2",
          "file": "plugins/json/plugin.go",
          "symbol": "Plugin.encode/decode dispatch",
          "anchor": "if err := core.ChargeEvalAllocBytes(ctx, core.ValueDeepBytes(res)); err != nil {",
          "change": "Concrete contextual-walk/publication callsite. Replace the context-free `core.ValueDeepBytes(res)` with the contextual exported `ValueDeepBytesContext(ctx, res)` (returns `(int64, error)`); the existing `ChargeEvalAllocBytes` already provides the per-call allocation charge. Treat Terminal from the contextual walk as refusal and return the wrap error before any caller-visible result is published."
        },
        {
          "task": "2.2",
          "file": "plugins/json/json_test.go",
          "symbol": "TestJSONEncodeDecode_RoundTripsAllocation",
          "anchor": "wantBytes := core.ValueDeepBytes(got)",
          "change": "Concrete test callsite. The expected allocation figure comes from the context-free host `core.ValueDeepBytes`. After chunk 03 contextualizes the production path, record the host-side context-free figure explicitly (parity at default cap) and assert the contextual `ValueDeepBytesContext` walks the same fixture under the same default cap. Do not assert a sharing-aware or memoized value."
        },
        {
          "task": "2.3",
          "file": "core/depth.go",
          "symbol": "package core",
          "anchor": "package core",
          "change": "Verify after all changes: go list -deps core/ shows only stdlib imports. Context threading uses context.Context (stdlib) and the existing evalState machinery (core/metering.go) — no new external deps."
        }
      ],
      "contract": {
        "states": [
          "live contextual walk or unchanged context-free host walk",
          "pending caller result/error/charge/output/chunk/cache",
          "Terminal identity",
          "published result"
        ],
        "transitions": [
          {
            "input": "active evaluation or compilation performs string, equality, deep-byte, node-count, construction-depth, or nested-depth work",
            "state": "live contextual walk or unchanged context-free host walk",
            "effect": "set",
            "evidence": "Evaluation and compilation must observe Terminal during the walk (`specs/core-engine/spec.md:14-26`); host APIs retain signatures (`design.md:27-34`)."
          },
          {
            "input": "reduction exhaustion is observable with deadline and/or cancellation",
            "state": "Terminal identity",
            "effect": "forced",
            "evidence": "Return the original `*core.LispicoError` with `CodeResourceLimit`; reduction precedes deadline and cancellation (`core/builtin_budget.go:49-64`)."
          },
          {
            "input": "deadline and cancellation are observable without reduction exhaustion",
            "state": "Terminal identity",
            "effect": "forced",
            "evidence": "Return `context.DeadlineExceeded` unchanged before `context.Canceled` (`core/builtin_budget.go:49-64`)."
          },
          {
            "input": "a contextual walk or final flush fails after validation but before publication",
            "state": "live contextual walk or unchanged context-free host walk",
            "effect": "clear",
            "evidence": "Terminal overrides pending domain/type errors and clears result; finishBuiltin/finishAdapter and FinishEval preserve Terminal identity (`plugins/stdlib/charges.go:11-16`; `runtime/eval.go:392-396,682-685,914-918`)."
          },
          {
            "input": "all contextual estimation/depth/size work and allocation charge succeed",
            "state": "published result",
            "effect": "set",
            "evidence": "Required order is validate -> contextual walk -> charge -> publish (`design.md:36-41`)."
          }
        ],
        "forbidden": [
          "Routing throw, assert, REPL, compiler, VM, stdlib, json, cache, or format output through a context-free walk while a live evaluation context exists.",
          "Changing or removing old context-free host signatures.",
          "Wrapping Terminal identities in a non-Terminal LispicoError or exposing them to Lisp try/catch.",
          "Publishing a result, allocation charge, formatted output, compiled chunk, or cache entry after contextual failure.",
          "Any coder edit to sealed chunk-03 RED tests."
        ],
        "seeding": [
          "Reach caller behavior through actual Engine tree and VM modes, real GoFunc builtins, json decode, compiler/cache paths, and REPL output; never invoke private publication helpers directly.",
          "Seed Terminal classes only with real evaluation context state as specified in chunk 01."
        ],
        "budgets": [
          "Synchronization batch 128; after observability at most 127 further units complete.",
          "Precedence: reduction `CodeResourceLimit` > `context.DeadlineExceeded` > `context.Canceled`.",
          "Structural default 1,024; exact behavior only while structural and work caps both hold."
        ],
        "names": [
          "Use the five exact contextual walk API names from chunk 03; callers holding Evaluator use unexported context-bearing helpers. `errors.Is` classifies cancellation/deadline and `errors.As` plus `Code == core.CodeResourceLimit` classifies reduction/work refusal."
        ],
        "refusals": [
          "The core walk refuses first; caller settlement preserves that Terminal and refuses all publication."
        ]
      },
      "redTasks": [
        "go-test-writer: before the chunk-04 coder runs, author and seal TestValueWalk_CallerPublication in runtime/value_walk_publication_test.go covering tree and VM modes: throw/assert/REPL/compiler/VM/stdlib/json — no result, charge, output, chunk, or cache entry after Terminal.",
        "go-test-writer: run TestValueWalk_TerminalClasses and TestValueWalk_TerminalPrecedence (sealed in chunk-03, core/depth_walk_budget_test.go) against migrated callers — run-only, never edit sealed files."
      ],
      "codeTasks": [
        "go-coder: after chunk-03 tests are sealed and chunk-03 APIs exist, migrate every active evaluator/compiler/VM/stdlib/json/runtime/REPL caller to the live context and handle `(result, error)` before charge or publication; retain all old context-free host signatures.",
        "go-coder: move throw/assert/REPL fallible rendering here, behind sealed tests; preserve assert's 200-rune message only after successful rendering.",
        "go-coder: synchronize before the hard work decision and at final exit so reduction > deadline > cancellation holds, Terminal remains uncatchable, and pending results/errors are cleared according to existing finishBuiltin/finishAdapter/FinishEval settlement.",
        "go-coder: preserve core's standard-library-only imports. Production code may not modify chunk-03 tests; contradictions require AMEND."
      ],
      "redTests": [
        "TestValueWalk_CallerPublication"
      ],
      "redRun": "go test -timeout 2m ./core/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... -run '^TestValueWalk_(CallerPublication|TerminalClasses|TerminalPrecedence)$'",
      "verify": "go test -timeout 2m ./core/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... -run '^TestValueWalk_(CallerPublication|TerminalClasses|TerminalPrecedence)$' && go list -deps ./core/... && go vet ./core/... ./core/compiler/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... && golangci-lint run ./core/... ./core/compiler/... ./plugins/stdlib/... ./plugins/json/... ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "chunk-05-inventory-retirement",
      "taskIds": [
        "3.1"
      ],
      "prev": "chunk-04-contextual-caller-publication",
      "sharedPkg": null,
      "parallel": true,
      "shard": "inventory",
      "seam": "chunk-05-inventory-retirement",
      "pkgDirs": [
        "internal/inventory",
        "plugins/stdlib"
      ],
      "pkgs": [
        "./internal/inventory",
        "./plugins/stdlib"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"result deep sizing\"]",
          "anchor": "PhaseLabel:  \"result deep sizing\"",
          "change": "Change Disposition from \"unbounded-tracked\" to \"budgeted\". Update Proof to reference the non-allocating work budget and the contextual walk entry point. Remove \"Owned by core-value-walk-sharing-bound.\" suffix."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"construction depth walk\"]",
          "anchor": "PhaseLabel:  \"construction depth walk\"",
          "change": "Change Disposition to \"budgeted\". Update Proof to reflect non-allocating work budget on constructionDepthExceeded."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"nested element depth walk\"]",
          "anchor": "PhaseLabel:  \"nested element depth walk\"",
          "change": "Change Disposition to \"budgeted\". Update Proof to reflect non-allocating work budget on checkDepthAt."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"value failure message format\"]",
          "anchor": "PhaseLabel:  \"value failure message format\"",
          "change": "Change Disposition to \"budgeted\" or \"bounded-exception\". The assert %.200v render now walks with a non-allocating work budget on boundedString. Update Proof."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"render assembly\"]",
          "anchor": "PhaseLabel:  \"render assembly\"",
          "change": "Change Disposition to \"bounded-exception\" with MaxWork: 67108864 bytes after successful contextual estimation and pre-charge. Delete the stale format-mismatched-verb-bound owner token from the Proof — that change is archived and complete. No ownership split to preserve."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"container render walk\"]",
          "anchor": "PhaseLabel:  \"container render walk\"",
          "change": "Change Disposition to \"budgeted\". boundedString now uses a non-allocating work budget with render reservation ceil(bytes/16). Update Proof."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"non-scalar render walk\"]",
          "anchor": "PhaseLabel:  \"non-scalar render walk\"",
          "change": "Change Disposition to \"budgeted\". The toAny v.String render now goes through a bounded walk with non-allocating work budget. Update Proof."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"default verb estimate\"]",
          "anchor": "PhaseLabel:  \"default verb estimate\"",
          "change": "Change Disposition to \"budgeted\". estimateFormatValueBytes calls the contextual ValueDeepBytes which is now bounded. Update Proof."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/work_data.go",
          "symbol": "WorkPhases[\"deep size walk\"]",
          "anchor": "PhaseLabel:  \"deep size walk\"",
          "change": "Change Disposition to \"budgeted\". formatStringBytes reaches the contextual ValueDeepBytes which is now bounded. Update Proof."
        },
        {
          "task": "3.1",
          "file": "internal/inventory/inventory.go",
          "symbol": "Dispositions",
          "anchor": "var Dispositions = []string{",
          "change": "If all unbounded-tracked rows are retired, remove \"unbounded-tracked\" from Dispositions list. Verify no stale format-mismatched-verb-bound owner tokens remain."
        },
        {
          "task": "3.1",
          "file": "plugins/stdlib/inventory_registration_test.go",
          "symbol": "invTrackedChanges",
          "anchor": "var invTrackedChanges = []string{",
          "change": "Remove \"core-value-walk-sharing-bound\" from invTrackedChanges if all its rows are retired."
        },
        {
          "task": "3.1",
          "file": "plugins/stdlib/inventory_source_test.go",
          "symbol": "TestInventorySource_UnboundedTrackedProof",
          "anchor": "proof: \"core.ValueDeepBytes walks the result as a tree. Owned by core-value-walk-sharing-bound.\"",
          "change": "Update or remove test rows that reference core-value-walk-sharing-bound as an unbounded-tracked owner. The row at line 1291 (tracking change named) and line 1296 (lookalike token) may need adjustment. Delete stale format-mismatched-verb-bound owner token from proof strings."
        }
      ],
      "contract": {
        "states": [
          "WorkPhase disposition, Proof, MaxWork, and owner tokens",
          "declared disposition set and tracked-change registry",
          "source reconciler opaque-call classification"
        ],
        "transitions": [
          {
            "input": "a scalable value walk now uses the contextual occurrence/work budget",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "set",
            "evidence": "Reclassify the current walk rows at `internal/inventory/work_data.go:483-520,812-835,955-969,1295-1319,1536-1588,1619-1646` as budgeted with the contextual callee proof."
          },
          {
            "input": "opaque fmt.Sprintf render assembly follows successful contextual estimation and pre-charge",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "forced",
            "evidence": "Set disposition `bounded-exception`, `MaxWork: 67108864` bytes, and proof that estimation and pre-charge succeeded before assembly (`design.md:43-47`)."
          },
          {
            "input": "the last unbounded-tracked row and stale owner token are removed",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "clear",
            "evidence": "Delete `unbounded-tracked`, its registry/branches/fixtures, and all stale owner tokens; keep every source-derived row represented."
          }
        ],
        "forbidden": [
          "Removing an inventory row instead of reclassifying its phase.",
          "A bounded-exception render-assembly row whose `MaxWork` is not exactly 67108864 bytes or whose proof omits successful contextual estimation and pre-charge.",
          "Any stale owner token or tracked-change-only branch/fixture after disposition retirement.",
          "Weakening source-derived phase coverage or bypassing opaque-call reconciliation."
        ],
        "seeding": [
          "Inventory state only through literal `internal/inventory.WorkPhases`; validations use the existing TestWorkInventory_* and TestInventorySource_* paths.",
          "Opaque-call ownership only through `invOpaqueQualified`; update it to the exact contextual APIs rather than bypassing the reconciler."
        ],
        "budgets": [
          "Budgeted walk phases use 4,194,304 default or evaluator-derived lower work units, batch 128, and maximum observable Terminal lag 127 units.",
          "Opaque render assembly is the sole specified bounded exception here with `MaxWork: 67108864` bytes."
        ],
        "names": [
          "Disposition `budgeted`; disposition `bounded-exception`; numeric `MaxWork: 67108864`; exact contextual callees from chunk 01."
        ],
        "refusals": [
          "Inventory registration/source guards reject every remaining `unbounded-tracked` declaration, stale owner token, missing row, or incorrectly bounded render assembly."
        ]
      },
      "redTasks": [
        "go-test-writer: before inventory production edits, seal the existing inventory guards to reject any `unbounded-tracked` declaration/row or stale owner token, require the exact contextual opaque callees, and require render assembly `bounded-exception` with `MaxWork: 67108864` and estimation/pre-charge proof.",
        "go-test-writer: retain source-derived phase coverage and synthetic reconciler fixtures so deleting a row cannot make the suite green."
      ],
      "codeTasks": [
        "go-coder: only after the chunk-05 RED is sealed, reclassify every former row, set the render-assembly bounded exception exactly, remove stale owner tokens, update `invOpaqueQualified`, and delete `unbounded-tracked` plus tracked-change-only branches/fixtures.",
        "go-coder: keep every registered builtin and source phase covered; do not edit sealed inventory tests."
      ],
      "redTests": [
        "Test(WorkInventory|InventorySource|ReconcileWork)"
      ],
      "redRun": "go test -timeout 2m ./plugins/stdlib/... -run 'Test(WorkInventory|InventorySource|ReconcileWork)'",
      "verify": "go test -timeout 2m ./plugins/stdlib/... ./internal/inventory/... -run 'Test(WorkInventory|InventorySource|ReconcileWork)' && golangci-lint run ./internal/inventory/... ./plugins/stdlib/...",
      "coder": "go-coder"
    },
    {
      "id": "chunk-06-charge-and-goldset-settlement",
      "taskIds": [
        "3.2"
      ],
      "prev": "chunk-05-inventory-retirement",
      "sharedPkg": null,
      "parallel": true,
      "shard": "goldset-settle",
      "seam": "chunk-06-charge-and-goldset-settlement",
      "pkgDirs": [
        "internal/goldset",
        "runtime"
      ],
      "pkgs": [
        "./internal/goldset",
        "./runtime"
      ],
      "sites": [
        {
          "task": "3.2",
          "file": "internal/goldset/testdata/text-render.lisp",
          "symbol": "text-render",
          "anchor": "; Data-dominated string building from an event map.",
          "change": "Re-measure: shared-structure cells will show different allocation charges after boundedDeepBytes uses a contextual work budget. Update .golden files with measured after values. The contextual cap produces capped partial results, not memoized unique-node proportional results."
        },
        {
          "task": "3.2",
          "file": "runtime/stdlib_result_ownership_test.go",
          "symbol": "TestResultOwnership_EveryInventoriedBranchHasAnArm",
          "anchor": "func TestResultOwnership_EveryInventoriedBranchHasAnArm",
          "change": "Verify allocation charges for stdlib results that use the contextual ValueDeepBytes entry point are consistent with the new bounded walks."
        },
        {
          "task": "3.2",
          "file": "runtime/meter_test.go",
          "symbol": "TestMeter_LeaseExhaustionIsTerminalResourceLimit",
          "anchor": "func TestMeter_LeaseExhaustionIsTerminalResourceLimit",
          "change": "Re-verify meter lease exhaustion behavior after ValueDeepBytes contextual entry point changes. Any gold-set cell that exercises shared structure will shift."
        }
      ],
      "contract": {
        "states": [
          "shared fixture contextual refusal and host capped partial",
          "allocation meter totals",
          "gold-set output golden",
          "gold-set ns/op, B/op and allocs/op evidence",
          "per-cell tier verdict"
        ],
        "transitions": [
          {
            "input": "the same 10-element base plus 26 returned Cons values is measured after the occurrence/work bound",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "set",
            "evidence": "Record `ValueDeepBytesContext`'s deterministic cap/refusal and context-free `ValueDeepBytes`'s deterministic capped partial result against the chunk-02 before artifact; do not infer a unique-node value (`proposal.md:15-22`; `design.md:59-65`)."
          },
          {
            "input": "ordinary gold-set fixture stays within both caps in evaluator or VM mode",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "no-op",
            "evidence": "Both modes continue to match independent goldens (`internal/goldset/goldset_test.go:10-31,35-58`; `internal/goldset/goldset.go:29-140`)."
          },
          {
            "input": "a deterministic charge expectation moves because a contextual estimator now refuses or returns a measured within-cap value",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "set",
            "evidence": "Update only after citing chunk-02 before and measured after; keep ledger movement separate from Go allocation/latency data."
          }
        ],
        "forbidden": [
          "Claiming a unique-node, sharing-aware, memoized, or exact over-cap ValueDeepBytes result.",
          "Inferring fewer charge cells from sharing rather than measuring contextual refusal/capped host behavior.",
          "Regenerating language goldens, changing fixture source, or silently rebaselining a deterministic charge.",
          "Using developer-machine timings to license or relax hosted performance tiers.",
          "Accepting a new ordinary-path allocation from traversal bookkeeping."
        ],
        "seeding": [
          "Gold set only through `internal/goldset.NewEngine`, `Fixtures`, and `CallFixture` (`internal/goldset/goldset.go:29-140`).",
          "Allocation movement only through WithEvalResourceLimits and EvalMeterFrom(ctx).Snapshot; no direct counter writes.",
          "Shared measurement only through NewList plus 26 returned Cons values, identical to chunk 02."
        ],
        "budgets": [
          "Benchmark parameters remain GOMAXPROCS=2 and benchtime=200ms in evaluator and VM modes.",
          "Hosted tier thresholds and the sole Goldset/guard-nil 4 B/op allowance remain unchanged.",
          "Walk evidence uses default 4,194,304 units or the explicitly recorded evaluator-derived lower cap; sync batch 128, observable lag at most 127 units."
        ],
        "names": [
          "Use concrete gold-set APIs `NewEngine`, `Fixtures`, and `CallFixture`; use the exact contextual and host walk names sealed in chunk 01."
        ],
        "refusals": [
          "Contextual over-cap measurement returns `CodeResourceLimit` and publishes no allocation charge; context-free measurement records only its capped partial value."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "go-coder: record the contextual refusal point and host capped partial result for the unchanged fixture, then update only deterministic ledger assertions that demonstrably move, each with before/after figures.",
        "go-coder: keep evaluator/VM output goldens unchanged for within-cap fixtures and attach paired performance evidence without changing hosted tier policy.",
        "go-coder: if ordinary B/op or allocs/op grows, remove the allocation from the walk path rather than adding a blanket allowance."
      ],
      "redTests": [],
      "redRun": "go test -timeout 2m ./internal/goldset/... -run '^TestGoldset$'",
      "verify": "go test -timeout 2m ./internal/goldset/... ./core/... ./plugins/stdlib/... ./plugins/json/... ./runtime/... -run 'Test(Goldset|ValueWalk|.*Meter.*|.*Charg.*)' && GOMAXPROCS=2 GOLDSET_MODE=eval go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem && GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem",
      "coder": "coder"
    },
    {
      "id": "chunk-07-repository-floor",
      "taskIds": [
        "3.3"
      ],
      "prev": "chunk-06-charge-and-goldset-settlement",
      "sharedPkg": null,
      "parallel": true,
      "shard": "floor",
      "seam": "chunk-07-repository-floor",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.3",
          "file": "core/depth_test.go",
          "symbol": "TestValueWalksBoundOverDeepValues",
          "anchor": "func TestValueWalksBoundOverDeepValues",
          "change": "Verify this existing depth-bounding test still passes after context threading."
        },
        {
          "task": "3.3",
          "file": "core/depth_with_test.go",
          "symbol": "TestCheckConstructionDepthWith_UsesEvaluator",
          "anchor": "func TestCheckConstructionDepthWith_UsesEvaluator",
          "change": "Verify existing evaluator-variant tests pass with new context parameter."
        },
        {
          "task": "3.3",
          "file": "core/metering_test.go",
          "symbol": "TestVectorLedgerBytesIndependentOfLayout",
          "anchor": "func TestVectorLedgerBytesIndependentOfLayout",
          "change": "Verify metering tests pass after ValueDeepBytes contextual entry point addition. ValueDeepBytes call at line 58 is the context-free host entry point; the contextual variant is tested separately."
        },
        {
          "task": "3.3",
          "file": "core/compiler/chunk_deepbytes_pinning_test.go",
          "symbol": "TestChunkDeepBytes_FlatVsSharedRepresentationIdentical",
          "anchor": "func TestChunkDeepBytes_FlatVsSharedRepresentationIdentical",
          "change": "Verify compiler chunk deep-bytes pinning test passes after ValueDeepBytes contextual entry point addition."
        },
        {
          "task": "3.3",
          "file": "plugins/stdlib/collection_build_budget_test.go",
          "symbol": "TestCollectionBuild_TerminalUnderLowReductions",
          "anchor": "func TestCollectionBuild_TerminalUnderLowReductions",
          "change": "Verify collection build budget tests pass after context threading through CheckConstructionDepthWith and CheckNestedElementDepthWith."
        },
        {
          "task": "3.3",
          "file": "plugins/stdlib/collection_build_budget_test.go",
          "symbol": "TestCollectionBuild_ExpiredDeadline",
          "anchor": "func TestCollectionBuild_ExpiredDeadline",
          "change": "Verify expired-deadline Terminal class is correctly observed after context threading."
        },
        {
          "task": "3.3",
          "file": "plugins/stdlib/collection_build_budget_test.go",
          "symbol": "TestCollectionBuild_Cancellation",
          "anchor": "func TestCollectionBuild_Cancellation",
          "change": "Verify cancellation Terminal class is correctly observed after context threading."
        },
        {
          "task": "3.3",
          "file": "plugins/stdlib/strings_budget_test.go",
          "symbol": "TestStrings_FormatEstimatorWalkIsUnboundedTracked",
          "anchor": "func TestStrings_FormatEstimatorWalkIsUnboundedTracked",
          "change": "Verify strings budget tests pass after walk changes."
        },
        {
          "task": "3.3",
          "file": "core/equals_bounded_test.go",
          "symbol": "TestEqualsBounded_StepsPerComparedNode",
          "anchor": "func TestEqualsBounded_StepsPerComparedNode",
          "change": "Verify equals-bounded charge-rate test passes after boundedEquals context threading."
        },
        {
          "task": "3.3",
          "file": "core/equals_bounded_test.go",
          "symbol": "TestEqualsBounded_MatchesEqualsAtDepthLimit",
          "anchor": "func TestEqualsBounded_MatchesEqualsAtDepthLimit",
          "change": "Verify equals-bounded depth-limit parity test passes after boundedEquals changes."
        }
      ],
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "Run every fullFloor command exactly as listed and retain the paired gold-set measurement output."
      ],
      "redTests": [],
      "redRun": "make lint",
      "verify": "make lint && make test",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "chunk-01-compatible-context-declarations",
      "tasks": [
        "1.1"
      ],
      "summary": "FIELD-FIRST: NO-RED-WAIVER: signature-only declarations needed before compiling RED; add five separately named contextual APIs so sealed tests compile, while every existing host signature and behavior remains unchanged. Do not route production callers or implement work/Terminal behavior here.",
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "go-coder: add exactly `func ValueStringContext(ctx context.Context, v Value) (string, error)`, `func ValueDeepBytesContext(ctx context.Context, v Value) (int64, error)`, `func ValueNodeCountContext(ctx context.Context, v Value) (int, error)`, `func CheckConstructionDepthContext(ctx context.Context, v Value, env *Env) error`, and `func CheckNestedElementDepthContext(ctx context.Context, v Value, env *Env) error` as behavior-neutral wrappers over current host behavior; their initial error result is nil.",
        "go-coder: preserve exactly `Value.String() string`, `Value.Equals(Value) bool`, `ValueDeepBytes(Value) int64`, `ValueNodeCount(Value) int`, `ValueDepthExceeds(Value, int) bool`, `CheckConstructionDepth(Value, *Env) error`, `CheckConstructionDepthWith(Value, Evaluator) error`, `CheckNestedElementDepth(Value, *Env) error`, `CheckNestedElementDepthWith(Value, Evaluator) error`, and `EqualsBounded(Value, Value, *BuiltinWorkBudget) (bool, error)`.",
        "go-coder: where later callers already hold an Evaluator rather than an Env, plan an unexported context-bearing helper; do not add another exported walk API. Do not thread compiler/VM/throw/assert/REPL callers or alter behavior in this chunk."
      ]
    },
    {
      "id": "chunk-02-before-measurement",
      "tasks": [
        "1.2"
      ],
      "summary": "NO-RED-WAIVER: sequential measurement-only baseline, captured before chunk 03; it changes no production behavior and supplies the comparison artifact required by task 3.2.",
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "Record the 26-cons shared fixture before values in a checked-in benchmark/result artifact: 10-element base, 26 `List.Cons` operations, structural depth 27, 1,040 charged bytes, ValueDeepBytes 24,159,191,024, String length 1,476,395,007 and 14.69s, construction-depth 1.677s; these current figures are recorded at `openspec/changes/core-value-walk-sharing-bound/proposal.md:15-22` and `internal/inventory/work_data.go:497-520`.",
        "Use the same constructor-only fixture for before and after; do not seed `List.flat`, `List.shared`, `listNode`, counters, or evalState fields directly."
      ]
    },
    {
      "id": "chunk-03-occurrence-work-bound",
      "tasks": [
        "2.1"
      ],
      "summary": "Sealed behavior RED followed by production implementation: first pin work caps, host degradation, render reservation, Equals, nested depth, Terminal classes/precedence/lag, caller publication, and parity; only then implement one non-allocating occurrence/work budget. Exact behavior is promised only inside both depth and work caps. Host degradation is ruled exactly: capped partials exclude the refused occurrence; host depth checks keep their depth-only answer; CodeResourceLimit is contextual-only.",
      "contract": {
        "states": [
          "logical occurrence count and hard work ceiling",
          "pending 128-unit synchronization batch and latched Terminal",
          "render-byte reservation",
          "exact, host-degraded, or Terminal-cleared result"
        ],
        "transitions": [
          {
            "input": "a core-owned walk visits one logical value occurrence",
            "state": "logical occurrence count and hard work ceiling",
            "effect": "set",
            "evidence": "The depth bound is not a work bound and shared references are visited once per occurrence (`specs/core-engine/spec.md:10-18`); no identity map is permitted (`design.md:19-25`)."
          },
          {
            "input": "a renderer knows a fragment byte length before append",
            "state": "render-byte reservation",
            "effect": "set",
            "evidence": "Reserve `(bytes + 15) / 16` with saturating overflow handling before append (`design.md:19-25`)."
          },
          {
            "input": "the next occurrence or render reservation would exceed max(1, effective MaxAllocationBytes/16)",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "Contextual APIs return `CodeResourceLimit`; unchanged host APIs use the sealed conservative degradation. The default is 67,108,864/16 = 4,194,304 units."
          },
          {
            "input": "a pending-batch check fires or a walk exits with a remainder",
            "state": "pending 128-unit synchronization batch and latched Terminal",
            "effect": "forced",
            "evidence": "Check budget/order at entry, synchronize before executing the unit that would make 128 since the last successful check, and assert 0–127 completed units after Terminal becomes observable. Charge reductions, then check absolute deadline, then cancellation through the existing budget order (`core/builtin_budget.go:25-64`)."
          },
          {
            "input": "a context-free host String or Equals walk reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "String returns its truncation marker and Equals returns false (`design.md:19-34`)."
          },
          {
            "input": "reduction exhaustion, expired absolute deadline, and cancellation are observable at one synchronization point",
            "state": "pending 128-unit synchronization batch and latched Terminal",
            "effect": "forced",
            "evidence": "Return reduction CodeResourceLimit before context.DeadlineExceeded before context.Canceled (`core/builtin_budget.go:49-64`; `design.md:27-34`)."
          },
          {
            "input": "a contextual walk returns Terminal while the caller holds a pending result, charge, output, chunk, cache entry, or non-Terminal error",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "clear",
            "evidence": "Terminal wins and nothing is published; required order is validate, contextual walk/estimate, charge, publish (`design.md:36-41`)."
          },
          {
            "input": "a context-free deep-byte or node-count walk reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "RULING: ValueDeepBytes and ValueNodeCount return the amount accrued at the capped point EXCLUDING the refused occurrence — a deterministic capped partial, never an error, never including the unit that would exceed. Exact expected values are computed from the sealed 19-Cons fixture (5,767,168 visits vs 4,194,304 cap) in the RED itself and frozen there."
          },
          {
            "input": "a context-free construction-depth or nested-element-depth check reaches its work ceiling",
            "state": "exact, host-degraded, or Terminal-cleared result",
            "effect": "forced",
            "evidence": "RULING: CheckConstructionDepth, CheckConstructionDepthWith, CheckNestedElementDepth, CheckNestedElementDepthWith, and ValueDepthExceeds return exactly their existing depth-only answer (nil error / existing boolean) when the structure is within MaxStructuralDepth — no CodeResourceLimit from host entry points; CodeResourceLimit is returned ONLY by the contextual APIs. Work-ceiling exhaustion inside a host depth walk stops the walk and yields the depth answer computed from what was visited."
          }
        ],
        "forbidden": [
          "Identity maps/tables, representation-aware identity, sharing markers, shared-node no-ops, memoized heights, reflection, unsafe keys, per-node atomics, or per-node clock reads.",
          "Allocating traversal bookkeeping on any path.",
          "Returning an exact contextual result after either cap is exceeded.",
          "Completing more than 127 further units after a Terminal condition becomes observable.",
          "Appending output before `ceil(renderedBytes/16)` reservation succeeds.",
          "Changing any context-free host signature named in chunk 01.",
          "Starting production behavior before every chunk-03 RED task is sealed.",
          "A production coder editing sealed RED instead of returning AMEND."
        ],
        "seeding": [
          "Contextual low-cap fixture: `base := core.NewList(tenScalarValues)` followed by exactly 5 returned-value `List.Cons(previous)` calls. It has `11 * 2^5 = 352` logical visits, deterministically exceeding the 256-unit ceiling derived from MaxAllocationBytes 4,096 without large output.",
          "Context-free default-cap fixture: the same 10-scalar base followed by exactly 19 returned-value `List.Cons(previous)` calls. It has `11 * 2^19 = 5,767,168` logical visits, deterministically exceeding the default 4,194,304-unit ceiling without the 26-cons rendering size.",
          "Reserve the 10-scalar plus 26-Cons fixture exclusively for chunk-02/chunk-06 measurement; never use it in RED or host String degradation tests.",
          "Ordinary/depth fixtures use only public NewList/NewVector/NewHashMap plus Set/Assoc and one-element nesting at depths 1,024/1,025; never mutate private backing fields.",
          "Canceled via context.WithCancel, deadline via an already expired core.WithEvalDeadline with a live parent, and reductions via the real eval state/budget; never fake Terminal errors.",
          "Caller publication only through actual tree/VM Engine, GoFunc, json, compiler/cache, and REPL surfaces; never direct private field writes.",
          "MID-WALK TERMINAL SEED (deterministic, constructor-only): reduction exhaustion is the mid-walk proof — seed a real evaluator via core.WithEvalResourceLimits with an allocation remainder that covers fewer than the fixture's 352 logical units but more than one 128-unit batch (e.g. remaining budget = 192 units' worth of bytes); the walk charges reductions at each sync point, so *core.LispicoError CodeResourceLimit fires at the second sync, mid-walk, deterministically. Cancellation and deadline reuse the same in-walk sync points: context.WithCancel cancelled after the first sync via the walk's own callback-free design is replaced by asserting (a) an entry-cancelled and entry-expired context still stops within 127 completed units of the first sync and (b) the reduction seed exercises the identical mid-loop check site, so the check provably sits inside the walk loop, not at entry/exit only. No sleeps, no wall-clock dependence."
        ],
        "budgets": [
          "Occurrence cost: 1 unit per logical visit.",
          "Render reservation: ceil(rendered bytes/16) before append; 15/16/17 bytes reserve 1/1/2 units.",
          "Ceiling: max(1, effective allocation bytes/16), default 4,194,304 units, low-cap RED 256 units.",
          "Batch size 128; observable Terminal lag at most 127 completed units.",
          "Mid-walk proof budget: reduction remainder 192 units (1.5 batches) on the 352-visit fixture; observable lag asserted <= 127 units."
        ],
        "names": [
          "`func ValueStringContext(ctx context.Context, v Value) (string, error)`",
          "`func ValueDeepBytesContext(ctx context.Context, v Value) (int64, error)`",
          "`func ValueNodeCountContext(ctx context.Context, v Value) (int, error)`",
          "`func CheckConstructionDepthContext(ctx context.Context, v Value, env *Env) error`",
          "`func CheckNestedElementDepthContext(ctx context.Context, v Value, env *Env) error`",
          "Existing `func EqualsBounded(a, b Value, budget *BuiltinWorkBudget) (bool, error)`.",
          "Tests: `TestValueWalk_WorkCap`, `TestValueWalk_HostDegradation`, `TestValueWalk_RenderReservation`, `TestValueWalk_TerminalClasses`, `TestValueWalk_TerminalPrecedence`, `TestValueWalk_CallerPublication`, `TestValueWalk_OrdinaryParity`, `TestValueWalk_DepthBoundary`."
        ],
        "refusals": [
          "Contextual walks refuse at the work/structural cap with the exact Terminal and no result; callers refuse publication; hosts expose only documented bounded degradation."
        ]
      },
      "redTasks": [
        "go-test-writer: after chunk-01 declarations and chunk-02 baseline, create and seal the exact walk-level `TestValueWalk_*` tests (WorkCap, HostDegradation, RenderReservation, TerminalClasses, TerminalPrecedence, OrdinaryParity, DepthBoundary — NOT CallerPublication, which chunk-04 owns); their failure must be an assertion against current behavior, never an undefined-symbol compile failure.",
        "go-test-writer: cover String, EqualsBounded, ValueDeepBytesContext, ValueNodeCountContext, CheckConstructionDepthContext, and CheckNestedElementDepthContext at the 256-unit cap with only the 10-scalar plus 5-Cons fixture (`11 * 2^5 = 352` logical visits).",
        "go-test-writer: cover unchanged host degradation directly for String, Equals, ValueDeepBytes, ValueNodeCount, ValueDepthExceeds, CheckConstructionDepth/With, and CheckNestedElementDepth/With using only the 10-scalar plus 19-Cons fixture (`11 * 2^19 = 5,767,168` visits > 4,194,304 default); never use the 26-Cons measurement fixture here.",
        "go-test-writer: cover render reservations at 15/16/17 bytes (1/1/2 units), proving reservation precedes append.",
        "go-test-writer: cover context.Canceled, context.DeadlineExceeded, and *core.LispicoError CodeResourceLimit, combined precedence reduction > deadline > cancellation, and no more than 127 completed units after observability.",
        "go-test-writer: seal ONLY the walk-level tests (WorkCap, HostDegradation, RenderReservation, TerminalClasses, TerminalPrecedence, OrdinaryParity, DepthBoundary) in core/depth_walk_budget_test.go; the CallerPublication tests are authored and sealed by chunk-04, never here."
      ],
      "codeTasks": [
        "go-coder: only after every chunk-03 RED assertion is sealed, implement the five exact contextual APIs without changing any old signature; keep EqualsBounded's signature and apply the same hard occurrence ceiling through its supplied budget. Callers holding Evaluator use an unexported context-bearing depth helper, not another exported API.",
        "go-coder: add a scalar, non-allocating walk budget: one unit per logical occurrence, default 4,194,304, evaluator-derived lower ceiling, 128-unit batching, final flush on every exit, and no identity or memoization state.",
        "go-coder: reserve ceil(renderedBytes/16) before every append with overflow-safe arithmetic; 15/16/17 bytes consume 1/1/2 units and failed reservation publishes no bytes.",
        "go-coder: preserve exact traversal within both caps and implement the sealed context-free truncation/false/capped-partial/conservative-depth degradation outside the work cap. Never edit sealed RED; return AMEND on contradiction."
      ]
    },
    {
      "id": "chunk-04-contextual-caller-publication",
      "tasks": [
        "2.2",
        "2.3"
      ],
      "summary": "Production caller migration after every behavior assertion is sealed in chunk 03. Active evaluator/compiler/VM/stdlib/json/REPL paths use the contextual APIs; old host signatures remain available and no fallible result is published before success.",
      "contract": {
        "states": [
          "live contextual walk or unchanged context-free host walk",
          "pending caller result/error/charge/output/chunk/cache",
          "Terminal identity",
          "published result"
        ],
        "transitions": [
          {
            "input": "active evaluation or compilation performs string, equality, deep-byte, node-count, construction-depth, or nested-depth work",
            "state": "live contextual walk or unchanged context-free host walk",
            "effect": "set",
            "evidence": "Evaluation and compilation must observe Terminal during the walk (`specs/core-engine/spec.md:14-26`); host APIs retain signatures (`design.md:27-34`)."
          },
          {
            "input": "reduction exhaustion is observable with deadline and/or cancellation",
            "state": "Terminal identity",
            "effect": "forced",
            "evidence": "Return the original `*core.LispicoError` with `CodeResourceLimit`; reduction precedes deadline and cancellation (`core/builtin_budget.go:49-64`)."
          },
          {
            "input": "deadline and cancellation are observable without reduction exhaustion",
            "state": "Terminal identity",
            "effect": "forced",
            "evidence": "Return `context.DeadlineExceeded` unchanged before `context.Canceled` (`core/builtin_budget.go:49-64`)."
          },
          {
            "input": "a contextual walk or final flush fails after validation but before publication",
            "state": "live contextual walk or unchanged context-free host walk",
            "effect": "clear",
            "evidence": "Terminal overrides pending domain/type errors and clears result; finishBuiltin/finishAdapter and FinishEval preserve Terminal identity (`plugins/stdlib/charges.go:11-16`; `runtime/eval.go:392-396,682-685,914-918`)."
          },
          {
            "input": "all contextual estimation/depth/size work and allocation charge succeed",
            "state": "published result",
            "effect": "set",
            "evidence": "Required order is validate -> contextual walk -> charge -> publish (`design.md:36-41`)."
          }
        ],
        "forbidden": [
          "Routing throw, assert, REPL, compiler, VM, stdlib, json, cache, or format output through a context-free walk while a live evaluation context exists.",
          "Changing or removing old context-free host signatures.",
          "Wrapping Terminal identities in a non-Terminal LispicoError or exposing them to Lisp try/catch.",
          "Publishing a result, allocation charge, formatted output, compiled chunk, or cache entry after contextual failure.",
          "Any coder edit to sealed chunk-03 RED tests."
        ],
        "seeding": [
          "Reach caller behavior through actual Engine tree and VM modes, real GoFunc builtins, json decode, compiler/cache paths, and REPL output; never invoke private publication helpers directly.",
          "Seed Terminal classes only with real evaluation context state as specified in chunk 01."
        ],
        "budgets": [
          "Synchronization batch 128; after observability at most 127 further units complete.",
          "Precedence: reduction `CodeResourceLimit` > `context.DeadlineExceeded` > `context.Canceled`.",
          "Structural default 1,024; exact behavior only while structural and work caps both hold."
        ],
        "names": [
          "Use the five exact contextual walk API names from chunk 03; callers holding Evaluator use unexported context-bearing helpers. `errors.Is` classifies cancellation/deadline and `errors.As` plus `Code == core.CodeResourceLimit` classifies reduction/work refusal."
        ],
        "refusals": [
          "The core walk refuses first; caller settlement preserves that Terminal and refuses all publication."
        ]
      },
      "redTasks": [
        "go-test-writer: before the chunk-04 coder runs, author and seal TestValueWalk_CallerPublication in runtime/value_walk_publication_test.go covering tree and VM modes: throw/assert/REPL/compiler/VM/stdlib/json — no result, charge, output, chunk, or cache entry after Terminal.",
        "go-test-writer: run TestValueWalk_TerminalClasses and TestValueWalk_TerminalPrecedence (sealed in chunk-03, core/depth_walk_budget_test.go) against migrated callers — run-only, never edit sealed files."
      ],
      "codeTasks": [
        "go-coder: after chunk-03 tests are sealed and chunk-03 APIs exist, migrate every active evaluator/compiler/VM/stdlib/json/runtime/REPL caller to the live context and handle `(result, error)` before charge or publication; retain all old context-free host signatures.",
        "go-coder: move throw/assert/REPL fallible rendering here, behind sealed tests; preserve assert's 200-rune message only after successful rendering.",
        "go-coder: synchronize before the hard work decision and at final exit so reduction > deadline > cancellation holds, Terminal remains uncatchable, and pending results/errors are cleared according to existing finishBuiltin/finishAdapter/FinishEval settlement.",
        "go-coder: preserve core's standard-library-only imports. Production code may not modify chunk-03 tests; contradictions require AMEND."
      ]
    },
    {
      "id": "chunk-05-inventory-retirement",
      "tasks": [
        "3.1"
      ],
      "summary": "Sequential inventory cutover after chunk 04: seal the inventory guard RED first, then classify every former unbounded-tracked phase as budgeted or as the numeric render-assembly bounded exception and delete the retired disposition machinery.",
      "contract": {
        "states": [
          "WorkPhase disposition, Proof, MaxWork, and owner tokens",
          "declared disposition set and tracked-change registry",
          "source reconciler opaque-call classification"
        ],
        "transitions": [
          {
            "input": "a scalable value walk now uses the contextual occurrence/work budget",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "set",
            "evidence": "Reclassify the current walk rows at `internal/inventory/work_data.go:483-520,812-835,955-969,1295-1319,1536-1588,1619-1646` as budgeted with the contextual callee proof."
          },
          {
            "input": "opaque fmt.Sprintf render assembly follows successful contextual estimation and pre-charge",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "forced",
            "evidence": "Set disposition `bounded-exception`, `MaxWork: 67108864` bytes, and proof that estimation and pre-charge succeeded before assembly (`design.md:43-47`)."
          },
          {
            "input": "the last unbounded-tracked row and stale owner token are removed",
            "state": "WorkPhase disposition, Proof, MaxWork, and owner tokens",
            "effect": "clear",
            "evidence": "Delete `unbounded-tracked`, its registry/branches/fixtures, and all stale owner tokens; keep every source-derived row represented."
          }
        ],
        "forbidden": [
          "Removing an inventory row instead of reclassifying its phase.",
          "A bounded-exception render-assembly row whose `MaxWork` is not exactly 67108864 bytes or whose proof omits successful contextual estimation and pre-charge.",
          "Any stale owner token or tracked-change-only branch/fixture after disposition retirement.",
          "Weakening source-derived phase coverage or bypassing opaque-call reconciliation."
        ],
        "seeding": [
          "Inventory state only through literal `internal/inventory.WorkPhases`; validations use the existing TestWorkInventory_* and TestInventorySource_* paths.",
          "Opaque-call ownership only through `invOpaqueQualified`; update it to the exact contextual APIs rather than bypassing the reconciler."
        ],
        "budgets": [
          "Budgeted walk phases use 4,194,304 default or evaluator-derived lower work units, batch 128, and maximum observable Terminal lag 127 units.",
          "Opaque render assembly is the sole specified bounded exception here with `MaxWork: 67108864` bytes."
        ],
        "names": [
          "Disposition `budgeted`; disposition `bounded-exception`; numeric `MaxWork: 67108864`; exact contextual callees from chunk 01."
        ],
        "refusals": [
          "Inventory registration/source guards reject every remaining `unbounded-tracked` declaration, stale owner token, missing row, or incorrectly bounded render assembly."
        ]
      },
      "redTasks": [
        "go-test-writer: before inventory production edits, seal the existing inventory guards to reject any `unbounded-tracked` declaration/row or stale owner token, require the exact contextual opaque callees, and require render assembly `bounded-exception` with `MaxWork: 67108864` and estimation/pre-charge proof.",
        "go-test-writer: retain source-derived phase coverage and synthetic reconciler fixtures so deleting a row cannot make the suite green."
      ],
      "codeTasks": [
        "go-coder: only after the chunk-05 RED is sealed, reclassify every former row, set the render-assembly bounded exception exactly, remove stale owner tokens, update `invOpaqueQualified`, and delete `unbounded-tracked` plus tracked-change-only branches/fixtures.",
        "go-coder: keep every registered builtin and source phase covered; do not edit sealed inventory tests."
      ]
    },
    {
      "id": "chunk-06-charge-and-goldset-settlement",
      "tasks": [
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: measurement and gold settlement only; compare the unchanged chunk-02 fixture benchmark already sealed earlier and gold-set cells, record contextual cap/refusal and host capped-partial behavior, and update only deterministic ledger expectations that actually move; keep Go benchmark evidence separate.",
      "contract": {
        "states": [
          "shared fixture contextual refusal and host capped partial",
          "allocation meter totals",
          "gold-set output golden",
          "gold-set ns/op, B/op and allocs/op evidence",
          "per-cell tier verdict"
        ],
        "transitions": [
          {
            "input": "the same 10-element base plus 26 returned Cons values is measured after the occurrence/work bound",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "set",
            "evidence": "Record `ValueDeepBytesContext`'s deterministic cap/refusal and context-free `ValueDeepBytes`'s deterministic capped partial result against the chunk-02 before artifact; do not infer a unique-node value (`proposal.md:15-22`; `design.md:59-65`)."
          },
          {
            "input": "ordinary gold-set fixture stays within both caps in evaluator or VM mode",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "no-op",
            "evidence": "Both modes continue to match independent goldens (`internal/goldset/goldset_test.go:10-31,35-58`; `internal/goldset/goldset.go:29-140`)."
          },
          {
            "input": "a deterministic charge expectation moves because a contextual estimator now refuses or returns a measured within-cap value",
            "state": "shared fixture contextual refusal and host capped partial",
            "effect": "set",
            "evidence": "Update only after citing chunk-02 before and measured after; keep ledger movement separate from Go allocation/latency data."
          }
        ],
        "forbidden": [
          "Claiming a unique-node, sharing-aware, memoized, or exact over-cap ValueDeepBytes result.",
          "Inferring fewer charge cells from sharing rather than measuring contextual refusal/capped host behavior.",
          "Regenerating language goldens, changing fixture source, or silently rebaselining a deterministic charge.",
          "Using developer-machine timings to license or relax hosted performance tiers.",
          "Accepting a new ordinary-path allocation from traversal bookkeeping."
        ],
        "seeding": [
          "Gold set only through `internal/goldset.NewEngine`, `Fixtures`, and `CallFixture` (`internal/goldset/goldset.go:29-140`).",
          "Allocation movement only through WithEvalResourceLimits and EvalMeterFrom(ctx).Snapshot; no direct counter writes.",
          "Shared measurement only through NewList plus 26 returned Cons values, identical to chunk 02."
        ],
        "budgets": [
          "Benchmark parameters remain GOMAXPROCS=2 and benchtime=200ms in evaluator and VM modes.",
          "Hosted tier thresholds and the sole Goldset/guard-nil 4 B/op allowance remain unchanged.",
          "Walk evidence uses default 4,194,304 units or the explicitly recorded evaluator-derived lower cap; sync batch 128, observable lag at most 127 units."
        ],
        "names": [
          "Use concrete gold-set APIs `NewEngine`, `Fixtures`, and `CallFixture`; use the exact contextual and host walk names sealed in chunk 01."
        ],
        "refusals": [
          "Contextual over-cap measurement returns `CodeResourceLimit` and publishes no allocation charge; context-free measurement records only its capped partial value."
        ]
      },
      "redTasks": [],
      "codeTasks": [
        "go-coder: record the contextual refusal point and host capped partial result for the unchanged fixture, then update only deterministic ledger assertions that demonstrably move, each with before/after figures.",
        "go-coder: keep evaluator/VM output goldens unchanged for within-cap fixtures and attach paired performance evidence without changing hosted tier policy.",
        "go-coder: if ordinary B/op or allocs/op grows, remove the allocation from the walk path rather than adding a blanket allowance."
      ]
    },
    {
      "id": "chunk-07-repository-floor",
      "tasks": [
        "3.3"
      ],
      "summary": "NO-TESTER-WAIVER: sequential command-only merge gate; it adds no new behavior and runs only after chunks 01-06 land.",
      "contract": {},
      "redTasks": [],
      "codeTasks": [
        "Run every fullFloor command exactly as listed and retain the paired gold-set measurement output."
      ]
    }
  ],
  "requirements": [
    {
      "shall": "`String`, `Equals`, `ValueDeepBytes`, and `ValueNodeCount` SHALL be depth-bounded",
      "tests": [
        "TestValueWalk_DepthBoundary",
        "TestValueWalksBoundOverDeepValues",
        "TestEqualsBounded_MatchesEqualsAtDepthLimit"
      ]
    },
    {
      "shall": "The depth bound alone SHALL NOT be treated as a bound on walk work. `core`",
      "tests": [
        "TestValueWalk_WorkCap",
        "TestStrings_ToStringWalkIsUnboundedTracked"
      ]
    },
    {
      "shall": "construction- and nested-element-depth checks, SHALL bound the work it performs",
      "tests": [
        "TestValueWalk_WorkCap",
        "TestValueWalk_RenderReservation"
      ]
    },
    {
      "shall": "Contextual walk entry points used by evaluation and compilation SHALL observe",
      "tests": [
        "TestValueWalk_TerminalClasses"
      ]
    },
    {
      "shall": "reduction budget while the walk runs, and SHALL stop with the corresponding",
      "tests": [
        "TestValueWalk_TerminalClasses",
        "TestValueWalk_TerminalPrecedence",
        "TestValueWalk_CallerPublication"
      ]
    },
    {
      "shall": "Terminal error. Existing context-free host entry points SHALL retain their",
      "tests": [
        "TestValueWalk_HostDegradation"
      ]
    },
    {
      "shall": "walk that checks only before it starts and after it finishes SHALL NOT establish",
      "tests": [
        "TestValueWalk_TerminalClasses",
        "TestValueWalk_TerminalPrecedence"
      ]
    },
    {
      "shall": "Values within both the structural-depth and work bounds SHALL be walked exactly",
      "tests": [
        "TestValueWalk_OrdinaryParity",
        "TestVectorLedgerBytesIndependentOfLayout"
      ]
    },
    {
      "shall": "- **THEN** it SHALL return a bounded result and SHALL NOT trigger a Go stack overflow",
      "tests": [
        "TestValueWalk_DepthBoundary"
      ]
    },
    {
      "shall": "- **THEN** every value-tree walk over it SHALL complete within a work bound stated in terms of its charged allocation, rather than growing with the number of references",
      "tests": [
        "TestValueWalk_WorkCap",
        "TestValueWalk_HostDegradation"
      ]
    },
    {
      "shall": "- **THEN** the walk SHALL stop at a bounded synchronization point and return the corresponding Terminal error rather than running to completion",
      "tests": [
        "TestValueWalk_TerminalClasses"
      ]
    }
  ],
  "testHarness": [
    "core/depth_test.go — TestSharedListDepthIsNotChainLength, TestValueWalksBoundOverDeepValues, TestCheckConstructionDepthWith_UsesEvaluator",
    "core/seq_property_test.go — TestConsMonotonic_ChargesPerNewNodeOnly, TestListFlatSharedEquivalence",
    "core/equals_bounded_test.go — TestEqualsBounded_StepsPerComparedNode, TestEqualsBounded_MatchesEqualsAtDepthLimit, TestEqualsBounded_HostValueNotStepped, TestEqualsBounded_ReturnsBudgetErrorUnchanged",
    "core/depth_with_test.go — TestCheckNestedElementDepthWith_UsesEvaluator",
    "core/metering_test.go — TestVectorLedgerBytesIndependentOfLayout",
    "core/compiler/chunk_deepbytes_pinning_test.go — TestChunkDeepBytes_IndependentOfListRepresentation",
    "plugins/stdlib/strings_budget_test.go — sbWorkPhases(), TestStrings_ToStringWalkIsUnboundedTracked, TestStrings_FormatEstimatorWalkIsUnboundedTracked, TestStrings_ToAnyRenderIsUnboundedTracked",
    "plugins/stdlib/inventory_registration_test.go — invTrackedChanges, invNamesTrackedChange()",
    "plugins/stdlib/inventory_source_test.go — TestInventorySource_UnboundedTrackedProof",
    "plugins/stdlib/collection_build_budget_test.go — TestCollectionBuild_TerminalUnderLowReductions, TestCollectionBuild_ExpiredDeadline, TestCollectionBuild_Cancellation, TestCollectionBuild_ValuesAndErrorsUnchanged",
    "plugins/stdlib/numeric_budget_test.go — TestEquals_DeepComparisonIsInterruptible, TestEquals_HostValueIsTrustedBoundary",
    "plugins/stdlib/lookup_budget_test.go — lbSharedList(), TestLast_TerminalOnSharedListUnderLowReductions",
    "plugins/json/json_test.go — TestJSONEncodeDecode_RoundTripsAllocation (parity at default cap)",
    "runtime/stdlib_result_ownership_test.go — TestResultOwnership_EveryInventoriedBranchHasAnArm",
    "runtime/meter_test.go — TestMeter_LeaseExhaustionIsTerminalResourceLimit",
    "core/vm/const_charged_test.go — TestConstCharged_SharingReturnsSameHashMap"
  ],
  "floor": "make lint && make test && go test -race -count=1 -timeout 10m ./core/... ./plugins/... ./runtime/... && go vet ./... && make profile && GOMAXPROCS=2 GOLDSET_MODE=eval go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem && GOMAXPROCS=2 GOLDSET_MODE=vm go test -timeout 10m ./internal/goldset/ -run '^$' -bench . -benchtime=200ms -benchmem",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 4
  }
}
```
