## 0. Predecessor and scope

- [x] 0.1 Verify `vm-local-stack-scope` has been implemented and archived before any implementation here; read its settled compiler scope/frame contract and record the passing regression evidence.
- [x] 0.2 Confirm existing-service-strict coverage of `def`, Lisp-1/Lisp-2 `defn`, empty scopes, whole-form fallback and once-only evaluation; verify existing `Eval` and `EvalCached` fallback branches handle `compiler.CodeUnsupported` before running a chunk.

## 1. Regressions before implementation

- [x] 1.1 Add new compiler test `TestCompilerScopedDefinitionFallback` for function, let/let*, loop and catch scopes, including empty binding lists and the Lisp-2 `compileDefn` branch; verify current scoped definitions compile incorrectly while genuine top-level definitions remain positive controls.
- [x] 1.2 Add new runtime test `TestRuntimeScopedDefinitionParity` with the three exact definition repros and empty-scope case in the delta; use both shipped dialects and evaluator modes, and confirm VM failures against pinned tree-walker results before fixing them.
- [x] 1.3 Add new runtime test `TestRuntimeScopedDefinitionFallbackOnce` with an observable mutation before a scoped definition, plus repeated evaluation and calls of a fallback-created function; verify each evaluation mutates exactly once and that no compiled prefix executes before fallback.

No project wrapper selects these packages/tests; use:

```sh
timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core/compiler ./core/vm ./runtime -run 'Test(CompilerScopedDefinitionFallback|RuntimeScopedDefinitionParity|RuntimeScopedDefinitionFallbackOnce)$'
```

## 2. Targeted compiler fallback

- [x] 2.1 Recognize lexical-definition scopes independently of binding count and syntax depth; apply existing typed unsupported handling to scoped `def` and both `defn` routes after shape validation, then verify `TestCompilerScopedDefinitionFallback` passes, including top-level `do` controls.
- [x] 2.2 Propagate refusal to the whole enclosing top-level form using existing runtime fallback paths; verify cold/repeated evaluation, Lisp-2 namespace isolation, fallback-created function application and once-only side-effect tests pass without allocating an environment for unaffected compiled calls.
- [x] 2.3 Verify existing nested-`defmacro` fallback and ordinary top-level definitions still behave as documented; extend existing regression tables only where the new scope guard interacts with those paths and confirm the targeted command passes.

## 3. Integration and documentation

- [x] 3.1 Run `timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/... ./runtime ./internal/goldset`; record results, including unaffected compiled closure/capture and resource-terminal behavior. Keep counter equality requirements limited to existing contracts.
- [x] 3.2 Run `timeout 10m go test -race -timeout 2m -p 2 -parallel 2 ./runtime`; verify concurrent evaluation and repeated fallback preserve environment isolation and side-effect counts.
- [x] 3.3 Update existing `ARCHITECTURE.md`, `docs/adr/0002-bytecode-vm-disposition.md`, `docs/adr/0013-bytecode-default-authorized-by-the-gold-set.md`, and related existing compiler/runtime fallback comments; verify all name scoped-definition whole-form fallback accurately and preserve the flat-closure architecture.
- [x] 3.4 Add a `[Unreleased]` entry in existing `CHANGELOG.md` describing lexical-definition correctness and affected compiled-subset performance; verify it makes no promise of unchanged reduction counts.
- [x] 3.5 Run `timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, `timeout 5m make lint`, and `openspec validate vm-scoped-definition-fallback --strict --json`; record results and archive only after implementation validation passes.
