# runtime-api — delta

## ADDED Requirements

### Requirement: UnloadPlugin removes the plugin's bindings

`UnloadPlugin` SHALL delete every binding the plugin registered into the root
environment, in addition to unregistering it from the registry. `ReloadPlugin`
SHALL clear the old bindings before re-running `Init`.

#### Scenario: Unloaded function becomes undefined

- **WHEN** a plugin registering `json/encode` is loaded, then `UnloadPlugin` is called for it, then `(json/encode "hi")` is evaluated
- **THEN** evaluation SHALL fail with an `UndefinedError`

#### Scenario: Reload does not stack stale bindings

- **WHEN** `ReloadPlugin` is called for a loaded plugin
- **THEN** the environment SHALL contain exactly the bindings from the fresh `Init`, with no leftovers from the previous load

### Requirement: REPL input balancing ignores comments

The REPL's continuation check SHALL treat `;` to end of line as a comment, per the
reader's comment rule, when deciding whether input is a complete form.

#### Scenario: Trailing comment with unbalanced paren

- **WHEN** the REPL receives the line `(+ 1 2) ; note (`
- **THEN** it SHALL evaluate the form and print `3` instead of waiting for a closing paren
