## 1. Option and default

- [x] 1.1 Flip the engine config default to bytecode-enabled; add `WithTreeWalker()`; make `WithBytecode()`/`WithTreeWalker()` last-wins; update option doc comments.
- [x] 1.2 Tests: default engine uses the VM path (observable via a compiled-subset probe or exported test hook); `WithTreeWalker()` engine never enters the VM; last-wins composition both orders.

## 2. Suite posture

- [x] 2.1 Audit tests that construct engines without options and assumed tree-walker execution; pin `WithTreeWalker()` where the test targets the tree-walker specifically, leave the rest on the new default.
- [x] 2.2 Goldset/crossval: both `GOLDSET_MODE` modes still run; release baseline job unaffected.

## 3. Docs

- [x] 3.1 ADR 0002 and ADR 0006 amendment notes with the promotion evidence.
- [x] 3.2 README, ARCHITECTURE.md, CLAUDE.md, and the bytecode-vm spec Purpose block: VM is the default path, tree-walker the complete fallback; document `WithTreeWalker()`.
- [x] 3.3 CHANGELOG `[Unreleased]` Changed entry marking the default flip and the rollback option.

## 4. Verify

- [x] 4.1 `go test ./...`, `-race`, crossval green under the new default.
- [x] 4.2 `GOLDSET_MODE=vm` and `GOLDSET_MODE=eval` gates green.
- [x] 4.3 Bench evidence (benchstat ≥6, bench repo without its `WithBytecode` probe overrides): Call/Callback/Rule at probe-level numbers on the default engine; startup tracked against `stdlib-startup-cache` (no regression in the release).
