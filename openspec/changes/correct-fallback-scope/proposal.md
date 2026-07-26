## Why

Seven places in the repository — including a live spec scenario — state that
the bytecode compiler falls back to the tree-walker for "a `defmacro` **nested
in a body**". It falls back for every `defmacro`. The rejection sits in
`compileList`'s special-form switch (`core/compiler/compiler.go:289`), which is
reached for any list form, at any position:

```
(defmacro m (x) x)         -> BytecodeUnsupported: defmacro is not supported…
(do (defmacro m (x) x))    -> BytecodeUnsupported: defmacro is not supported…
```

Both compiled through `NewCompiler().Compile(...)` directly, so this is the
compiler's own answer rather than an inference from behaviour.

The qualifier matters because it changes what a reader concludes. "Nested in a
body" reads as a corner case that ordinary top-level macro definitions avoid.
In fact any source defining a macro has that form evaluated by the tree-walker
and never cached as a chunk — which is why `twice-macro` is the gold set's
slowest engine-sensitive cell, and why two changes landed today were needed to
stop it recompiling its neighbours.

Nothing here changes behaviour. The claim was wrong; the code was not.

## What Changes

- The wording is corrected in all seven places: the `bytecode-vm` spec scenario,
  `CLAUDE.md`, `README.md`, `docs/adr/0002-bytecode-vm-disposition.md`,
  `docs/adr/0013-bytecode-default-authorized-by-the-gold-set.md`, and the doc
  comments on `CodeUnsupported` (`core/compiler/compiler.go`) and
  `isUnsupportedInBytecode` (`runtime/eval.go`).
- A compiler test pins the fact, so the wording cannot drift back without a
  failing test.

No behaviour change, no API change.

## Capabilities

### Modified Capabilities

- `bytecode-vm`: the `Unsupported form defers to the tree-walker` scenario names
  the trigger incorrectly. It gains the accurate one. The requirement's
  substance — typed error, tree-walker fallback, never panic — is unchanged.

## Impact

- Code: documentation and two doc comments; one new test.
- Risk: none to behaviour. The risk this removes is a reader — human or agent —
  planning against a fallback surface narrower than the real one.
  `CLAUDE.md` is the file agents read first, which is how this survived.
- Not in scope: making the compiler support `defmacro`. That remains open, and
  correcting the record is a precondition for scoping it honestly rather than a
  substitute for doing it.
