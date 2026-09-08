## ADDED Requirements

### Requirement: Failed plugin operations restore owned binding state

When `Use` or `ReloadPlugin` returns an initialization, vocabulary, or settlement error, it SHALL remove the failed operation's engine-owned changes while preserving concurrent host writes. Without a competing host write, prior value and function bindings, canonical status, live handles, plugin registry/ownership, active-plugin count, deferred attachments, deletion state, and retained ownership SHALL remain as before the operation. Previously unmaterialized names SHALL remain resolvable after failed reload without forcing eager materialization.

A competing host write SHALL win over rollback, including a write to the same name between two writes by the failing plugin. Rollback SHALL preserve unrelated host materialization and deletion. Cache invalidation SHALL prevent stale failed definitions without reversing generations observed by concurrent users. The root environment's identity SHALL remain stable.

The environment supplied to `Plugin.Init` SHALL retain normal binding, lookup, evaluator, and lexical-scope behavior and SHALL continue to address the same root when retained after successful initialization; its pointer identity need not equal `RootEnv()`. Effects reached through that supplied environment SHALL remain attributable to the plugin operation. Independently retained environment references, plugin Go objects, and external effects are outside rollback. Reads during initialization need not be isolated from intermediate registration writes. Successful unload's existing last-writer semantics and ordinary `Eval` effects SHALL remain unchanged.

#### Scenario: Failed initial load restores overwritten bindings

- **WHEN** a plugin overwrites existing value and function bindings, adds new names, and initialization fails without competing host writes
- **THEN** old values and canonical status SHALL be restored, new names SHALL be absent, the failed plugin SHALL be unregistered, and existing handles SHALL remain usable

#### Scenario: Failed cold stdlib reload keeps deferred names

- **WHEN** a freshly loaded stdlib is replaced by the same plugin name with a different version whose initialization fails before any old builtin is materialized
- **THEN** the original plugin SHALL remain registered and its deferred arithmetic, function, and macro names SHALL remain available

#### Scenario: Partly materialized reload restores deletion state

- **WHEN** failed reload follows a mixture of materialization, user shadowing, and deletion of deferred plugin names
- **THEN** the original visible and deleted names SHALL retain their behavior, existing handles SHALL remain valid, and sibling engines sharing templates SHALL be unaffected

#### Scenario: Concurrent host writes survive abort

- **WHEN** a host adds, rebinds, or deletes root bindings while a plugin operation later fails
- **THEN** those host writes SHALL survive rollback, including when the plugin writes the same binding again after the host

#### Scenario: Concurrent host materialization survives abort

- **WHEN** a host first-resolves an unrelated deferred name while another plugin operation later fails
- **THEN** that materialized binding and its per-engine lazy state SHALL remain available without being mistaken for failed plugin output

#### Scenario: Vocabulary or retained rejection rolls back once

- **WHEN** initialization succeeds but vocabulary application or retained settlement rejects the operation
- **THEN** only the failed operation's owned changes SHALL be reverted, the error SHALL be returned, active-plugin counts SHALL remain correct, and retained credits SHALL neither leak nor be released twice

#### Scenario: Registration aliases retain ownership

- **WHEN** initialization reaches the supplied environment through lookup of an owning scope, a child scope, evaluator reentry, or an environment merge
- **THEN** resulting root writes SHALL remain part of the same plugin operation and SHALL obey its rollback conflict policy

#### Scenario: Successful registration remains live

- **WHEN** a plugin retains its supplied environment or creates closures against it and initialization succeeds
- **THEN** later calls SHALL observe root rebindings, and subsequent successful unload/reload SHALL preserve existing ownership semantics
