# runtime-api — delta

## ADDED Requirements

### Requirement: Function redefinitions survive plugin loading

Loading a plugin with `Use()` SHALL NOT revert a user redefinition of an
existing binding. Under a Lisp-2 dialect the engine bridges value-cell
`GoFunc`s into the function cell so they are callable in head position; this
bridge SHALL NOT overwrite a function-cell binding that differs from the
value-cell `GoFunc` it would install. A binding first established by the bridge,
or absent, MAY be (re)bridged; a binding a program has redefined SHALL be left
untouched. The guarantee holds regardless of how many plugins are loaded and in
what order, preserving the deterministic-evaluation contract.

#### Scenario: Operator redefinition survives an unrelated Use

- **WHEN** a program redefines a canonical operator with `defun` and a later, unrelated `Use()` loads another plugin
- **THEN** the operator SHALL keep the redefined behavior, and no error or silent revert SHALL occur

#### Scenario: Non-operator builtin redefinition survives

- **WHEN** a program redefines a non-operator builtin (for example `map`) with `defun` and a later `Use()` loads another plugin
- **THEN** the redefinition SHALL persist

#### Scenario: Newly loaded plugin functions are still callable in head position

- **WHEN** a plugin is loaded and its functions have not been redefined by the program
- **THEN** those functions SHALL be callable in head position, and un-redefined canonical operators SHALL still take the VM native-op fast path

#### Scenario: Reloading the owning plugin resets its bindings

- **WHEN** the plugin that owns a binding is reloaded with `ReloadPlugin`
- **THEN** that binding MAY be restored to the plugin's definition, since reloading the owning plugin is the sanctioned reset path
