## Why

The consumer Rule benchmark — the yagel shape: a routing function returning
config literals — spends most of its allocation rebuilding values that never
change. `(defn route [task] (cond ... {:model :large :tools [:read :grep]}
... {:model :small}))` reconstructs its result map, its nested vector, and
every intermediate on **every call**, although each is a literal whose
elements are all compile-time constants.

Measured on the article harness at dcbdf62 (Rule 856ns / 480 B / 13 allocs
per call; GopherLua 711ns, goja 877ns):

- `core.(*HashMap).Set` 40.9% + `core.NewHashMap` 6.3% of allocated bytes —
  the result maps, rebuilt per call, with the compact form copying per entry.
- `vm.go:969` (`items := make([]core.Value, n)`) + `vm.go:979` (`vm.push(res)`)
  another 21.7% — the `[:read :grep]` vector and friends.
- `constructionDepthExceeded` 7.9% of CPU — depth-walking values whose depth
  was knowable at compile time.

Roughly two thirds of Rule's engine-side allocation is construction of
constants.

The compiler has no folding of all-constant collection literals: `Compile`'s
`core.Vector`/`*core.HashMap` cases (compiler.go:157-182) emit element pushes
plus `OpMakeVector`/`OpMakeMap` unconditionally. The repo already contains the
safe precedent in miniature: the quasiquote `*core.HashMap` path
(compiler.go:1132-1139) freezes the whole map into the chunk constant pool as
a single `OpConst`, shared across every execution, relying on the same value
immutability. Clojure's compiler does exactly this for every all-constant map,
vector, and set literal (`MapExpr.parse` returns a `ConstantExpr`), and it is
sound there for the same reason it is sound here: persistent values have value
semantics, so sharing one instance across calls is unobservable from the
language. Lispico values carry no per-occurrence metadata (no `Meta` field on
any of the 13 types), so Clojure's one caveat does not even apply.

Expected effect: Rule loses the construction column — on the measured split
that is on the order of −300 B and −5 allocs per call and −20% latency,
putting Rule ahead of GopherLua's 711ns. Every yagel rule that returns literal
config benefits identically; this is the single highest-leverage change for
the rule-latency goal.

## What Changes

- The compiler folds a `Vector`, `HashMap`, or list literal whose elements
  (recursively) are all compile-time constants into a single prebuilt value in
  the chunk constant pool, emitted as `OpConst` — no element pushes, no
  `OpMake*`, no per-call construction. Nested all-constant literals fold into
  their parent.
- Ledger parity is preserved by precomputation, not skipped: the folded
  constant's deep bytes and structural depth are computed once at compile
  time and stored with the constant reference; executing the fold charges the
  per-evaluation allocation ledger by the precomputed amount and checks the
  precomputed depth against the engine's `MaxStructuralDepth` in O(1). A
  program that trips `MaxAllocationBytes` or `MaxStructuralDepth` under the
  tree-walker still trips it identically under the VM — the charge model is
  unchanged, only the walk and the rebuild are gone.
- The chunk's retained-bytes accounting already covers constants
  (`chunkDeepBytes` → `ChargeRetained` at cache admission, per ADR 0011's
  compile-time charge site); folded constants ride that existing path.
- Literals containing any non-constant element (a symbol, a nested call)
  compile exactly as today.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: adds a requirement that all-constant collection literals
  compile to shared chunk constants, with ledger and depth enforcement
  preserved via precomputed charges, and cross-evaluator result parity
  unchanged.

## Impact

- Code: `core/compiler/compiler.go` (literal cases + a constant-detection
  helper), `core/vm` (a charge-carrying constant emission — either an operand
  table or a fused charge opcode), tests, goldset.
- Risk — observable sharing: repeated evaluations now return the *same* Go
  value (pointer-identical) where they previously returned equal fresh values.
  In-language this is unobservable (immutability, `Equals`-based comparison);
  a Go embedder comparing pointers could notice. The quasiquote-HashMap path
  has already shipped this behavior without issue; documented in the design
  doc rather than left implicit.
- Risk — metering divergence between evaluators if the charge were skipped
  instead of precomputed: avoided by design (see Ledger parity above). The
  `vm-fused-native-ops` charge precedent (charging even unmetered to keep
  parity) is followed.
- Cross-val: `TestVMVsTreeWalker` compares results by value; folding changes
  no result. Goldset: VM cells improve; tree-walker cells untouched;
  one-sided perfgate accepts improvement.
