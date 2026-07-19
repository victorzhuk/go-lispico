# Design — dialect-aware native-op emission

## Context

`compileList` emitted native opcodes (`OpAdd`, …) only inside `if isSpecial`, and
under a configured dialect `isSpecial = CanonicalName(op).ok` — false for
`+ - * / < > <= >= =`. So the shipped runtime path (`NewCompilerWithDialect`)
never emitted native opcodes; every operator ran as a `GoFunc`. Hoisting the
native-op gate out of `if isSpecial` fixes this for Lisp-1 (clojure, the
goldset/bench dialect).

But the proposal's rebind-safety argument — "the VM freezes canonical
eligibility at `OpGetGlobal`, so a rebind is safe" — silently assumes head
resolution *is* the value cell. That holds only for Lisp-1. Under the **default
CL dialect (Lisp-2)** a symbol's callable identity lives in a separate
**function cell**; head position resolves through it (`OpGetFunc`), and `defun`
rebinds only that cell. `compileNativeOp` emitted `OpGetGlobal` (value cell), so
the native fast path read the wrong cell.

Verified divergence (before this design's fix): under CL, after
`(defun + (a b) (- a b))`, `(+ 5 3)` gives tree-walker `2` (rebound function
cell) but VM `8` (native `OpAdd` over the still-canonical value cell) — a
tree-walker/VM parity break in the default dialect, violating the accepted
"Rebound operator falls back … matching tree-walker" scenario.

## Decisions

### 1. Lisp-2 native head resolves through the function cell

Under a Lisp-2 dialect, `compileNativeOp` emits `OpGetFunc` for the operator
head (mirroring `compileCall`'s Lisp-2 branch) instead of `OpGetGlobal`.
Arguments stay value-namespace (`OpGetGlobal`/`OpGetLocal`), matching Lisp-2:
head in the function namespace, args in the value namespace. Lisp-1 (and the
nil-dialect compiler) keep `OpGetGlobal`.

### 2. The function cell carries a canonical flag

`Env.funcs` stored a bare `Value` with no canonical marker, so the VM had no way
to tell a canonical stdlib operator from a `defun`-rebound one in head position.
Add function-cell canonical tracking, mirroring the value cell:

- `SetCanonicalFunc(name, val)` — binds the function cell and marks it canonical.
- `GetFuncCanonical(name) (Value, found, canonical)` — resolves like `GetFunc`
  and reports whether the owning scope's binding is canonical.
- Plain `SetFunc(name, val)` — the rebind path (`OpSetFunc`, tree-walker
  `defun`, `MergeInto`) — clears the canonical marker, so any rebind is detected
  as non-canonical. This mirrors `Set` clearing the value-cell marker.

### 3. The bridge marks canonical operators canonical in the function cell

`applyVocabulary`'s Lisp-2 bridge copied every value-cell `GoFunc` into the
function cell via `SetFunc`. Change it to consult the value cell's canonical
flag (`GetCanonical`) and bridge canonical bindings via `SetCanonicalFunc`,
non-canonical ones via `SetFunc`. The bridge re-runs on every `Use`, so it
covers stdlib's operators (registered via `SetCanonical`) once stdlib loads.

Embedder bindings via `Engine.Bind` / `EvalWithBindings` stay `SetFunc`
(non-canonical) — a custom `+` must not take the native path, exactly the
existing `bindBuiltin` benchmark case.

### 4. The VM freezes native ops resolved through the function cell

`OpGetFunc` gains the same freeze the value-cell `OpGetGlobal` path has: after
pushing the resolved head, if the symbol is a native operator and the
function-cell binding is canonical, `freezeNativeOp` records the opcode for that
stack slot. A rebound (non-canonical) function cell is not frozen, so
`dispatchNativeOp` falls back to `vm.call` over the pushed value — the rebound
function — matching the tree-walker.

The freeze slot mechanism (per-slot `nativeOp`, cleared after dispatch) is
unchanged; only the resolution source (function cell) differs. `OpGetFunc` is
not site-cached (unlike `OpGetGlobal`); `GetFuncCanonical` walks the scope chain
per resolution — allocation-free map reads, and the win (`execNativeFast` vs a
`GoFunc` call frame) does not depend on the cache.

### 5. What is *not* changed

- Lisp-1 (clojure/nil) path is untouched: value-cell `OpGetGlobal` freeze,
  already proven parity-safe for `def`/`defn` rebinds.
- No new opcode: `OpGetFunc` already exists and is emitted for Lisp-2 calls;
  the change adds the freeze branch to it.
- `MergeInto` copies function cells via `SetFunc` (canonical lost on merge),
  consistent with its existing value-cell `Set` behavior — not on the goldset
  or native test paths.

## Verification

- Parity: new crossval — under CL, `(defun + (a b) (- a b))` then `(+ 5 3)`
  gives identical tree-walker and VM results (`2`); the same for `-`, `<`.
  Existing `core/vm` crossval + full `runtime` suite stay green.
- Native path live under CL: a `WithBytecode()+cl+stdlib` engine's `(+ a b)`
  executes via `execNativeFast` (alloc-drop / no `GoFunc` dispatch), same proof
  shape as the clojure runtime test.
- Compiler: under CL, `(+ a b)` emits `OpGetFunc … OpAdd`; under clojure,
  `OpGetGlobal … OpAdd`; a locally-shadowed or `defun`-rebound operator emits
  the ordinary call path / falls back.
- `-race`: concurrent `Call` on one CL engine (canonical-func reads under the
  env lock).
- Goldset (clojure) unaffected; CL arithmetic now takes the native path.
