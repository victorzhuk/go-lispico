## 0. Baseline

- [x] 0.1 Enumerate every `Env` method and core/VM direct-field path that can reach a root while bypassing a forwarding view, and inspect `core/env_test.go`, `core/env_merge_test.go`, and Makefile test limits; verify the ownership checklist and a bounded focused test command are recorded before source changes.

## 1. Pin view and journal contracts

- [x] 1.1 Add core regressions for abort restoring overwritten value/function/canonical bindings and removing added names, foreign-write interleavings (op→host→abort, op→host→op→abort, host delete/recreate), and alias paths (`Find` owner, `Child`, evaluator reentry, merge target, captured closure, retained view after completion); verify each fails on assertions against the baseline.

## 2. Introduce the registration seam

- [x] 2.1 Add the forwarding registration view and operation journal without copying `Env` locks or lexical maps; verify canonical root identity stays fixed, view reads match root reads, and a retained view forwards unattributed after completion or abort.
- [x] 2.2 Route binding, canonical, and context `Set` variants, `ReplaceCell`, `Delete`, and cell lookups through the canonical owner, tagging only view-originated writes; verify an equal-value raw-root write is unowned and both namespaces pass.
- [x] 2.3 Route `Find`, `Child`, `ChildVariadic`, evaluator/capture paths, enumeration, `MergeInto`/`MergeIntoCanonical`, `Rebuild`, and root configuration accessors through owner-aware views; verify the 0.1 checklist has no raw-root escape.
- [x] 2.4 Implement per-entry before-images, foreign-write rebasing, and conditional abort under the root lock; verify op→host→abort and op→host→op→abort keep host state, including delete and recreate.
- [x] 2.5 Preserve live cell identity and monotone `NameGen`, cell versions, and `MacroEpoch` across abort and `Rebuild`; verify a cell obtained before the operation observes the restored value and VM site caches cannot retain a reverted definition.

## 3. Validate and document

- [x] 3.1 Run `go test -timeout 2m -p 2 -parallel 2 ./core ./core/vm -run 'Test(Env|Merge|Registration)'` with every new regression name included, the same under `-race`, `make lint`, and `openspec validate registration-journal --strict --json`; update existing architecture docs for the view and journal; verify no dependency, no `Plugin` interface change, and no behavior change for environments without an active operation.
