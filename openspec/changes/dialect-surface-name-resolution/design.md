## Context

- VM: `compileList` → `CanonicalName(head)`; `removed` → `CompileError "undefined form"` (`core/compiler/compiler.go:241-244`), not `CodeUnsupported`, so `runtime/eval.go:596-599` never falls back.
- Tree-walker: `e.forms[sym]` from `resolve()`; a missing name is evaluated as an ordinary call.
- `expandDeepList` also refuses removed heads (`core/eval.go:1114-1117`).
- `NormalizeCond` (`core/dialect.go:565-617`) builds a new `List` per clause, and wraps multi-expression bodies in `Symbol{visibleName("do")}`.

## Decisions

### Resolved table is the only dispatch source

`CanonicalName` is reimplemented over the resolved table plus a reverse map (visible → canonical) computed once in `NewDialect` and stored in the frozen state. "Removed" then means exactly "not in the table", identically for the compiler, expansion and the tree-walker.

### cond clauses as structured values

`NormalizeCond` returns `[]CondClause{Test Value; Body []Value}` instead of Lisp lists. The tree-walker evaluates `Body` as an implicit progn; the compiler compiles it as a `do` sequence using its kernel emitter. No symbol is synthesized, so the surface name of `do` stops mattering, and the tree-walker stops allocating clause lists. The existing "one normalizer for both paths" requirement is kept.

### Typed compiler errors

Use the same constructors the tree-walker uses for `function`/`funcall` shape errors.

## Risks

- The VM starts accepting programs it used to reject; no program that compiled before changes meaning.
