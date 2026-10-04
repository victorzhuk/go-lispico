## Context

The dialect layer was rewritten across the seven archived changes of the 2026-09-26..2026-10-03 set. The rewrite landed the behavior the corpus asks for, and it introduced the process-wide `vocabShape` cache in `runtime/engine.go` (`fix(runtime): scope vocabulary pass to the operation's written names`). This change fixes what the release review of that delta found, before the release is cut.

## Decisions

- **Adapter values are per dialect.** `vocabShape` keeps only the sorted adapter names; `applyVocabulary` reads each adapter's value from the engine's own dialect through `Dialect.VocabEntry`. The fingerprint is deliberately value-blind — it identifies semantics, not object identity — so it may key a structural cache and must never key a value.
- **The shape cache stays fingerprint-keyed.** It exists so `New` and `Use` do not copy the vocabulary map; the change keeps that and bounds what an entry can retain to names.
- **Unknown base is a construction error.** `DialectSpec.Base` documents two values. A third accepted silently is an inconsistent mode: an empty table that `IsBaseEmpty` reports false and the empty-base allowlist filter ignores. Rejecting it makes this guard the same kind as every other one in the constructor.
- **The scaling test becomes deterministic.** A wall-clock ratio on a shared machine is not evidence: one test measured 7.25 under the parallel floor and 2.0 alone. Allocation counts are deterministic and catch the same regression, because a quadratic decode grows allocations per key.
- **Documents are corrected, not reworded.** Every drift fix replaces a stale number, tree entry, axis claim or link with the measured state; no document is restructured.
- **The two spec amendments travel as deltas.** `bytecode-vm` and `dialect` requirements are modified in this change's `specs/` and fold into the corpus at archive.

- **`bytecode-vm`'s execution requirement is renamed.** `Dialect-axis execution` becomes `Dialect-driven execution` (a `REMOVED` + `ADDED` pair): the tool refuses to drop a scenario inside a `MODIFIED` block, and the old name asserts an axis that no longer exists.

## Non-goals

- No new dialect axis, no cache eviction policy, no change to `Dialect.Fingerprint`.
- `docs/adr/0005-dialect-layer.md`'s Amendment block stays the record of the truthiness removal; only its stale body lines and the method it names are corrected.
- The historical `CHANGELOG.md` sections that quote the removed builder API stay as written: they are the record of earlier releases.

## Plan appendix

