# consumer-release-gate — delta

## ADDED Requirements

### Requirement: Gate corpus dialect and recursion coverage

The gold-set corpus SHALL either (a) include at least one fixture exercising
the shipped engine's default dialect configuration in addition to any
fixture exercising `clojure.Dialect()`, and at least one fixture exercising
deep call-stack recursion rather than only bounded iteration or closure
state; or (b) explicitly document, in this specification, that gate coverage
is scoped to one dialect and non-recursive shapes by decision, with the
excluded path's regression protection assigned elsewhere (dialect test
suites, a named follow-up). A benchmark added specifically to cover a gap
in this corpus SHALL live where the gate actually runs — a benchmark that
exists only in a package the release workflow does not execute SHALL NOT be
cited as closing that gap.

#### Scenario: A dialect-coverage benchmark lives where the gate looks

- **WHEN** a benchmark is added specifically because the gold-set corpus does not exercise a dialect the shipped engine defaults to
- **THEN** that benchmark SHALL be part of `internal/goldset` (or another package the release workflow actually executes), not a package outside the workflow's run step

#### Scenario: An excluded path is a stated decision, not a silent gap

- **WHEN** the gold-set corpus does not cover a dialect configuration or an execution shape (such as deep recursion)
- **THEN** this specification SHALL name the exclusion explicitly and state where that path's regression protection comes from instead
