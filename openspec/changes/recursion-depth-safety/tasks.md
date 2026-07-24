## 1. Behavior contracts

- [ ] 1.1 Red test: a `loop`/`recur` + `list` program that builds nesting past
  `MaxStructuralDepth` returns a terminal `ResourceLimitError` (both evaluators),
  and the error is not catchable by in-script `try`/`catch`.
- [ ] 1.2 Red test: `json/decode` of deeply nested input past the limit returns
  a terminal `ResourceLimitError` (not a crash, not an uncharged structure).
- [ ] 1.3 Red test: a macro expanding to an over-deep form fails compilation with
  a terminal `ResourceLimitError`.
- [ ] 1.4 Red test: `String()`/`Equals()` on a directly Go-constructed over-deep
  value returns a bounded result and does not stack-overflow (a unit test on the
  value methods, no evaluator needed).
- [ ] 1.5 Characterization: ordinary shallow values — including wide flat
  collections — construct, stringify, compare, and compile exactly as before.

## 2. Implementation

- [ ] 2.1 Add a bounded depth check (cap at `MaxStructuralDepth + 1`) and wire it
  into `OpMakeList`/`OpMakeVector`/`OpMakeMap` and the stdlib
  `list`/`cons`/`vector`/`conj`/`assoc`/`merge` builders and `json/decode`;
  return `CodeResourceLimit` on breach. Track per-value depth where practical to
  keep the check O(1) amortized.
- [ ] 2.2 Make `String`/`Equals`/`ValueDeepBytes`/`ValueNodeCount` depth-bounded
  with safe degradation (truncation marker / defined result / capped count).
- [ ] 2.3 `Compiler.Compile`: compare `compileDepth` to `maxCompileDepth` at the
  increment site; guard `literalDepth()` the same way; return `CodeResourceLimit`.

## 3. Integration

- [ ] 3.1 `go test ./... -race` green; add a build/run of the crash repros to
  confirm they now return errors (no `fatal error: stack overflow`).
- [ ] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — the depth check is an
  integer compare on the construction path; verify no allocation or measurable
  regression on the goldset cells.
- [ ] 3.3 Crossval parity: construction depth breaches error identically in both
  evaluators.

## 4. Verification

- [ ] 4.1 `openspec validate --strict recursion-depth-safety`.
- [ ] 4.2 CHANGELOG `[Unreleased]` under Fixed/Security: deeply nested value
  construction, value-tree walks, and macro-expanded compilation are now
  depth-bounded and return a terminal `ResourceLimitError` instead of crashing.
