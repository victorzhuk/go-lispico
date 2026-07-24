## Context

Plugin lifecycle (active/idle/frozen) is documented in ADR 0004 but recorded
nowhere in code — `ListPlugins()` fabricates `"active"` for all. The status must
come from a real source of truth.

## Goals / Non-Goals

- Goal: `ListPlugins()` reports each plugin's true lifecycle per ADR 0004.
- Non-Goal: changing which plugins are registered, frozen, or callable.
- Non-Goal: enforcing the freeze at load time (a separate concern).

## Decisions

### Status source: a PluginMeta lifecycle field

Add a `Lifecycle` (or `Status`) field to `core.PluginMeta` with a small enum
(`active`/`idle`/`frozen`) that each plugin declares in its `Metadata()`.
`ListPlugins()` reads it through the registered plugin's `Metadata()` instead of
hardcoding.

Rationale over a runtime name→status map: the status belongs with the plugin
that owns it (single source of truth, no drift between a central map and the
plugin set), and `Metadata()` already exists and is already read by
`ListPlugins()`. A central map would re-introduce the same
doc-vs-code divergence one layer over.

Default: a plugin that does not set the field is treated as `active` (backward
compatible for third-party plugins), so only the frozen/idle first-party plugins
need to declare their state.

## Risks / Trade-offs

- Adding a `PluginMeta` field is an additive API change; existing third-party
  plugins that construct `PluginMeta` without it keep compiling and default to
  active.
- The frozen/idle first-party plugins must each set the field; a missed one
  silently reports active — cover with a test asserting the known frozen/idle set
  reports correctly.

## Migration

Additive. Third-party plugins need no change (default active). First-party
frozen/idle plugins declare their lifecycle in `Metadata()`.
