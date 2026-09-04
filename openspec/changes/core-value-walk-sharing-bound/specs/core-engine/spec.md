## MODIFIED Requirements

### Requirement: Value-tree walks cannot crash on pathological depth

`String`, `Equals`, `ValueDeepBytes`, and `ValueNodeCount` SHALL be depth-bounded
so that a value exceeding `MaxStructuralDepth` degrades safely — a truncation
marker for `String`, a defined result for `Equals`, a capped count for the
byte/node walks — rather than recursing until the Go stack overflows. Ordinary
values within the depth limit SHALL be walked exactly as before.

The depth bound alone SHALL NOT be treated as a bound on walk work. `core`
shares structure, so the number of nodes a walk visits is not bounded by the
allocation charged for the structure: a node reachable through several
references is visited once per reference. Each of these walks, and the
construction- and nested-element-depth checks, SHALL bound the work it performs
by a quantity the allocation ledger bounds — by traversing shared structure
once, by accruing an interruptible node budget, or by both.

While such a walk runs, cancellation, an expired absolute evaluation deadline,
and an exhausted reduction budget SHALL remain observable, and the walk SHALL
stop with the corresponding Terminal error. A walk that checks a context only
before it starts and after it finishes SHALL NOT establish compliance.

#### Scenario: Stringifying an over-deep value does not crash

- **WHEN** `String()` (or an `Equals`/deep-bytes walk) is called on a value deeper than `MaxStructuralDepth`
- **THEN** it SHALL return a bounded result and SHALL NOT trigger a Go stack overflow

#### Scenario: A shared structure of bounded cost is walked in bounded work

- **WHEN** a script conses a collection onto itself repeatedly, so the structure stays within the depth limit and its charged allocation stays small while the number of references into its nodes doubles each step
- **THEN** every value-tree walk over it SHALL complete within a work bound stated in terms of its charged allocation, rather than growing with the number of references

#### Scenario: A running walk observes Terminal conditions

- **WHEN** a value-tree walk is in progress and the evaluation is cancelled, its absolute deadline expires, or its reduction budget is exhausted
- **THEN** the walk SHALL stop at a bounded synchronization point and return the corresponding Terminal error rather than running to completion
