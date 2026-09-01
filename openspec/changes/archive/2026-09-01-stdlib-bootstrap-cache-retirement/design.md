## Context

`get-in` is the only stdlib bootstrap entry marked reusable and the only caller
that can reach the process-level artifact cache. Macro definitions capture their
defining environment and are intentionally excluded. Once `get-in` becomes a
Builtin, cache entries, hits, and compiles remain permanently zero.

The per-Engine compiled-chunk cache is a separate subsystem with byte/node/entry
ceilings, macro-epoch invalidation, retained charging, and observable Engine
statistics. It remains fully useful and is not part of this retirement.

## Goals / Non-Goals

**Goals:**

- Delete the producerless process-level cache and all supporting surface.
- Preserve eager/lazy bootstrap values and cross-Engine isolation.
- Keep the bounded per-Engine compiled-chunk cache unchanged.
- Make the exported source-registration seam describe only behavior that exists.

**Non-Goals:**

- Add a replacement global cache or make environment-capturing macros reusable.
- Change bytecode cache admission, LRU, metering, or macro invalidation.
- Revisit whether a future pure source definition could justify a new cache.

## Decisions

### Delete rather than retain dormant infrastructure

Remove the artifact map, key/fingerprint hashing, compilation/replay path, test
statistics, disable hooks, and the `EvalStdlibBootstrap` evaluator interface/path
used only to enter that cache. Delete bootstrap artifact map/stat/disable/reset
symbols and their cache keys and bounds. Retain `core.BootstrapDefiner` and its
`DefineBootstrap` operation from `stdlib-bootstrap-evaluator-ownership`; eager
and lazy definitions still require that non-caching ownership seam. Compile-time
assertions pin both remaining evaluator implementations.

A dormant cache was rejected because it keeps concurrency and resource contracts
alive without a workload; speculative future source can propose a cache with its
actual constraints.

### Simplify source registration

Bootstrap entries no longer carry a `reusable` field. `Env.RegisterSource` becomes
`RegisterSource(name, source string) bool`, and `LazyLayer.RegisterSource` becomes
`RegisterSource(env *Env, name, source string) bool`; neither implementation nor
caller stores a reuse policy. All remaining source definitions use
`core.BootstrapDefiner` on the environment-owned evaluator.

The lazy layer MAY still share immutable name/source template metadata used to
install per-environment definitions. It SHALL NOT share compiled chunks,
evaluation results, Macros/Lambdas, cells, or defining environments. This
distinction preserves cheap source discovery without resurrecting the artifact
cache or cross-Engine closures.

Keeping an ignored public boolean was rejected because it advertises behavior
that no longer exists. This repository is alpha, so the exported Go seam is
changed directly and called out as breaking.

### Preserve behavior tests, remove cache implementation tests

Retain startup behavior goldens, concurrent Engine construction under `-race`,
Dialect isolation, unload/reload, lazy first touch, and macro redefinition tests.
Delete assertions about global entries/hits/misses/compiles and update benchmarks
to measure startup without cache toggles.

### Narrow canonical cache contracts

The bytecode contract continues to require the bounded per-Engine chunk cache but
removes process-level plugin compilation reuse and cross-Engine artifact
scenarios. The runtime cache-limit contract drops the exemption sentence because
the exempt cache no longer exists.

## Risks / Trade-offs

- [Risk] A cache-specific test was also guarding isolation → preserve every
  behavior assertion in cache-independent startup/unload tests before deletion.
- [Risk] Public lazy-layer implementations stop compiling → mark the signature
  change breaking and let the compiler enumerate every implementation.
- [Risk] A hidden reusable caller remains → add static searches/tests proving no
  `reusable` field/parameter, `EvalStdlibBootstrap` path, artifact map/stat/
  disable/reset hook, or process cache symbol survives.
- [Risk] Cache deletion accidentally removes the ownership seam → assert both
  evaluators still implement `core.BootstrapDefiner` and run eager/lazy tests.

## Migration Plan

First preserve cache-independent behavior goldens, then remove callers and the
source-registration flag, delete only cache machinery, and finally narrow
specs/docs while retaining the ownership capability.
Rollback restores the deleted cache and interface from version control; no
persisted artifacts or on-disk cache format exist.
