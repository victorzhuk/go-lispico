## ADDED Requirements

### Requirement: A Dialect enumerates its special forms

A Dialect SHALL expose the sorted list of names its resolved special-form table dispatches, reflecting its base, every form it adds or hides, and the Lisp-2 intrinsics when that axis is enabled. The returned list SHALL be owned by the caller; modifying it SHALL NOT affect the Dialect or any other caller.

#### Scenario: Full base lists the kernel forms

- **WHEN** `Forms()` is called on a full-base Dialect with no added or hidden forms
- **THEN** it SHALL return exactly the kernel form names, sorted

#### Scenario: Renames and Lisp-2 intrinsics are reflected

- **WHEN** `cl.Dialect().Forms()` is called
- **THEN** the result SHALL contain `defun`, `setq`, `progn`, `funcall` and `function`, and SHALL NOT contain `set!` or `do`

#### Scenario: Empty base lists only specified forms

- **WHEN** `Forms()` is called on an empty-base Dialect whose specification maps only `if` and `let`
- **THEN** it SHALL return `["if", "let"]`

#### Scenario: Caller mutation does not leak

- **WHEN** a caller modifies the slice returned by `clojure.Dialect().Forms()`
- **THEN** a later call SHALL return the unmodified list
