## Decisions

- **Truthiness:** delete rather than deprecate. The project is alpha, the axis is gone by ADR amendment, and yagel and zhk do not call `TruthyFunc`.
- **Compiler dialect by value:** `NewCompilerWithDialect(name string, d core.Dialect)`. `core.Dialect{}` is the identity dialect (full kernel, no delta), as `dialect-declarative-spec` defines the zero value.
- **IsCallable:** `core.IsCallable(v)` returns true for `GoFunc`, `Lambda`, `Keyword` and any value implementing a small exported marker interface in core that `*vm.Closure` implements. Keyword stays callable to keep `cl` adapter behavior unchanged.
- **cacheKey:** remove the field; stripe routing already ignores it.
