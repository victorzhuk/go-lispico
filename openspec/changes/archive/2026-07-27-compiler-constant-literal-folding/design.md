## Context

Rule-shaped consumer code returns literal config from compiled functions. The
VM rebuilds those literals per call. Everything needed to stop that already
exists: immutable values, a chunk constant pool deduped by `Equals`
(chunk.go:205-213), a compile-time retained-charge path (`chunkDeepBytes`,
compiler.go:826-842), and a shipped precedent (quasiquote HashMap →
`OpConst`, compiler.go:1132-1139).

## Decision: fold at compile, charge precomputed at execution

A literal folds when every element is a compile-time constant: `Nil`, `Bool`,
`Int`, `Float`, `String`, `Keyword`, or a collection whose elements all fold.
`Symbol` never folds outside `quote` (it resolves). The fold builds the value
once with the same constructors the runtime path uses, registers it via
`AddConstant` (dedup by `Equals` collapses repeated literals across the
chunk), and emits a constant reference.

### Why execution still charges the ledger

ADR 0011 separates per-evaluation allocation charges (VM `OpMake*` sites) from
one-time compile charges (constant pool → retained meter). A folded literal
genuinely stops allocating per call, so skipping the per-eval charge would be
"honest" — but it would make the allocation ledger evaluator-dependent: a
program near `MaxAllocationBytes` would pass under the VM and fail under the
tree-walker. `vm-fused-native-ops` faced the same choice and charged for
parity; follow it. The charge is one integer add against a precomputed
amount — nanoseconds — while the win (no map/vector rebuild, no depth walk,
no GC pressure) is preserved intact.

The same parity argument cuts the other way for `quote`: the tree-walker's
`evalQuote` returns the datum with no construction charge or depth check, and
the VM already compiled quoted collections to a plain `OpConst`. Quoted
literals therefore keep the plain `OpConst` emission — folding them to a
charged constant would charge the VM where the tree-walker charges nothing.
Quasiquote is different: the tree-walker constructs (and charges for)
quasiquoted collections, so all-constant quasiquote templates fold to the
charged constant like plain literal syntax.

Mechanism: constants gain an optional side table `constCharges[constIdx] →
constructionBytes` populated only for folded collection literals. Emission
uses a distinct opcode (`OpConstCharged` or an equivalent fused form) so the
plain `OpConst` hot path pays nothing new. `Validate()` must cover the new
opcode's constant index and side-table presence — the validation-completeness
lesson from `vm-dispatch-loop-tightening` applies verbatim.

The precomputed charge is the construction-skeleton bytes — the recursive sum
of `core.ListShallowBytes`/`core.VectorShallowBytes`/`core.HashMapShallowBytes`
over the literal's containers, scalar leaves uncharged — because that is what
the per-execution construction path charges on both evaluators
(`chargeAllocBytes(core.VectorShallowBytes(n))` etc. in the VM,
`st.chargeAllocBytes(VectorShallowBytes(n))` in the tree-walker). Charging
`ValueDeepBytes` instead would add scalar leaf bytes the construction path
never charges, breaking ledger parity for programs near `MaxAllocationBytes`.

### Depth stays enforced, O(1)

`checkConstructionDepth` today walks the built value per call. The folded
literal's depth is computed once at compile and carried by an
`OpStructEnter(d)`/`OpStructLeave(d)` wrap around the charged-constant
instruction — the shipped quasiquote-HashMap precedent
(compiler.go:1132-1139). The existing `OpStructEnter` gate adds `d` to the
running VM's cumulative structural-depth counter and compares against the
running engine's `maxStructuralDepth`, raising the same terminal
`ResourceLimitError`; this stays correct when a folded literal nests inside
non-folded construction, where an own-depth comparison would diverge.
Enforcing at execution (not at compile) keeps the chunk correct even if it is
ever shared across engines with different limits — the process-level chunk
tier (`stdlib-startup-cache`, and `engine-startup-template-sharing` if it
lands) already exists, so bake no engine config into the chunk.

## Alternatives considered

- **Skip the per-eval charge entirely** — rejected: evaluator-dependent
  ledger, see above.
- **Fold in the reader** — rejected: the reader has no notion of evaluation
  context; `[a b]` folds or not depending on whether `a` resolves. Folding is
  a compiler concern.
- **Intern across chunks (Clojure's compiler-wide CONSTANTS pool)** —
  deferred: per-chunk `AddConstant` dedup already collapses within a chunk;
  cross-chunk interning adds a process-level structure with lifetime/bounds
  questions for marginal extra sharing. Revisit only with profile evidence.
- **Extend to `OpMakeList` call sites in quasiquote** — the quasiquote
  List/Vector paths (compiler.go:1090-1131) run elements to support nested
  `unquote`; an all-constant quasiquote template with no unquotes reduces to
  the plain-literal case after expansion. No special handling; if the plain
  path sees it, it folds.

## Observable-behavior note

Pointer identity of results changes: two evaluations of a folded literal
return the same Go value. In-language semantics are unchanged (immutable
values, `Equals` comparison, no identity primitive over collections in the
stdlib). Go embedders comparing `Value` pointers could observe it; the
quasiquote-HashMap path shipped identical behavior. Recorded here so a future
"why is this the same pointer" question lands on a decision, not an accident.

## Measurement plan

Baseline and candidate captured in ONE interleaved benchstat session
(≥10 counts) per the perfgate memo. Cells: article-harness Rule/Callback/Call
rows, goldset both modes, plus a dedicated micro: a compiled function
returning a two-entry map with a nested vector (the yagel shape), asserting
B/op drops to boundary-only cost.
