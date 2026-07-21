## Why

yagel ADR 0105 charges 10 million reductions and 64 MiB cumulative allocation
per Rule load/dispatch with context checks within 1,024 reductions, and states
"Reader, macro, compiler, evaluator, pure plugins and every dynamic
constructor/growth path charge before work; missing instrumentation is engine
incompatibility." go-lispico currently bounds structural depth, collection
length, and cache entries (ADR 0007) but not eval-step count or cumulative
allocation, so a tight allocation loop or macro-amplified work cycle exhausts
the host without tripping any ceiling. yagel's own pending change
`meter-lisp-evaluation` is blocked on exactly this list (its design.md names
reductions and cumulative allocation as absent from the pinned engine).

This change adds per-evaluation reduction and allocation metering on top of
the existing `ResourceLimits` struct. It charges the per-eval ledger only;
cross-scope composition (leases, embedder meters) is
`meter-leases-and-session-ledgers`, built on the charge sites defined here.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxReductions int` and
  `MaxAllocationBytes int` (zero → defaults `10_000_000` reductions,
  `64 * 1024 * 1024` bytes per evaluation, matching yagel ADR 0105; never
  unlimited, consistent with the existing ADR 0007 fail-closed defaults).
- Extend the per-call `evalState` with reduction and allocation counters
  (same context-threading as `structDepth`, ADR 0003/0007). The VM keeps
  frame-local counters and flushes to the shared state at its existing sync
  points, so tree-walker fallback and GoFunc re-entry charge one ledger.
- Reduction counting piggybacks the existing 128-step batched-cancellation
  countdown in both evaluators: consumed budget is flushed to the reduction
  counter at each poll boundary and at evaluation end. No new per-step work,
  no new clock reads, no new check-interval constant — the existing 128-step
  interval already satisfies the ≤1,024-reduction context-observation
  contract. Ceiling checks happen at poll boundaries (precision ±128,
  documented).
- Reduction definition is per-evaluator: tree-walker charges one per
  apply-trampoline iteration and one per form dispatch; the VM charges one
  per instruction decode; macro expansion charges one per expansion step;
  the compiler charges one per emitted instruction; GoFunc dispatch charges
  one at each of the two centralized apply sites (`core/eval.go` `apply`,
  `core/vm/vm.go` `apply`) — no per-plugin edits. Counter values are NOT
  comparable across evaluators; parity is same-ceiling, same-terminal-error
  behavior.
- Allocation charging uses a fixed, documented, architecture-independent
  per-type size table (deterministic ledgers; `unsafe.Sizeof` varies by
  platform) at these normative charge sites:
  - VM `OpMakeList` / `OpMakeVector` / `OpMakeMap` / `OpClosure`;
  - tree-walker collection-literal and quasiquote construction;
  - compiler emit — chunk code and constants charge the evaluation that
    triggered compilation, before the chunk is cached (yagel core-runtime
    "compilation allocation first charges the current evaluation");
  - GoFunc dispatch return — the result's shallow size (string byte length,
    collection header + element slots) charged at the two apply sites,
    covering every plugin without per-plugin edits;
  - reader output — the reader counts nodes and approximate bytes while
    parsing (it already tracks depth, no context needed); the engine bridges
    the totals into the evaluation ledger immediately after `Read`, before
    the first form evaluates. This replaces the earlier reader exemption,
    which contradicted ADR 0105.
- Exceeding either ceiling raises `*core.LispicoError{Code:
  CodeResourceLimit}` — terminal and non-catchable via
  `eval-noncatchable-terminal-errors`, which this change depends on.
- Counters are per-evaluation (per-call `evalState`), never engine-shared
  (preserves ADR 0003).
- Introduces ADR 0011 (charging model + size table).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new `ResourceLimits` fields + new requirement `Evaluation
  reductions and cumulative allocation are metered`.
- `core-engine`: new requirement `Per-evaluation reduction and allocation
  counters`.

## Impact

- Depends on: `eval-noncatchable-terminal-errors` (terminal classifier).
- Code: `runtime/{engine,eval}.go`, `core/{eval,env,reader,error}.go`,
  `core/compiler/compiler.go`, `core/vm/vm.go`. No plugin edits — GoFunc
  charging is centralized at the two apply sites.
- Defaults chosen so the existing suite and goldset stay green; goldset gate
  (`GOLDSET_MODE=vm`) must stay non-increasing — the piggyback design exists
  precisely to keep the hot loop unchanged.
- yagel: unblocks `meter-lisp-evaluation`'s reduction/allocation half; its
  dispatch path (`Evaluator.Apply`, not `Engine.Call`) is covered because
  counters live in `evalState`, not in runtime entry wrappers.
