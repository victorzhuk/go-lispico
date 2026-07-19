## Why

The default engine runs the tree-walking evaluator; the VM is opt-in (`WithBytecode()`). Every consumer that does not discover the option — including the published comparison article — measures the slowest path. Measured on the article bench (v0.8.0, same code, only the option flipped): Call 723→260 ns, Callback 740→365 ns (ahead of goja's 767), Rule 1884→812 ns (near GopherLua's 725), fib stays the VM row. The tree-walker remains 2.8–11x behind on these workloads.

The promotion criteria set out in ADR 0006 (staged VM adoption) and ADR 0008 (consumer gate) are met: the VM is crossval-parity-verified on its compiled subset, falls back to the tree-walker per unsupported form, and the goldset gate has held non-increasing through five perf changes. Keeping the fast, parity-verified path opt-in is now the largest single performance decision in the project.

## What Changes

- `runtime.New` enables the bytecode evaluator by default: compiled subset on the VM, per-form fallback to the tree-walker, chunk cache active.
- Evaluator selection becomes an explicit last-wins option pair: `WithBytecode()` (explicit select, kept for source compatibility) and new `WithTreeWalker()` (opt-out for embedders that want the complete single-path evaluator).
- The tree-walker remains the complete, supported evaluator — nothing is removed; it stops being the default entry path.
- Startup cost note: bytecode startup currently recompiles stdlib per engine (120 µs vs 74 µs tree). `stdlib-startup-cache` addresses it; both changes land in the same release so the default flip never ships a startup regression.
- ADR 0002 (bytecode disposition) and ADR 0006 (staged adoption) amendment notes recording the promotion and its evidence; README / ARCHITECTURE / CLAUDE.md wording updates ("opt-in optimizer" → "default execution path with tree-walker fallback").

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `bytecode-vm`: `Bytecode VM execution` — the VM is the default evaluator rather than opt-in; fallback and parity obligations unchanged.
- `runtime-api`: new requirement `Default evaluator selection` — default bytecode, `WithTreeWalker()` opt-out, `WithBytecode()` explicit select, last-wins composition.

## Impact

- Code: `runtime/engine.go` (config default, `WithTreeWalker` option), docs, ADR amendments; test matrix already runs both modes via goldset/crossval and keeps doing so.
- Consumers: YAGEL and any embedder get the VM path on upgrade; behavior differences are bounded by the crossval parity contract; `WithTreeWalker()` is the escape hatch. CHANGELOG entry marks the behavioral default change prominently.
- Expected: article-bench Call/Callback/Rule move to the probe numbers above with zero consumer code changes.
- Sequencing: after `vm-budget-only-polls` and `env-cell-versioned-reads` (so the promoted default carries their gains); same release as `stdlib-startup-cache`.
