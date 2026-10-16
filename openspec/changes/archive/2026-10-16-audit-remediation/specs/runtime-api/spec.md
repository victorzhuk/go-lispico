## ADDED Requirements

### Requirement: Plugin binding ownership is attributed through the registration journal

A plugin's owned bindings SHALL be attributed from the writes it made through its registration view, not from a whole-environment before-and-after difference. A name qualifies only when the live cell is the cell this registration created and still carries its recorded version. Attribution SHALL NOT materialize a lazy binding, SHALL NOT claim a name that already existed before the plugin ran, and SHALL report nothing for an ended registration.

#### Scenario: A plugin binding created through the registration view is owned

- **WHEN** a plugin writes a name through its registration view and the plugin is then unloaded
- **THEN** that name SHALL be removed from the root environment

#### Scenario: A host binding written during plugin load is not owned

- **WHEN** a host writes a root binding while the plugin's load is paused inside its own initialization, and the plugin's own initialization completes successfully, and the plugin is then unloaded
- **THEN** the plugin's own bindings SHALL be removed and the host binding SHALL remain and still evaluate

#### Scenario: A replaced cell stops being owned

- **WHEN** a name the registration created is subsequently replaced by another cell before the plugin is unloaded
- **THEN** attribution SHALL NOT report that name as owned by the registration

#### Scenario: An overwritten pre-existing name is not owned

- **WHEN** a plugin overwrites a binding that already existed in the root environment before the plugin ran
- **THEN** the pre-existing name SHALL NOT become unload-owned by that plugin

#### Scenario: Attribution does not evaluate a lazy binding

- **WHEN** attribution runs for a registration holding a lazy binding that has not been materialized
- **THEN** the binding SHALL remain unmaterialized

#### Scenario: An aborted registration owns nothing

- **WHEN** a registration is aborted or has already ended
- **THEN** attribution SHALL report no owned names

### Requirement: Engine shutdown cancels watcher work without joining it

`Engine.Stop` SHALL request cancellation of watcher work and SHALL NOT wait for an in-flight reload or for a host Go function invoked by a watched file to return. `Engine.Close` SHALL request cancellation and synchronously flush the bytecode cache. Both SHALL be idempotent. Shutdown requested from inside evaluated watcher content SHALL NOT join the goroutine running that content, and a reload observing cancellation SHALL NOT publish its child bindings.

#### Scenario: Shutdown from inside a watched GoFunc returns

- **WHEN** a watched file's host GoFunc calls `Engine.Stop` while the watcher is evaluating that file
- **THEN** the call SHALL return within the caller's deadline

#### Scenario: Close from inside a watched GoFunc returns and flushes

- **WHEN** a watched file's host GoFunc calls `Engine.Close` while the watcher is evaluating that file
- **THEN** the call SHALL return within the caller's deadline and the bytecode cache SHALL be flushed

#### Scenario: Shutdown under each evaluator mode returns

- **WHEN** the shutdown-from-content scenario runs once with the bytecode evaluator and once with the tree-walker evaluator, for both `Stop` and `Close`
- **THEN** every case SHALL return within the caller's deadline

#### Scenario: Shutdown without content that shuts down returns

- **WHEN** the same watch scenario runs with a file whose forms do not call shutdown
- **THEN** `Stop` SHALL return no error and the scenario SHALL complete

#### Scenario: A cancelled reload publishes nothing

- **WHEN** a reload observes cancellation after processing at least one form and before the remaining forms
- **THEN** the remaining forms SHALL NOT be executed and the child environment's bindings SHALL NOT be published

#### Scenario: Cancellation before the first scan starts the scan

- **WHEN** cancellation is requested before a watcher performs its initial scan
- **THEN** the initial scan SHALL NOT run

#### Scenario: Repeated shutdown is idempotent

- **WHEN** `Stop` or `Close` is called more than once on one engine
- **THEN** every call SHALL return without an error and without waiting for watcher completion