## ADDED Requirements

### Requirement: Common Lisp dialect keeps shared-core bindings for unrenamed builtins

A builtin that the Common Lisp dialect does not rename or adapt SHALL stay callable under its registered name with the binding the shared core registered, including its canonical flag. The CL vocabulary SHALL NOT contain entries that map a name to itself.

#### Scenario: Unrenamed builtin keeps its registration

- **WHEN** an Engine runs `cl.Dialect()` with the stdlib loaded, on either evaluator
- **THEN** `cons`, `list`, `reverse`, `apply` and `type` SHALL evaluate as the shared core defines them, and each binding's canonical flag SHALL equal the one the stdlib registered
