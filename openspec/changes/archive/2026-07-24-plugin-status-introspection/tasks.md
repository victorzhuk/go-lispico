## 1. Behavior contracts

- [x] 1.1 Red test: an engine with `stdlib` + `data` + a frozen plugin (e.g.
  `net`) + `fsm` reports `active`/`active`/`frozen`/`idle` from `ListPlugins()`,
  not all `active`.
- [x] 1.2 Red test: a third-party plugin that omits the lifecycle field reports
  `active`.
- [x] 1.3 Characterization: the set of registered/callable plugins is unchanged
  by this change.

## 2. Implementation

- [x] 2.1 Add a `Lifecycle` (active/idle/frozen) field to `core.PluginMeta`,
  defaulting to active when unset.
- [x] 2.2 First-party frozen/idle plugins declare their lifecycle in
  `Metadata()`.
- [x] 2.3 `ListPlugins()` reads the lifecycle from `Metadata()` instead of the
  hardcoded `"active"`.

## 3. Integration

- [x] 3.1 `go test ./... -race` green.
- [x] 3.2 `go build ./...` — verify no third-party-plugin construction breaks
  (additive field).

## 4. Verification

- [x] 4.1 `openspec validate --strict plugin-status-introspection`.
- [x] 4.2 CHANGELOG `[Unreleased]` under Fixed: `ListPlugins()` reports real
  plugin lifecycle status instead of always "active".
