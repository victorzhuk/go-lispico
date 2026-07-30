---
status: accepted
---

# The bytecode default is authorized; the gold set now guards non-regression

ADR 0006's amendment records that `runtime.New()` defaults to bytecode VM
execution. ADR 0008's third consequence still says changing the global Engine
default "requires the dialect-wide evidence anticipated by ADR 0006", written
when the gate did not yet exist. Read together the two records contradict each
other on whether the flip was authorized.

The flip is authorized and shipped. The evidence ADR 0006 asked for is the
repo-owned gold set of ADR 0008: it runs every fixture under both execution
modes against goldens hand-derived from the language contract. That
correctness leg passed, and `runtime.New()` flipped. ADR 0008's consequence is
superseded by this record: the gate's standing role from here is VM
non-regression on later changes, not authorizing a flip that already happened.

The performance leg came from elsewhere. This record originally read "the
perfgate tiers decide the performance cells. Passing it was the condition; it
passed", which says the hosted ADR-0008 workflow returned that verdict. It did
not: `.github/workflows/release.yml` had never executed when the default
flipped. What stood behind the flip is the ad hoc benchstat evidence recorded
in `openspec/changes/archive/2026-07-20-engine-bytecode-default/tasks.md` task
4.3, gathered against a separate bench repository, with no stored baseline
asset and no tier-threshold judgment of the kind ADR 0008 describes. Whether
the flip was justified is not reopened here; only the attribution of which
artifact produced its evidence.

## Consequences

- ADR 0006's amendment and this record are the current statement of the
  default; ADR 0008 consequence 3 is historical.
- No release in this repository carries an ADR-0008 gate verdict yet. The
  hosted gate's first real run and its stored `bench-vm.txt` baseline are
  tracked by the `release-gate-activation` change; until that run exists,
  "the gate passed" is not a claim this repository can make.
- A future default change of the same weight (making the tree-walker the default
  again, or defaulting a third execution mode) needs a fresh gate run under both
  modes, not a new category of evidence.
- Forms the VM cannot compile still defer to the tree-walker form by form
  (a `defmacro` nested inside a larger form, `unquote-splicing`), and
  `runtime.WithTreeWalker()` remains the rollback. Neither is affected here.
- The gold set runs the Clojure dialect, so dialect-specific default behavior
  (Lisp-2 function cells, CL list bindings) is covered by the dialect test
  suites rather than the gate. A default-affecting dialect change must add its
  regression there.

## Considered options

- Amend ADR 0008's consequence in place: rejected — it would rewrite the
  reasoning of an accepted record whose gate design was correct at the time.
- Amend ADR 0006 to call the default provisional: rejected — the default is not
  provisional, and marking it so would misreport shipped behavior to embedders.
