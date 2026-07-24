# runtime-api — delta

## ADDED Requirements

### Requirement: ListPlugins reports real lifecycle status

`ListPlugins()` SHALL report each registered plugin's real lifecycle status —
active, idle, or frozen — rather than a hardcoded value. The reported status
SHALL match the plugin's documented ADR-0004 lifecycle: `stdlib` and `data`
active, `fsm` idle, and `llm`/`agent`/`lio`/`net`/`exec` frozen. A plugin that
does not declare a lifecycle SHALL default to active, preserving behavior for
third-party plugins.

#### Scenario: Frozen and idle plugins are reported correctly

- **WHEN** an engine has loaded plugins spanning active, idle, and frozen lifecycles and `ListPlugins()` is called
- **THEN** each plugin's reported status SHALL match its ADR-0004 lifecycle, not a uniform "active"

#### Scenario: Third-party plugin without a declared lifecycle

- **WHEN** a plugin does not declare a lifecycle status
- **THEN** `ListPlugins()` SHALL report it as active