```json
{
  "v": 2,
  "change": "dialect-adapter-identity-and-drift",
  "baseSha": "1401f451b7793234812c85e3a39c509a3b16ae75",
  "generatedAt": "2026-10-04T17:36:59.126Z",
  "tier": "standard",
  "mode": "existing-service-strict",
  "chunks": [
    {
      "id": "adapter-identity",
      "taskIds": [
        "1.1",
        "1.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-adapter-identity",
      "shard": "",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "runtime/vocab_adapter_identity_test.go",
          "symbol": "TestVocabAdapterIdentity_PerDialectValues",
          "anchor": "",
          "change": "Create the test: two engines built from specifications whose adapter maps share a visible name and semantic ID but carry different values must each call their own value."
        },
        {
          "task": "1.2",
          "file": "runtime/engine.go",
          "symbol": "vocabShapeOf",
          "anchor": "func vocabShapeOf(d core.Dialect) *vocabShape {",
          "change": "Keep only the sorted adapter names in the cached shape, and read each adapter value from the engine dialect through Dialect.VocabEntry when the vocabulary is installed."
        }
      ],
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "1.2"
      ],
      "redTests": [
        "TestVocabAdapterIdentity_PerDialectValues"
      ],
      "redRun": "go test ./runtime -run 'TestVocabAdapterIdentity_PerDialectValues' -timeout 120s",
      "verify": "timeout 300s go test -timeout 120s ./runtime",
      "coder": "go-coder"
    },
    {
      "id": "base-validation",
      "taskIds": [
        "2.1",
        "2.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-base-validation",
      "shard": "",
      "pkgDirs": [
        "core"
      ],
      "pkgs": [
        "./core"
      ],
      "sites": [
        {
          "task": "2.1",
          "file": "core/dialect_spec_test.go",
          "symbol": "TestNewDialect_RejectsUnknownBase",
          "anchor": "",
          "change": "Add the test: DialectSpec{Base: DialectBase(2)} must return an error and the zero Dialect, and the error must name the base."
        },
        {
          "task": "2.2",
          "file": "core/dialect.go",
          "symbol": "validateSpec",
          "anchor": "func validateSpec(spec DialectSpec, hide []string) error {",
          "change": "Reject a Base that is neither BaseFull nor BaseEmpty, before the dialect is resolved."
        }
      ],
      "redTasks": [
        "2.1"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestNewDialect_RejectsUnknownBase"
      ],
      "redRun": "go test ./core -run 'TestNewDialect_RejectsUnknownBase' -timeout 120s",
      "verify": "timeout 300s go test -timeout 120s ./core",
      "coder": "go-coder"
    },
    {
      "id": "json-scaling-determinism",
      "taskIds": [
        "3.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-json-scaling-determinism",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "3.1",
          "file": "plugins/json/json_test.go",
          "symbol": "TestDecodeHashMap_Scaling",
          "anchor": "func TestDecodeHashMap_Scaling(t *testing.T) {",
          "change": "Replace the wall-clock ratio assertion with a deterministic allocation-ratio assertion (testing.AllocsPerRun at 2000 and 4000 keys), and keep the measurement logged."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "3.1"
      ],
      "redTests": [],
      "redRun": "",
      "waiver": "NO-RED-WAIVER: the chunk rewrites an existing repo-owned timing test; no production behaviour changes",
      "verify": "timeout 300s go test -timeout 120s ./plugins/json",
      "coder": "go-coder"
    },
    {
      "id": "doc-counts-and-trees",
      "taskIds": [
        "4.1",
        "4.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-doc-counts-and-trees",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "4.1",
          "file": "AGENTS.md",
          "symbol": "Status bullet and core tree",
          "anchor": "- Core interpreter with 13 types and 22 special forms",
          "change": "Set the count to 21 special forms and add core/dialect.go and core/callable.go to the core tree."
        },
        {
          "task": "4.2",
          "file": "ARCHITECTURE.md",
          "symbol": "special-forms table header and core tree",
          "anchor": "22 special forms handled directly by the evaluator:",
          "change": "Set the header count to 21 and add core/dialect.go, core/callable.go and core/registration.go to the core tree."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "4.1",
        "4.2"
      ],
      "redTests": [],
      "redRun": "",
      "waiver": "NO-RED-WAIVER: the chunk corrects counts and file trees in two documents; there is no production behaviour to assert.",
      "verify": "timeout 120s openspec validate dialect-adapter-identity-and-drift --strict",
      "coder": "go-coder"
    },
    {
      "id": "adr-and-prd-drift",
      "taskIds": [
        "5.1",
        "5.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-adr-and-prd-drift",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "5.1",
          "file": "docs/adr/0005-dialect-layer.md",
          "symbol": "TruthyFunc and the truthiness axis lines",
          "anchor": "Dialect.TruthyFunc()",
          "change": "State that TruthyFunc and NilOnlyFalsy are removed and truthiness is the fixed core.IsTruthy rule, in the body and the consequences as well as the amendment."
        },
        {
          "task": "5.1",
          "file": "docs/adr/0006-vm-first-staged.md",
          "symbol": "the three-axes sentence",
          "anchor": "",
          "change": "Drop the truthiness predicate from the dialect axes: two axes, rename normalization and the Lisp-2 function cell."
        },
        {
          "task": "5.2",
          "file": "docs/prd/dialect-layer.md",
          "symbol": "semantic axes and evaluator default",
          "anchor": "Two semantic axes are configurable in v1",
          "change": "State one configurable axis (symbol namespaces), a fixed truthiness rule, and the bytecode VM as the default and complete path."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "5.1",
        "5.2"
      ],
      "redTests": [],
      "redRun": "",
      "waiver": "NO-RED-WAIVER: the chunk corrects ADR and PRD prose about a removed axis; there is no production behaviour to assert.",
      "verify": "timeout 120s openspec validate dialect-adapter-identity-and-drift --strict",
      "coder": "go-coder"
    },
    {
      "id": "deps-and-release-notes",
      "taskIds": [
        "6.1",
        "6.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "seam": "fastpass-deps-and-release-notes",
      "shard": "",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "6.1",
          "file": "openspec/changes/DEPENDENCIES.md",
          "symbol": "active change dependency order",
          "anchor": "# Active change dependency order",
          "change": "Record the empty active set and list all seven dialect changes as archived, each linked under archive/, with the 2026-09-26 decisions kept as the landed record."
        },
        {
          "task": "6.2",
          "file": "CHANGELOG.md",
          "symbol": "Unreleased Changed section",
          "anchor": "- `compiler.NewCompilerWithDialect(name, dialect)` takes the `core.Dialect`",
          "change": "Add the two breaking signature entries: Dialect.CanonicalName loses its third result, and Dialect.NormalizeCond returns []core.CondClause."
        }
      ],
      "redTasks": [],
      "codeTasks": [
        "6.1",
        "6.2"
      ],
      "redTests": [],
      "redRun": "",
      "waiver": "NO-RED-WAIVER: the chunk rewrites the change-set record and two changelog entries; there is no production behaviour to assert.",
      "verify": "timeout 120s openspec validate dialect-adapter-identity-and-drift --strict",
      "coder": "go-coder"
    }
  ],
  "seams": [
    {
      "id": "fastpass-adapter-identity",
      "tasks": [
        "1.1",
        "1.2"
      ],
      "redTasks": [
        "1.1"
      ],
      "summary": "Fastpass chunk adapter-identity: tasks 1.1, 1.2 from tasks.md, no authored contract — the red stage works from the task text and the sites."
    },
    {
      "id": "fastpass-base-validation",
      "tasks": [
        "2.1",
        "2.2"
      ],
      "redTasks": [
        "2.1"
      ],
      "summary": "Fastpass chunk base-validation: tasks 2.1, 2.2 from tasks.md, no authored contract — the red stage works from the task text and the sites."
    },
    {
      "id": "fastpass-json-scaling-determinism",
      "tasks": [
        "3.1"
      ],
      "redTasks": [],
      "summary": "NO-RED-WAIVER: fastpass run with the red stage waived by the user (test-only determinism rewrite: the existing wall-clock ratio cannot be made to fail deterministically at base); tasks 3.1 are verified by the chunk command and the floor alone."
    },
    {
      "id": "fastpass-doc-counts-and-trees",
      "tasks": [
        "4.1",
        "4.2"
      ],
      "redTasks": [],
      "summary": "NO-RED-WAIVER: fastpass run with the red stage waived by the user (documents only: no behaviour to prove red); tasks 4.1, 4.2 are verified by the chunk command and the floor alone."
    },
    {
      "id": "fastpass-adr-and-prd-drift",
      "tasks": [
        "5.1",
        "5.2"
      ],
      "redTasks": [],
      "summary": "NO-RED-WAIVER: fastpass run with the red stage waived by the user (documents only: no behaviour to prove red); tasks 5.1, 5.2 are verified by the chunk command and the floor alone."
    },
    {
      "id": "fastpass-deps-and-release-notes",
      "tasks": [
        "6.1",
        "6.2"
      ],
      "redTasks": [],
      "summary": "NO-RED-WAIVER: fastpass run with the red stage waived by the user (documents only: no behaviour to prove red); tasks 6.1, 6.2 are verified by the chunk command and the floor alone."
    }
  ],
  "requirements": [],
  "testHarness": [],
  "floor": "go test -timeout 10m ./... && timeout 600s make lint && timeout 120s openspec validate dialect-adapter-identity-and-drift --strict",
  "lenses": [
    "spec",
    "quality",
    "perf"
  ],
  "estimateHours": 3.6,
  "fastpass": true,
  "fastpassReason": "Pre-release consistency pass: a five-lens review of the v0.14.0..HEAD delta named every defect by file and line, so the fixes are mechanical and a planning fan-out would re-derive known sites.",
  "planRulings": [
    "Fastpass: no planner fan-out, no seam contracts, no plan review. Stated reason: Pre-release consistency pass: a five-lens review of the v0.14.0..HEAD delta named every defect by file and line, so the fixes are mechanical and a planning fan-out would re-derive known sites.. Every mechanical guard still applies — chunk cap, seal, guard, verify-or-waiver, floor, and the per-chunk command rules.",
    "chunk adapter-identity verify is a raw `go test` while Makefile declares `test-unit` — the project target keeps the cache and the project's own flags; prefer `make test-unit`",
    "chunk base-validation verify is a raw `go test` while Makefile declares `test-unit` — the project target keeps the cache and the project's own flags; prefer `make test-unit`",
    "chunk json-scaling-determinism verify is a raw `go test` while Makefile declares `test-unit` — the project target keeps the cache and the project's own flags; prefer `make test-unit`",
    "chunk base-validation site core/dialect_spec_test.go: no anchor — the file exists at baseSha, so every stage handed this site re-derives it. Pass sites: [{ file, symbol, anchor }] with the anchor you verified instead of files: […]",
    "chunk adr-and-prd-drift site docs/adr/0006-vm-first-staged.md: no anchor — the file exists at baseSha, so every stage handed this site re-derives it. Pass sites: [{ file, symbol, anchor }] with the anchor you verified instead of files: […]"
  ],
  "planReview": {
    "verdict": "fastpass",
    "reviewer": "none",
    "rounds": 0
  }
}
```
