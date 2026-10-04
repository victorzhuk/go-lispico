## Why

The dialect-layer rewrite (v0.14.0..HEAD, 74 commits) is complete and its seven changes are archived, but a release review of that delta found six verified defects and the release is not shippable as-is.

- **Adapter value substitution.** `runtime.vocabShapeOf` caches a resolved shape by dialect fingerprint (`runtime/engine.go:110-157`) and stores the first dialect's concrete `Adapter.Value`s in it. `core.Dialect` fingerprints the adapter's semantic ID, not its Value (`core/dialect.go:361-370`), so two engines built from dialects with equal fingerprints but different adapter values get the first dialect's values (`runtime/engine.go:590-604`). The cache also retains every custom dialect's adapter values for the process lifetime, with no eviction.
- **Unknown base accepted.** `validateSpec` never checks `DialectSpec.Base` (`core/dialect.go:214-252`). `NewDialect(DialectSpec{Base: DialectBase(2)})` succeeds as a third, inconsistent mode: an empty special-form table that `IsBaseEmpty` reports false and the empty-base vocabulary filter ignores.
- **Flaky release gate.** `plugins/json.TestDecodeHashMap_Scaling` asserts a wall-clock ratio below 3. It measured 7.25 in a whole-module `make test` and about 2.0 alone, so the floor both `.github/workflows/ci.yml` and the release gate run fails at random.
- **Corpus drift.** `openspec/specs/bytecode-vm/spec.md` still requires the compiled-chunk cache to be keyed by dialect (`:730`) and keeps a truthiness-axis scenario (`:356`, `:367`) that no dialect can satisfy: the axis was removed and `openspec/specs/dialect/spec.md:309` forbids it.
- **Document drift.** `AGENTS.md:13` and `ARCHITECTURE.md:237` claim 22 special forms where the kernel registers 21; both file trees omit `core/dialect.go` and `core/callable.go`; `docs/adr/0005-dialect-layer.md`, `docs/adr/0006-vm-first-staged.md` and `docs/prd/dialect-layer.md` still describe a truthiness axis, `Dialect.TruthyFunc()` and a tree-walker default.
- **Stale dependency record and incomplete release notes.** `openspec/changes/DEPENDENCIES.md` still calls the seven archived dialect changes the active set and links to change directories that no longer exist; `CHANGELOG.md` `[Unreleased]` omits the two breaking signature changes on the exported Dialect API (`CanonicalName` arity, `NormalizeCond` returning `[]CondClause`).

## What Changes

- Resolve adapter values from the dialect in hand when a vocabulary is installed, and keep only adapter names in the process-wide shape cache.
- Reject a `DialectSpec.Base` that is neither `BaseFull` nor `BaseEmpty`.
- Replace the wall-clock scaling assertion with a deterministic allocation-ratio assertion.
- Amend the `dialect` and `bytecode-vm` requirements to the measured behavior.
- Correct the special-form counts, the two file trees, the ADRs and the PRD.
- Rewrite `DEPENDENCIES.md` as the landed record and add the two missing release-note entries.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `dialect`: each dialect binds its own adapter values; an unknown base is refused at construction.
- `bytecode-vm`: the compiled-chunk cache key carries no dialect term; there is no truthiness axis.

## Impact

`runtime/engine.go`, `core/dialect.go`, `plugins/json/json_test.go`, `AGENTS.md`, `ARCHITECTURE.md`, `docs/adr/0005-dialect-layer.md`, `docs/adr/0006-vm-first-staged.md`, `docs/prd/dialect-layer.md`, `openspec/changes/DEPENDENCIES.md`, `CHANGELOG.md`. No public API change: the adapter fix restores the documented behavior, and the base check turns a silently accepted invalid input into the documented error.
