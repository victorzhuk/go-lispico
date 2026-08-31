## ADDED Requirements

### Requirement: Trusted hosts can define bootstrap source through the owning evaluator

The core SHALL expose `BootstrapDefiner` with
`DefineBootstrap(context.Context, string, *Env) (Value, error)`. Both the core
tree evaluator and runtime bytecode evaluator SHALL implement the capability;
the bytecode implementation SHALL delegate to its dialect-configured tree
evaluator rather than create an identity evaluator.

The operation SHALL parse with the trusted full bootstrap reader and accept
exactly one top-level proper list headed by `defn` or `defmacro`. Only dispatch of
that top-level definition MAY use the full kernel. Namespace mode, truthiness,
evaluation state, resource limits, and target environment SHALL remain those of
the owning evaluator. Read/evaluation failures SHALL remain typed.

Host-installed Go plugins are trusted and MAY discover this exported structural
interface. The capability SHALL NOT be registered as a Lisp value or Special
form and SHALL NOT be reachable from evaluated Lisp code; no isolation is claimed
among Go plugins.

#### Scenario: Both execution evaluators implement the capability

- **WHEN** a host inspects the core tree evaluator or runtime bytecode evaluator installed on an environment
- **THEN** each SHALL satisfy `core.BootstrapDefiner`, and the bytecode path SHALL retain its dialect-configured tree owner

#### Scenario: Trusted definition grammar fails closed

- **WHEN** bootstrap input contains multiple forms or a top-level form other than `defn` or `defmacro`
- **THEN** `DefineBootstrap` SHALL return a typed error without evaluating the input

#### Scenario: Lisp cannot invoke the host capability

- **WHEN** code executes under a full-base or empty-base Dialect
- **THEN** no Lisp name, Special form, reflected object, or first-class value SHALL expose `DefineBootstrap`
