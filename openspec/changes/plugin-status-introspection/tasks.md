## 1. Behavior contracts

- [ ] 1.1 Red test: an engine with `stdlib` + `data` + a frozen plugin (e.g.
  `net`) + `fsm` reports `active`/`active`/`frozen`/`idle` from `ListPlugins()`,
  not all `active`.
- [ ] 1.2 Red test: a third-party plugin that omits the lifecycle field reports
  `active`.
- [ ] 1.3 Characterization: the set of registered/callable plugins is unchanged
  by this change.

## 2. Implementation

- [ ] 2.1 Add a `Lifecycle` (active/idle/frozen) field to `core.PluginMeta`,
  defaulting to active when unset.
- [ ] 2.2 First-party frozen/idle plugins declare their lifecycle in
  `Metadata()`.
- [ ] 2.3 `ListPlugins()` reads the lifecycle from `Metadata()` instead of the
  hardcoded `"active"`.

## 3. Integration

- [ ] 3.1 `go test ./... -race` green.
- [ ] 3.2 `go build ./...` — verify no third-party-plugin construction breaks
  (additive field).

## 4. Verification

- [ ] 4.1 `openspec validate --strict plugin-status-introspection`.
- [ ] 4.2 CHANGELOG `[Unreleased]` under Fixed: `ListPlugins()` reports real
  plugin lifecycle status instead of always "active".
