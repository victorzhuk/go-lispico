## 1. Parity pinning (before any code)

- [x] 1.1 Crossval cells pinning tree-walker capture semantics: closure-in-`loop` (fresh vs shared per iteration), closure-in-`let`-in-`loop`, `set!` aliasing between sibling closures, defining-scope mutation visibility, transitive nested capture. These cells are the spec for every later task.

## 2. Compiler

- [x] 2.1 Capture descriptors per sub-chunk: each free variable → enclosing captured slot N or enclosing-closure capture index M; extend `MarkCaptures` output.
- [x] 2.2 Emission: box allocation at captured binding sites (position fixed by 1.1); `OpGetCell`/`OpSetCell` for captured slots, existing opcodes untouched for uncaptured; `OpClosure` operand references the descriptor list.
- [x] 2.3 `Chunk.Validate` cases for every new opcode operand (slot ranges, descriptor indices) — validation-completeness lesson.

## 3. VM

- [x] 3.1 `cellBox` (VM-internal), frame handling, `OpClosure` capture materialization from frame slots / enclosing `caps`; Closure carries `{chunk, caps, globals}`; `vm.apply` and `reloadFrame` drop the lexical-env plumbing for closures.
- [x] 3.2 Delete `FullEnv` mirroring and `env.Set`-from-`OpSetLocal`; global/`set!`-lexical opcodes resolve against the closure's globals env.
- [x] 3.3 Audit: no compiled-subset path observes locals by name at runtime (the flat-closure soundness invariant); document the audit in the design notes.

## 4. Verify

- [x] 4.1 All 1.1 cells + full crossval parity suite green on both dialects; `go test ./...`, `-race` green.
- [x] 4.2 `GOLDSET_MODE=vm` gate non-increasing — attention on loop cells mutating captured locals (expected to drop) and closure-creation cells (one box per captured binding).
- [x] 4.3 Benchstat ≥6: closure-heavy shapes (counter/accumulator closures, callback factories), loop-sum family, fib (expected ~flat), memory retention spot-check (closure no longer pins its scope chain).
