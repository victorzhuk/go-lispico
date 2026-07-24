## Why

`ListPlugins()` (`runtime/plugin.go:324`) hardcodes `Status: "active"` for every
registered plugin, so the public introspection API reports the frozen
(`llm`/`agent`/`lio`/`net`/`exec`) and idle (`fsm`) plugins as active,
contradicting ADR 0004 and every doc that lists them as frozen/idle. This is a
functional bug in the public API, not just stale prose — an embedder inspecting
plugin state to decide what to trust or load gets wrong information.

## What Changes

- `ListPlugins()` SHALL report each plugin's real lifecycle status rather than a
  hardcoded `"active"`.
- Introduce a lifecycle-status source: either a `Status`/`Lifecycle` field on
  `core.PluginMeta` that each plugin declares (active/idle/frozen), or a runtime
  mapping keyed on the plugin name per ADR 0004. The status reported by
  `ListPlugins()` SHALL match the plugin's documented ADR-0004 lifecycle.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new requirement that `ListPlugins()` reports each plugin's real
  lifecycle status (active/idle/frozen), consistent with ADR 0004.

## Impact

- Code: `runtime/plugin.go` (`ListPlugins` status source), and either
  `core/plugin.go` (`PluginMeta` gains a lifecycle field) or a runtime
  status registry; each plugin's `Metadata()` if the field approach is chosen.
- Behavior: `ListPlugins()` output distinguishes active/idle/frozen plugins;
  no change to what is registered or callable.
