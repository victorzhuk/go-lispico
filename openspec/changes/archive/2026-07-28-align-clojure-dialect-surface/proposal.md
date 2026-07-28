## Why

The kernel surface diverges from Clojure on five points, and every dialect
inherits the divergence because dialects are deltas over the kernel. YAGEL
ships `.clj` rules written against documented Clojure semantics; each
divergence is a silent wrong answer or a forced workaround in rule source:

- `let` binds in parallel — a later init cannot see an earlier sibling, so
  `(let [x 1 y x] y)` resolves `y` against the enclosing scope instead of `1`.
- `unless` exists as a kernel special form; Clojure has no `unless`.
- A bare `else` symbol (and even the `:else` text read as a symbol) marks a
  `cond` else clause; Clojure `cond` recognizes the `:else` keyword only.
- `string/join` takes `(coll sep)`; Clojure takes `(sep coll)`.
- `catch` binds the thrown value stringified via `%v`, so a structured throw
  like `{:code :denied}` arrives as an opaque string; Clojure binds the value.

## What changes

- `let` binds sequentially on both execution paths: each init is evaluated
  against the child scope that already holds the prior bindings (what `let*`
  does today). `let*` stays registered as the same semantics for backward
  compatibility.
- `unless` leaves the kernel table, the tree-walker, and the compiler. Under
  every dialect `(unless ...)` resolves as an ordinary call and fails as an
  unresolved symbol; embedders negate with `(when (not ...) ...)` or a macro.
- `cond` else detection recognizes only the `:else` keyword, in the
  tree-walker (`isCondElse`) and the compiler (`isElse`). A bare `else`
  symbol becomes an ordinary test expression.
- `string/join` takes the separator first: `(string/join sep coll)`.
- `catch` binds the originally thrown value on both execution paths. Only
  errors that did not come from `throw` (engine errors returned by
  primitives) bind their message string, as before.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: the kernel table's conditional/binding/error surface aligns to
  Clojure — sequential `let`, no `unless`, `:else`-keyword-only `cond` else,
  structured `catch` binding; the truthiness requirement no longer lists
  `unless` among conditional forms.
- `bytecode-vm`: compiled `let` registers each local before compiling the
  next init (sequential, matching the tree-walker); `OpThrow` delivers the
  raw thrown value to the handler instead of a `%v`-stringified one; the
  compiler drops its `unless` case.

## Impact

- Breaking for embedders relying on parallel `let`, the `unless` form, a
  bare-`else` `cond` marker, `(string/join coll sep)`, or stringified catch
  bindings. Released as v0.10.0 (pre-1.0 minor for breaking surface changes).
- The Common Lisp dialect loses `unless` with the kernel; no CL adapter is
  added — negated conditionals use `(when (not ...) ...)` or a user macro.
- Invariants preserved: Evaluator/VM result agreement (cross-validation
  corpus extended), quoted data never rewritten, gold-set fixture
  `guard-nil` keeps its `[nil nil]` golden on kernel forms only.
- Out of scope: removing `let*` (kept as an alias of the new `let`
  semantics); any stdlib change beyond `string/join`; reader changes.
