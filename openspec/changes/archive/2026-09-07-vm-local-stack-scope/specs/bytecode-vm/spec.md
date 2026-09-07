## MODIFIED Requirements

### Requirement: Kernel let binding scope parity

The VM SHALL bind kernel `let` sequentially: each binding initializer SHALL
resolve names in the child scope containing bindings introduced earlier in the
same form, matching the tree-walking evaluator. Kernel `let*` SHALL retain the
same sequential behavior. Bindings SHALL remain local to their binding form.

#### Scenario: let init sees the enclosing binding, not its sibling

- **WHEN** the VM evaluates `(def a 10) (let [b a a 1] b)`, where the sibling `a` binding occurs after `b`'s initializer
- **THEN** the result SHALL be `10`, equal to the tree-walking evaluator's result, because a later sibling is not yet visible

#### Scenario: let init sees the earlier sibling

- **WHEN** the VM evaluates `(def a 10) (let [a 1 b a] b)`
- **THEN** the result SHALL be `1`, equal to the tree-walking evaluator's result

#### Scenario: let* init sees the earlier sibling

- **WHEN** the VM evaluates `(def a 10) (let* [a 1 b a] b)`
- **THEN** the result SHALL be `1`, equal to the tree-walking evaluator's result

## ADDED Requirements

### Requirement: Lexical bindings preserve surrounding expression operands

Compiled lexical bindings SHALL preserve the callee and every previously
evaluated operand of their containing expression. Each normally completed
expression SHALL contribute exactly one result, including binding forms used
as call arguments or collection elements. Leaving a lexical scope SHALL not
change an enclosing binding or expose a discarded initializer as a result.
Repeated loop iterations SHALL not accumulate discarded binding operands.

#### Scenario: Binding expression preserves an earlier vector element

- **WHEN** `[10 (let [x 2] x)]` is evaluated under the VM with the Clojure dialect
- **THEN** the result SHALL be `[10 2]`, equal in value and type to the tree-walker result

#### Scenario: Binding expression preserves an earlier native argument

- **WHEN** `(+ 10 (let [x 2] x))` is evaluated with the standard library
- **THEN** the result SHALL be `12` under both evaluators

#### Scenario: Binding expression preserves an ordinary call target

- **WHEN** `((fn [a b] a) 10 (let [x 2] x))` is evaluated
- **THEN** the VM SHALL return `10` without replacing the function or either argument with a binding initializer

#### Scenario: Repeated lexical scopes retain bounded operand storage

- **WHEN** a fixed `loop` body creates and leaves a local binding scope on each of 10,000 iterations
- **THEN** the result SHALL match the tree-walker and live operand storage SHALL remain bounded independently of iteration count

#### Scenario: Escaping closures retain their own binding

- **WHEN** a closure captures a binding created within an operand expression and is invoked after that lexical scope exits
- **THEN** the closure SHALL retain that binding and observe its mutations without observing operands or later unrelated bindings

### Requirement: Loop bindings have a bounded lexical scope

Every `loop` initializer SHALL resolve names in the scope enclosing the loop;
sibling loop bindings SHALL not be visible during initialization. The loop body
SHALL see all loop bindings, and leaving the loop SHALL restore the enclosing
name resolution. Nested loops SHALL retain distinct binding scopes and recur
targets. Replacement arguments to `recur` SHALL be evaluated before rebinding
the loop variables, preserving existing per-iteration capture identity.

#### Scenario: Loop exit restores a shadowed outer binding

- **WHEN** `(let [x 10] (loop [x 1] x) x)` is evaluated
- **THEN** the result SHALL be `10` under both evaluators

#### Scenario: Loop binding is unavailable after exit

- **WHEN** `(do (loop [x 1] x) x)` is evaluated without an outer binding for `x`
- **THEN** both evaluators SHALL report an undefined-symbol error for the final `x`

#### Scenario: Loop initializer sees the enclosing binding

- **WHEN** `(let [a 10] (loop [a 1 b a] b))` is evaluated
- **THEN** the result SHALL be `10` under both evaluators

#### Scenario: Nested loop exit retains the outer loop binding

- **WHEN** an inner loop shadows an outer loop variable and returns before the outer body reads or recurs with that variable
- **THEN** the outer body SHALL use the outer binding and its own recur target, matching the tree-walker
