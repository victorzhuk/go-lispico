## ADDED Requirements

### Requirement: Registration view attributes and reverts owned root writes

A registration view over a root environment SHALL forward every read and write to that root, and the root's identity SHALL NOT change. While its operation is active, writes reached through the view SHALL be attributed to that operation, including writes reached through an owner returned by lookup, a child scope, evaluator reentry, a closure capturing the view, or a merge whose target is the view. Writes through the raw root SHALL be unattributed even when their value equals an attributed write.

Aborting the operation SHALL, for each name the operation wrote, restore the prior value, canonical status, presence, and deletion state only when the current state is still the operation's latest write, and SHALL otherwise leave the current state untouched. Abort SHALL restore prior cells in place rather than install replacement cells, SHALL keep cell versions, name generations, and macro epochs monotone, and SHALL invalidate cached lookups that observed a reverted definition. After completion or abort the view SHALL forward as an ordinary unattributed environment. An environment with no active operation SHALL keep its existing binding behavior.

#### Scenario: Abort restores overwritten bindings and removes added names

- **WHEN** an operation overwrites existing value and function bindings, including canonical ones, adds new names, and aborts with no foreign writes
- **THEN** prior values and canonical status SHALL be restored in the same cells, added names SHALL be absent, and version counters SHALL NOT decrease

#### Scenario: Foreign writes survive abort

- **WHEN** a raw-root write rebinds, adds, deletes, or deletes and recreates a name after the operation wrote it, with or without a further operation write to the same name, and the operation then aborts
- **THEN** the raw-root write's resulting state SHALL remain

#### Scenario: Aliases stay attributed

- **WHEN** an operation writes through an owner found by lookup, a child scope, evaluator reentry, a captured closure, or a merge targeting the view, and then aborts
- **THEN** each of those writes SHALL be reverted under the same conflict rule

#### Scenario: Completed view keeps forwarding

- **WHEN** a view is retained after its operation completes or aborts and the root is later rebound
- **THEN** reads through the view SHALL observe the rebinding and later writes through it SHALL be unattributed

#### Scenario: Caches drop a reverted definition

- **WHEN** a cached lookup or macro expansion observed a definition the operation wrote and the operation aborts
- **THEN** the next use SHALL observe the restored binding
