## Why

Several value-tree walks recurse in Go with no depth bound, so a sufficiently
deep value produces `fatal error: stack overflow` — a Go fatal error, not a
panic, which none of the engine's `recover()` boundaries can catch. This
violates the "no panics escape / all errors returned gracefully" invariant with
an uncatchable host crash. Reproduced:

- `List.String`/`Equals`, `Vector.String`/`Equals`, `*HashMap.String`/`Equals`
  (`core/types.go`), and `core.ValueDeepBytes`/`ValueNodeCount` recurse with no
  guard. A ~3M-deep value crashes `String()` (reproduced directly).
- `Compiler.Compile` (`core/compiler/compiler.go:118`) recurses unbounded. It
  tracks `compileDepth` but never compares it to a limit. The reader caps
  nesting, but `MacroExpand` runs before `Compile`, and a macro can build an
  arbitrarily deep expansion that reaches `Compile` unbounded (reproduced —
  crash in `Compile`→`compileCall`).

At stock 64-bit defaults the reduction/allocation ledger incidentally stops the
pure-script vectors short, but the crash is reachable under a raised
`MaxAllocationBytes` (a documented, invited config action), a smaller stack
(32-bit or `debug.SetMaxStack`), or host code calling a value method on a deep
value returned from evaluation. The unbounded recursion is the root defect
regardless of ledger tuning.

## What Changes

- Value construction that can deepen a value SHALL be bounded: the VM
  `OpMakeList`/`OpMakeVector`/`OpMakeMap` and the stdlib builders
  (`list`/`cons`/`vector`/`conj`/`assoc`/`merge`) and `json/decode` SHALL reject
  a result whose nesting exceeds the structural-depth limit with a terminal
  `ResourceLimitError`, using a bounded-depth check that itself cannot overflow.
- `Compiler.Compile` SHALL compare its existing `compileDepth` against a limit
  and return a terminal `ResourceLimitError` when exceeded; `literalDepth()`
  gets the same guard.
- The value methods `String`/`Equals` and `ValueDeepBytes`/`ValueNodeCount`
  SHALL be depth-bounded so any future caller — not only today's known ones — is
  protected against a stack-overflow crash on a pathological value.
- The bound reuses the existing structural-depth default (1024); breaches are
  terminal (`CodeResourceLimit`), consistent with how literal structural depth
  is already enforced.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: new requirement that value construction, value-tree walks
  (`String`/`Equals`/deep-bytes/node-count), and bytecode compilation are all
  depth-bounded, converting a previously uncatchable stack-overflow crash into a
  terminal `ResourceLimitError`.

## Impact

- Code: `core/types.go` (value methods), `core/value*.go` (`ValueDeepBytes`/
  `ValueNodeCount`), `core/compiler/compiler.go` (`Compile`/`literalDepth`),
  `core/vm/vm.go` (`OpMake*`), `plugins/stdlib` builders, `plugins/data`
  (`json/decode` — already deep-charges; add the depth gate).
- Behavior: a pathological deep value or macro expansion now fails with a
  terminal `ResourceLimitError` instead of crashing the process.
- Security: closes the untrusted-script and raised-limit stack-overflow vectors;
  a precondition for claiming "safe to embed untrusted scripts."
