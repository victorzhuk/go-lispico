## MODIFIED Requirements

### Requirement: Bootstrap macros bind through the engine's evaluator

The stdlib bootstrap SHALL define its trusted Lisp-source macros and functions
through `core.BootstrapDefiner` on the evaluator owned by the target environment,
so definitions land where the Engine's Dialect axes place them. The capability
SHALL expose `DefineBootstrap(context.Context, string, *core.Env)
(core.Value, error)` and SHALL be implemented by both the core tree evaluator and
the runtime bytecode evaluator. The bytecode implementation SHALL delegate to its
dialect-configured tree evaluator's definition path rather than construct an
identity evaluator.

The capability SHALL use the trusted full bootstrap reader and accept exactly
one top-level proper list headed by `defn` or `defmacro`. Only that definition
dispatch MAY use the full kernel when the running Dialect removes the form;
namespace mode, truthiness, evaluation state, limits, and target environment
SHALL remain those of the installed owner. Eager loading and lazy first-touch
materialization SHALL use this same operation and publication rules.

Host-installed Go plugins are trusted and MAY discover the exported capability
through a structural interface assertion. The capability SHALL NOT be registered
as a Lisp value or Special form and SHALL NOT be reachable from evaluated Lisp
code. The system does not claim isolation among Go plugins.

If stdlib is initialized directly on an environment that has no evaluator, the
loader SHALL install the default Evaluator on that environment before defining
source. It SHALL NOT evaluate bootstrap source through an unowned temporary
Evaluator or replace an evaluator already installed by an Engine or embedder.

#### Scenario: Threading macros work under the default CL dialect

- **WHEN** `runtime.New(nil)` loads the stdlib plugin and evaluates `(-> 1 (+ 2))`
- **THEN** the result SHALL be `3`, not an `UndefinedError`

#### Scenario: All bootstrap macros resolve in head position under Lisp-2

- **WHEN** a Lisp-2 Engine evaluates each of `->`, `->>`, `as->`, `if-let`, and `when-let` in head position
- **THEN** every form SHALL resolve and evaluate without `UndefinedError`

#### Scenario: Eager and lazy definitions use the installed owner

- **WHEN** the same bootstrap name is defined during eager startup and lazy first touch with an evaluator already installed on the environment
- **THEN** both paths SHALL invoke that evaluator's bootstrap-definition operation and SHALL publish equivalent bindings

#### Scenario: Restricted Dialect does not lose trusted definitions

- **WHEN** an empty-base Dialect removes user access to `defn` and `defmacro` while stdlib bootstrap source is loaded
- **THEN** trusted definitions SHALL still be installed without making either removed Special form callable by user code

#### Scenario: Trusted reader is independent of CL reader flags

- **WHEN** bootstrap source containing vector parameter or binding syntax is loaded for a Dialect whose public reader disables brackets
- **THEN** the trusted definition SHALL load while the same bracket syntax submitted as user source remains rejected

#### Scenario: Bootstrap input is definition-only

- **WHEN** trusted bootstrap input contains multiple forms or a top-level form other than `defn` or `defmacro`
- **THEN** the operation SHALL fail with a typed error without evaluating that input

#### Scenario: Go capability is absent from Lisp

- **WHEN** evaluated code inspects and invokes every name available in a full-base or empty-base Dialect
- **THEN** no name or first-class value SHALL expose `DefineBootstrap`, while trusted host Go code MAY assert `core.BootstrapDefiner`

#### Scenario: Standalone initialization adopts its evaluator

- **WHEN** stdlib initializes directly on an environment with no evaluator
- **THEN** the environment SHALL own the default Evaluator before the first bootstrap definition is evaluated
