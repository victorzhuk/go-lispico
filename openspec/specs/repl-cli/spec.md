# repl-cli Specification

## Purpose
TBD - created by archiving change repl-binary. Update Purpose after archive.
## Requirements
### Requirement: Interactive REPL binary

`cmd/lispico` SHALL provide an interactive REPL on a terminal: raw-mode line
editing (cursor movement, backspace/delete, kill-to-end, history recall with
arrows), a continuation prompt while input is unbalanced per the reader's balance
rule (comment- and string-aware), Ctrl-C discarding the current input without
exiting, and Ctrl-D (on an empty line) exiting cleanly. When stdin is not a
terminal it SHALL fall back to plain line-buffered reading so piped input works.

#### Scenario: Multiline form

- **WHEN** the user enters `(defn f (x)` then `(+ x 1))` on the continuation prompt
- **THEN** the completed form SHALL evaluate and print the result

#### Scenario: Ctrl-C cancels input, not the session

- **WHEN** the user types a partial form and presses Ctrl-C
- **THEN** the input SHALL be discarded, a fresh prompt shown, and the process keeps running

#### Scenario: Piped input

- **WHEN** `echo '(+ 1 2)' | lispico` runs
- **THEN** the output SHALL contain `3` and the process SHALL exit 0

### Requirement: Persistent history

The REPL SHALL persist input history across sessions to a state file under the
user's state directory, recall it with arrow keys, and never fail startup because
the history file is missing or unwritable.

#### Scenario: History survives restart

- **WHEN** a form is entered, the REPL exits, and a new session starts
- **THEN** arrow-up SHALL recall that form

#### Scenario: Unwritable history is non-fatal

- **WHEN** the history path is not writable
- **THEN** the REPL SHALL run normally without persistence and without erroring at startup

### Requirement: Dialect and evaluator selection

The binary SHALL accept `-dialect` with values `cl` (default) and `clojure`, and
`-bytecode` to opt into the bytecode evaluator, mapping directly onto
`runtime.WithDialect` and `runtime.WithBytecode`. An unknown dialect SHALL fail
with a clear error listing valid values.

#### Scenario: Clojure dialect selected

- **WHEN** `lispico -dialect clojure` evaluates `(do 1 2)`
- **THEN** the result SHALL be `2`

#### Scenario: Unknown dialect

- **WHEN** `lispico -dialect scheme` starts
- **THEN** it SHALL exit non-zero naming the valid dialects

### Requirement: File execution

Positional arguments SHALL be evaluated as source files in order, then the process
exits with 0 on success or non-zero with the error (including position when
available) on the first failure; the REPL starts only when no files are given.

#### Scenario: Run a file

- **WHEN** `lispico prog.lisp` runs a valid program
- **THEN** the program SHALL evaluate and the process exits 0

#### Scenario: Error reports position

- **WHEN** a file fails to parse on line 3
- **THEN** stderr SHALL include the file name and line 3, and the exit code SHALL be non-zero

