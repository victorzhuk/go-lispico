# dialect — delta

## ADDED Requirements

### Requirement: Kernel surface is Clojure-aligned

The kernel special-form table SHALL follow Clojure semantics on four points,
identically on the tree-walker and the bytecode VM: `let` SHALL bind
sequentially — each init expression sees the bindings established before it in
the same form; `unless` SHALL NOT be a special form — `(unless ...)` resolves
as an ordinary call and fails as an unresolved symbol under every dialect; a
`cond` else clause SHALL be marked by the `:else` keyword only — a clause
headed by the bare symbol `else` is an ordinary test expression; `catch`
SHALL bind the originally thrown value, and only errors that did not come
from `throw` SHALL bind their message string. `let*` SHALL remain registered
with the same sequential semantics as `let`.

#### Scenario: let binds sequentially

- **WHEN** an Engine evaluates `(let [x 1 y x] y)` with an outer `x` bound to `99`
- **THEN** the result SHALL be `1` on both execution paths

#### Scenario: unless is not a special form

- **WHEN** an Engine evaluates `(unless false 1)` under any dialect
- **THEN** evaluation SHALL fail as an unresolved symbol on both execution paths

#### Scenario: cond else is the :else keyword

- **WHEN** an Engine evaluates `(cond (false :a) (:else :b))`
- **THEN** the result SHALL be `:b`, and a clause headed by the bare symbol `else` SHALL NOT act as an else clause on either execution path

#### Scenario: catch binds the thrown value

- **WHEN** an Engine evaluates `(try (throw {:code :denied}) (catch e (get e :code)))`
- **THEN** the result SHALL be `:denied` on both execution paths

#### Scenario: Non-thrown errors bind their message

- **WHEN** a primitive returns an engine error that is caught by `catch`
- **THEN** the catch binding SHALL be the error's message string, as before this change

## MODIFIED Requirements

### Requirement: Truthiness is uniform: nil and false are falsy

Truthiness SHALL be uniform across all dialects: `nil` and the concrete boolean
`false` SHALL be falsy, and every other value SHALL be truthy. A concrete
`Bool{false}` — whether written as the `false` literal or returned by a
comparison or predicate builtin — SHALL be falsy under every dialect. All
conditional special forms — `if`, `when`, `cond`, `and`, `or`, `not`
— SHALL determine truthiness this way on both the tree-walker and the bytecode
VM. Dialects SHALL NOT vary truthiness; there is no truthiness axis.

#### Scenario: Predicate-driven conditional takes the correct branch

- **WHEN** any dialect evaluates `(if (= 1 2) :then :else)`
- **THEN** it SHALL evaluate to `:else`

#### Scenario: Literal false is falsy

- **WHEN** any dialect evaluates `(if false :then :else)`
- **THEN** it SHALL evaluate to `:else`

#### Scenario: Axis applies across all conditional forms

- **WHEN** any dialect evaluates `when`, `cond`, `and`, `or`, and `not` against a `false` value
- **THEN** each SHALL treat `false` as falsy, consistently with `if`, on both evaluators
