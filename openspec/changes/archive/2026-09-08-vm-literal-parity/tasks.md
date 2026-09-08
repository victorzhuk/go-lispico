## 0. Scope

- [x] 0.1 Confirm existing-service-strict behavior scope against `compileList`, `evalQuote` and empty-list evaluation; verify the change is independent of frame layout and excludes parser, truthiness and broader arity redesign.

## 1. Regressions before implementation

- [x] 1.1 Extend `TestCompiler_MalformedForms` with extra quote operands and add new test `TestCompilerEmptyListValue`; verify `(quote 1 2)` is wrongly accepted and `()` wrongly becomes nil before the compiler correction.
- [x] 1.2 Add new cross-evaluator/runtime test `TestVMLiteralParity` for `(quote)`, `(quote 1 2)`, `(quote missing)`, `()`, and `(quote ())` under both shipped dialects; assert typed rejection for malformed forms and value/type parity for valid forms through cold and repeated evaluation, then confirm the current VM failures.

No project wrapper selects these packages/tests; use:

```sh
timeout 5m go test -timeout 2m -p 2 -parallel 2 ./core/compiler ./core/vm ./runtime -run 'Test(Compiler_MalformedForms|CompilerEmptyListValue|VMLiteralParity)$'
```

## 2. Compiler correction

- [x] 2.1 Require exactly one quote operand before indexing or emission; verify malformed cases return typed compile errors and valid quotation still returns an unevaluated symbol with the targeted command.
- [x] 2.2 Compile an evaluated empty list through the existing plain constant path; verify list runtime type and structural equality under both evaluator modes without introducing collection-construction charges or depth checks.

## 3. Integration and documentation

- [x] 3.1 Run `timeout 10m go test -timeout 2m -p 2 -parallel 2 ./core/compiler ./core/vm ./runtime ./internal/goldset`; record command, test names and results, including existing quotation, literal-allocation and dialect cases.
- [x] 3.2 Add a `[Unreleased]` entry in existing `CHANGELOG.md` for quote-arity rejection and empty-list type preservation; verify the text describes the two observable changes without implying a reader or dialect change.
- [x] 3.3 Run `timeout 10m make test GOTESTFLAGS='-timeout 2m -p 2 -parallel 2'`, `timeout 5m make lint`, and `openspec validate vm-literal-parity --strict --json`; record results and archive only after implementation validation passes.
