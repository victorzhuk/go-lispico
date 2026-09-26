# dialect Specification

## Purpose
Define per-engine Lisp dialect selection, reader syntax, symbol namespaces and
builtin vocabulary while preserving shared evaluation semantics and isolation.

## Requirements

### Requirement: Dialect selection is fixed at Engine construction
An Engine SHALL run exactly one Dialect, selected via a construction-time option, and that Dialect SHALL be immutable for the Engine's lifetime. Evaluated code SHALL NOT be able to change the running Dialect.

#### Scenario: Dialect chosen at construction
- **WHEN** an Engine is created with a Dialect option
- **THEN** every evaluation on that Engine SHALL dispatch special forms through that Dialect's effective table

#### Scenario: Evaluated code cannot change the Dialect
- **WHEN** evaluated source attempts to alter which special forms are available
- **THEN** the effective table SHALL remain the one resolved at construction

### Requirement: A Dialect is a Delta over a declared base

A Dialect SHALL be defined as a delta over a declared base: the full kernel table or an empty table. The delta SHALL be expressed as a specification of forms added under visible names and kernel names hidden from the base, and resolving it SHALL yield one effective name→form table. Rename SHALL be expressed as adding the canonical form under the new name and hiding the canonical name.

#### Scenario: Rename resolves to the canonical form

- **WHEN** a Dialect renames a canonical Kernel form to another name
- **THEN** invoking the renamed name SHALL evaluate the canonical form
- **AND** the original canonical name SHALL NOT resolve unless the Delta also keeps it

#### Scenario: Removal makes a form uncallable

- **WHEN** a Dialect removes a form from its base
- **THEN** invoking that form SHALL fail as undefined

#### Scenario: Rename through the specification

- **WHEN** a full-base specification maps `setq` to `set!` and hides `set!`
- **THEN** `setq` SHALL dispatch the `set!` kernel form and `set!` SHALL be undefined

#### Scenario: Empty base exposes only specified forms

- **WHEN** an empty-base specification maps only `if` and `let`
- **THEN** only `if` and `let` SHALL dispatch as special forms

### Requirement: Empty-base Dialects are fail-closed
A Dialect built on the empty base SHALL expose only the special forms its Delta explicitly adds. A special form added to the Kernel table by a later change SHALL NOT become callable in an empty-base Dialect unless its Delta adds it.

#### Scenario: Unlisted kernel form is rejected
- **WHEN** an empty-base Dialect omits a kernel special form from its Delta
- **THEN** invoking that form under the Dialect SHALL fail as undefined

### Requirement: Per-Engine dispatch isolation
Two Engines running different Dialects in one process SHALL NOT share special-form dispatch state.

#### Scenario: Divergent Dialects on concurrent Engines
- **WHEN** two Engines are constructed with different Dialects
- **THEN** a form present in one Dialect and absent in the other SHALL resolve only on the Engine whose Dialect defines it

### Requirement: Default Engine behavior is preserved
An Engine created without a Dialect option SHALL evaluate the identity Dialect, reproducing the special-form behavior of the interpreter prior to this change.

#### Scenario: No option selects the identity Dialect
- **WHEN** an Engine is created with no Dialect option
- **THEN** the special forms `if`, `def`, `defn`, `let`, `quote`, `cond`, `loop`, and `recur` SHALL behave as they did before this change

### Requirement: Symbol namespaces are a Dialect axis
A Dialect SHALL set the namespace axis to Lisp-1 (a single binding namespace) or Lisp-2 (a separate function cell). Under Lisp-2 a symbol MAY name a function and a value simultaneously; under Lisp-1 a symbol names one binding.

#### Scenario: Lisp-2 resolves head and argument positions separately
- **WHEN** an Engine runs a Lisp-2 Dialect and a symbol is bound as both a value and a function
- **THEN** the symbol in head position SHALL resolve to its function binding
- **AND** the same symbol in argument position SHALL resolve to its value binding

#### Scenario: Lisp-1 resolves both positions from one namespace
- **WHEN** an Engine runs a Lisp-1 Dialect
- **THEN** a symbol SHALL resolve to the same binding in head and argument position

### Requirement: funcall and function-reference are Lisp-2 forms
Under a Lisp-2 Dialect, `funcall` SHALL apply a function value taken from value position, and `#'name` SHALL yield the function-cell binding of `name`. These forms SHALL be absent under a Lisp-1 Dialect.

#### Scenario: funcall and #' apply the function cell under Lisp-2
- **WHEN** an Engine runs a Lisp-2 Dialect with `f` bound in the function cell
- **THEN** `(funcall #'f args...)` SHALL apply that function to the arguments

#### Scenario: funcall and #' are undefined under Lisp-1
- **WHEN** an Engine runs the default Lisp-1 Dialect
- **THEN** referencing `funcall` or `#'` SHALL fail as undefined

### Requirement: Identity Dialect namespace is unchanged
The identity Dialect SHALL be Lisp-1, so an Engine created without selecting the axis resolves symbols exactly as before this change.

#### Scenario: Default Engine keeps single-namespace resolution
- **WHEN** an Engine is created without changing the namespace axis
- **THEN** head and argument symbols SHALL resolve from the single binding namespace as before

### Requirement: Reader syntax varies by Dialect flags
A Dialect SHALL carry reader feature flags controlling: `[..]` and `{..}` literal syntax, `#'` function-reference syntax, and `#(...)` vector syntax. The reader SHALL honor the running Dialect's flags when tokenizing and parsing source.

#### Scenario: Function-reference syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#'` enabled
- **THEN** `#'foo` SHALL read as a function-reference form
- **AND** under a Dialect with `#'` disabled, `#'foo` SHALL NOT read as a function-reference form

#### Scenario: Reader-vector syntax gated by flag
- **WHEN** an Engine runs a Dialect with `#(...)` enabled
- **THEN** `#(...)` SHALL read as a vector form

#### Scenario: Bracket literals gated by flag
- **WHEN** an Engine runs a Dialect with `[..]` literals disabled
- **THEN** `[1 2]` SHALL NOT read as a vector literal
- **AND** under a Dialect with `[..]` literals enabled, `[1 2]` SHALL read as a vector literal

### Requirement: Identity Dialect reader flags are unchanged
The identity Dialect SHALL enable `[..]`/`{..}` literals and disable `#'` and `#(...)`, so an Engine created without changing reader flags parses source exactly as before this change.

#### Scenario: Default Engine parsing is preserved
- **WHEN** an Engine is created without changing reader flags
- **THEN** `[..]` and `{..}` literals SHALL parse as before, and `#'`/`#(...)` SHALL NOT be special

### Requirement: Vocabulary is a name map over shared implementations
A Dialect SHALL present builtins under dialect-specific names via a vocabulary map from a visible name to a shared builtin implementation. A Dialect SHALL NOT carry its own copy of an implementation that the shared core already provides.

#### Scenario: Renamed builtin resolves to the shared implementation
- **WHEN** an Engine runs a Dialect mapping `car` to the shared first-implementation
- **THEN** `(car '(1 2 3))` SHALL evaluate to `1` using that shared implementation

#### Scenario: Semantics-differing name uses an adapter over the shared core
- **WHEN** a Dialect maps a name whose semantics differ from the shared core by argument order or arity
- **THEN** the name SHALL resolve through a thin adapter over the shared implementation, not a duplicated implementation

### Requirement: Empty-base vocabulary is fail-closed
An empty-base Dialect's vocabulary SHALL be an allowlist. A builtin whose name is absent from the Dialect's vocabulary map SHALL be uncallable, and a builtin added to the shared core by a later change SHALL NOT become callable unless the map adds it.

#### Scenario: Unlisted builtin is rejected
- **WHEN** an empty-base Dialect omits a builtin from its vocabulary map
- **THEN** invoking that builtin under the Dialect SHALL fail as undefined

### Requirement: Identity Dialect vocabulary is unchanged
The identity Dialect SHALL map today's builtin names onto today's implementations, so an Engine created without changing vocabulary resolves builtins exactly as before this change.

#### Scenario: Default Engine vocabulary is preserved
- **WHEN** an Engine is created without changing vocabulary
- **THEN** the builtins registered by loaded plugins SHALL be callable under their current names

### Requirement: Common Lisp dialect

The system SHALL provide a Common Lisp dialect composed of: a CL vocabulary over
the shared builtin core (`defun`, `setq`, `progn`, `car`, `cdr`, `funcall`, and
related), the Lisp-2 namespace axis, and CL reader flags (`#'` and `#(...)`
enabled, `[..]`/`{..}` literals disabled). Its truthiness SHALL be the uniform
truthiness (`nil` and `false` falsy), like every other Dialect.

A CL collection name whose argument order or arity differs from its shared
canonical Builtin SHALL resolve through a thin adapter over shared collection
kernels. `nth` SHALL accept a non-negative index followed by a list and SHALL
return `nil` beyond the list; `nil` SHALL be accepted as an empty list. `mapcar`
SHALL accept a function and one or more lists (including `nil` as an empty list),
apply the function to aligned elements, stop at the shortest list, and return a
List.

CL `sort` SHALL accept exactly a list, vector, or `nil` followed by a predicate
and either no options or one `:key` value. `:key nil` SHALL select identity.
Missing required arguments, dangling options, and extra positional arguments
SHALL be `ArityError`; unknown or duplicate keywords SHALL be `EvalError`; an
unsupported sequence SHALL be `TypeError`. The adapter SHALL project each
element exactly once in original order before invoking any predicate, apply the
predicate to projected keys using the active Dialect's generalized truthiness,
and stop all later callbacks after the first callback error. Sorting SHALL be
stable and SHALL NOT mutate the input. A List SHALL produce a List, a Vector a
Vector, and `nil` an empty List. The first callback error SHALL be preserved
unless the mandatory work-budget flush discovers a Terminal error, in which case
the shared Terminal precedence rule applies.

Adapter/kernel phases that do not re-enter evaluation, including input copying,
tuple alignment, key/result storage, and comparator scheduling, SHALL use the
shared Builtin work budget. Callback re-entry SHALL remain the sole owner of
callback execution charges. Canonical Clojure-style names SHALL retain their own
argument shapes and natural-sort behavior.

#### Scenario: CL surface forms evaluate

- **WHEN** an Engine runs the Common Lisp Dialect
- **THEN** `defun` SHALL define a function, `(if false :y :n)` SHALL evaluate to `:n`, and `(funcall #'f args...)` SHALL apply `f`

#### Scenario: CL reader affordances parse

- **WHEN** an Engine runs the Common Lisp Dialect
- **THEN** `#'f` and `#(...)` SHALL parse, and `[1 2]` SHALL NOT read as a vector literal

#### Scenario: CL nth adapts order and absence

- **WHEN** `(nth 1 '(a b c))` and `(nth 9 '(a))` are evaluated under the Common Lisp Dialect
- **THEN** the results SHALL be `b` and `nil`, respectively

#### Scenario: CL mapcar maps aligned tuples

- **WHEN** `(mapcar #'+ '(1 2) '(10 20 30))` is evaluated under the Common Lisp Dialect
- **THEN** the result SHALL be `(11 22)` and the function SHALL be invoked exactly twice

#### Scenario: CL sort uses predicate and key adapters

- **WHEN** CL `sort` receives a supported sequence, predicate, and optional `:key` function
- **THEN** it SHALL project each element once, order stably by generalized predicate truthiness, return the specified result type, and leave the input unchanged

#### Scenario: CL sort rejects malformed options deterministically

- **WHEN** CL `sort` receives a missing/dangling/extra argument, unknown keyword, or duplicate `:key`
- **THEN** it SHALL return `ArityError` for argument-shape failures and `EvalError` for unknown or duplicate keywords before invoking callbacks

#### Scenario: CL sort stops at the first callback error

- **WHEN** a key or predicate callback returns a typed or Terminal error
- **THEN** `sort` SHALL return that error unchanged and SHALL NOT invoke any later Lisp callback

#### Scenario: Canonical collection names are unchanged

- **WHEN** canonical `nth`, `map`, and natural `sort` are evaluated under a Dialect that exposes them directly
- **THEN** their argument shapes, results, and typed errors SHALL remain unchanged

#### Scenario: Adapters share implementation kernels

- **WHEN** CL adapter names and canonical names perform corresponding indexing, mapping, or sorting work
- **THEN** they SHALL use one shared kernel per operation family rather than duplicate algorithms

### Requirement: Clojure dialect preserves the prior surface
The system SHALL provide a Clojure dialect reproducing the interpreter's behavior prior to the default flip: Lisp-1, `nil`+`false` truthiness, current vocabulary, and bracket literals enabled.

#### Scenario: Clojure dialect matches the old default
- **WHEN** an Engine runs the Clojure dialect
- **THEN** conditionals SHALL treat `false` as falsy, symbols SHALL resolve from a single namespace, and `[..]`/`{..}` literals SHALL parse as before this change

#### Scenario: Clojure dialect is identity-compatible with the bytecode VM
- **WHEN** an Engine is constructed with `New(nil, WithBytecode(), WithDialect(clojure.Dialect()))`
- **THEN** the construction SHALL succeed and the Engine SHALL run the bytecode evaluator; the `clojure.Dialect()` value SHALL report `IsIdentity() == true`

### Requirement: Dialect renames normalize to canonical kernel forms

A resolved Dialect SHALL expose the mapping from its visible form names to
canonical kernel forms, and compilation SHALL normalize source through that
mapping so the compiler and VM operate only on canonical names. Removed forms
SHALL stay absent — normalization never resurrects a form the Dialect excludes.
The resolved special-form table SHALL be the only source of special-form
dispatch for the evaluator, macro expansion and the compiler. A name absent from
that table SHALL be treated as an ordinary symbol on every execution path, with
identical results and error codes.

#### Scenario: Renamed form compiles to the canonical form

- **WHEN** a Dialect renames `do` to `progn` and `(progn 1 2)` is compiled
- **THEN** the emitted chunk SHALL be equivalent to compiling `(do 1 2)` under the identity Dialect

#### Scenario: Removed form stays removed

- **WHEN** a fail-closed Dialect excludes `set!` and source calls `set!` without binding it
- **THEN** evaluation SHALL fail with `UndefinedError` on both execution paths, and SHALL NOT silently normalize to the kernel form

#### Scenario: Renamed-away name bound by the user

- **WHEN** under the CL dialect a program evaluates `(defun do (x) (* x 2))` and then `(do 5)`
- **THEN** the result SHALL be `10` under both the evaluator and the VM

### Requirement: Form-shape rules are Dialect-owned

A Dialect MAY define a Form-shape rule for a special form: a normalizer that
produces the form's canonical argument structure at special-form dispatch. One
normalizer SHALL serve both the Evaluator and the Compiler, so the two execution
paths cannot parse the same form differently. Normalization SHALL NOT rewrite
Reader output or stored data: quoted and quasiquoted forms pass through unchanged.
The first Form-shape rule is `cond` clause shape: the Clojure dialect accepts flat
test/expression pairs, the Common Lisp dialect retains nested clauses, and a
canonical clause is one test plus a body sequence evaluated as kernel `do`. The
body SHALL evaluate as kernel `do` regardless of whether the Dialect exposes
`do` under any name. A form that does not match its dialect's shape SHALL
produce a typed error, never a panic.

#### Scenario: Clojure flat cond

- **WHEN** a Clojure-dialect Engine evaluates `(cond (< x 0) :neg (> x 0) :pos :else :zero)`
- **THEN** the flat pairs SHALL evaluate as clauses with the same result under the Evaluator and the VM

#### Scenario: CL nested cond with implicit progn

- **WHEN** a CL-dialect Engine evaluates a `cond` clause whose body holds multiple expressions
- **THEN** the body SHALL evaluate in order as if wrapped in kernel `do`, returning the last expression's value, identically under both execution paths

#### Scenario: Multi-expression body without an exposed do

- **WHEN** a Dialect that removes `do`, or an empty-base Dialect that adds `if` and `cond` only, evaluates `(cond (true 1 2))`
- **THEN** the result SHALL be `2` under both execution paths

#### Scenario: Quoted cond data is untouched

- **WHEN** a program evaluates `(quote (cond ...))` or embeds a `cond` form in quasiquoted data
- **THEN** the resulting data SHALL be structurally identical to the source, with no normalization applied

#### Scenario: Malformed clause shape is a typed error

- **WHEN** a `cond` form violates its dialect's clause shape (odd flat pair, non-list nested clause)
- **THEN** evaluation SHALL return a typed error under both execution paths, never a panic

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

### Requirement: Stock dialect construction is memoized and immutable

The stock dialect constructors (`cl.Dialect()`, `clojure.Dialect()`) SHALL
return a process-shared value built once from a static specification by the
validated constructor: repeated calls SHALL NOT rebuild the vocabulary map,
the resolved special-form table, or the dialect fingerprint. All shared state
SHALL be immutable after construction; nothing observable through a `Dialect`
value SHALL change over the process lifetime. `Fingerprint()` SHALL equal the
fingerprint of an independently constructed equal specification. Per-engine
dispatch isolation SHALL be preserved: engine-level definition or redefinition
of operators SHALL affect only that engine's environment, never the shared
resolution state, and two engines constructed from one shared stock dialect
SHALL behave as two engines constructed from independently built equal
dialects.

#### Scenario: Repeated construction shares one resolution

- **WHEN** two engines are constructed with `clojure.Dialect()` in one process
- **THEN** dialect resolution work SHALL be performed once, and both engines SHALL evaluate the dialect test corpus with results identical to independently constructed dialects

#### Scenario: Redefinition on one engine does not leak through the shared dialect

- **WHEN** one engine redefines an operator name that the shared dialect resolves
- **THEN** the other engine's evaluation of that name SHALL be unaffected

#### Scenario: Fingerprint is stable under memoization

- **WHEN** `Fingerprint()` is read from a stock dialect and from a dialect independently constructed from an equal specification
- **THEN** the digests SHALL be equal

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

### Requirement: Dialect adapters have semantic fingerprint identity

Every Dialect adapter SHALL have a non-empty stable semantic ID/version supplied
in the specification's adapter entry and stored in its `VocabEntry`. The Dialect
fingerprint SHALL include that ID with the visible/canonical names and SHALL
change when the ID/configuration changes. It SHALL NOT use a function pointer or
only the adapter's Go concrete type as semantic identity. Dialect construction
SHALL reject an adapter entry with an empty ID.

#### Scenario: Adapter semantics participate in the fingerprint

- **WHEN** two otherwise identical Dialects bind an adapter under the same visible name with different semantic IDs or versions
- **THEN** their fingerprints SHALL differ, while repeated construction with the same stable ID SHALL produce the same fingerprint

#### Scenario: Empty adapter identity fails closed

- **WHEN** a specification declares an adapter with an empty semantic ID
- **THEN** construction SHALL return an error rather than a Dialect with an ambiguous fingerprint

### Requirement: Dialect adapter failures are typed by violated contract

Every validation failure originated locally by an active Dialect adapter SHALL
be recoverable through `errors.As` as a `*core.LispicoError`. Wrong argument
shape SHALL carry `Code: "ArityError"`, a value of the wrong runtime type SHALL
carry `Code: "TypeError"`, and a correctly typed value or keyword option outside
the adapter's accepted domain SHALL carry `Code: "EvalError"` unless a more
specific existing code governs it.

Errors received from shared kernels, evaluator callbacks, Builtin work-budget
flushes, and resource helpers SHALL retain their original type and code. Terminal
errors SHALL remain Terminal. The completeness check SHALL cover CL `nth`,
`mapcar`, and `sort` adapter bodies plus their factories and transitive helpers.

#### Scenario: CL adapter validation is classifiable

- **WHEN** CL `nth`, `mapcar`, or `sort` rejects argument shape, runtime type, or option domain locally
- **THEN** its error SHALL carry `ArityError`, `TypeError`, or `EvalError` according to the violated contract

#### Scenario: CL adapter callback errors pass through

- **WHEN** a CL adapter receives a typed or Terminal error from its shared kernel, key function, predicate, or mapped callback
- **THEN** it SHALL return that error without changing its code or Terminal status

### Requirement: Dialect accessors never expose mutable state

No exported accessor of a Dialect SHALL return a map, slice or pointer through which a caller can change the Dialect or any other value sharing its state. Modifying a value returned by an accessor SHALL NOT change the Dialect's behavior, its fingerprint, or any engine built from it.

#### Scenario: Mutating the returned vocabulary does not leak

- **WHEN** a caller writes into the map returned by `cl.Dialect().Vocab()`
- **THEN** a later `cl.Dialect().Vocab()` SHALL return the original entries and an engine built afterwards SHALL resolve `car` to `first`

#### Scenario: Policy allowlist cannot be widened after construction

- **WHEN** a caller adds a name to the map returned by an empty-base Dialect's `Vocab()`
- **THEN** an engine built from that Dialect SHALL NOT make the added name callable

### Requirement: Dialect fingerprint is a process-local, collision-free identity

`Fingerprint()` SHALL encode every input unambiguously, so Dialects that differ in any fingerprinted field SHALL produce different digests regardless of the characters their names contain. It SHALL identify a Dialect within one process running one go-lispico version and SHALL NOT be a persistence or interchange format: its digest MAY change between releases.

#### Scenario: Separator characters do not collide

- **WHEN** one Dialect maps `a:b` to `c` and another maps `a` to `b:c`
- **THEN** their fingerprints SHALL differ

#### Scenario: Structurally identical Dialects agree

- **WHEN** two structurally identical Dialects are built in one process
- **THEN** their fingerprints SHALL be equal

### Requirement: Dialects are constructed from a validated specification

The system SHALL construct a Dialect from a plain-data specification in one step that validates, resolves and fingerprints it, returning either a frozen Dialect or an error together with the zero Dialect. Construction SHALL reject: a form mapping to an unknown kernel form, a hidden name absent from the base, a name both hidden and mapped as a visible form, a Lisp-2 specification that maps `funcall` or `function` as a visible form, an adapter without a semantic ID, an adapter without a value, and a name present both as a vocabulary rename and as an adapter. The returned Dialect SHALL NOT observe later mutation of the specification's maps or slices. The fingerprint SHALL be a function of the resolved configuration only, so two specifications with identical base, dispatch, axes, vocabulary (including whether one is configured) and adapter IDs SHALL produce equal fingerprints. The zero-value Dialect SHALL behave as the identity Dialect: the full kernel with no delta, the Lisp-1 namespace and no vocabulary.

#### Scenario: Invalid specification fails at construction

- **WHEN** a specification maps a visible name to a kernel form that does not exist
- **THEN** construction SHALL return an error and the zero Dialect

#### Scenario: Conflicting vocabulary and adapter entries are rejected

- **WHEN** a specification names `nth` both in its vocabulary map and in its adapters
- **THEN** construction SHALL return an error

#### Scenario: Hidden and mapped name is rejected

- **WHEN** a specification hides a name and also maps the same name as a visible form
- **THEN** construction SHALL return an error

#### Scenario: Lisp-2 reserved form names are rejected

- **WHEN** a Lisp-2 specification maps `funcall` or `function` as a visible form
- **THEN** construction SHALL return an error

#### Scenario: Adapter without a value is rejected

- **WHEN** a specification declares an adapter with a semantic ID and no value
- **THEN** construction SHALL return an error

#### Scenario: Specification mutation does not leak

- **WHEN** the caller modifies the specification's vocabulary map after construction
- **THEN** the Dialect's vocabulary and fingerprint SHALL be unchanged

#### Scenario: Zero value is the identity Dialect

- **WHEN** an Engine is constructed with `WithDialect(core.Dialect{})`
- **THEN** it SHALL dispatch every kernel form under its kernel name and resolve builtins under their registered names

#### Scenario: Equal dispatch yields equal fingerprints

- **WHEN** two specifications express the same base, resolved form table, axes, vocabulary and adapter IDs through different but equivalent entries
- **THEN** their fingerprints SHALL be equal

#### Scenario: Configured empty vocabulary differs from no vocabulary

- **WHEN** two empty-base specifications differ only in that one configures an empty vocabulary and the other configures none
- **THEN** their fingerprints SHALL differ

### Requirement: Lisp-2 dialects bridge every plugin builtin

Under a Lisp-2 Dialect, every GoFunc a plugin registers and the Dialect exposes SHALL be callable in head position, whether or not the Dialect has a vocabulary, and whether the builtin was registered eagerly or through a lazy template.

#### Scenario: Lisp-2 without vocabulary

- **WHEN** an Engine runs `core.NewDialect(core.DialectSpec{Lisp2: true})` with the stdlib and json plugins loaded
- **THEN** `(json/encode 1)` and `(+ 1 2)` SHALL both evaluate successfully on both execution paths

### Requirement: Host bindings survive plugin vocabulary passes

A binding the host creates through the Engine SHALL NOT be removed or replaced by the vocabulary or allowlist processing of a later plugin load or reload.

#### Scenario: Empty-base allowlist keeps a host binding

- **WHEN** an empty-base Dialect Engine binds `host/f` through `Bind` and then loads another plugin
- **THEN** `(host/f)` SHALL still evaluate to the host function's result

### Requirement: Vocabulary registration is one rule set on every path

Vocabulary renames, adapters, the empty-base allowlist and the Lisp-2 function-cell mirror SHALL be decided by one rule set shared by eager and lazy registration, so the same Dialect and plugins produce the same bindings, canonical flags and function cells on either path. Loading a plugin SHALL process only the names that operation registers.

#### Scenario: Eager and lazy agree

- **WHEN** two Engines with the same Dialect load the stdlib, one eagerly and one through lazy templates
- **THEN** every exposed name SHALL have the same value-cell binding, canonical flag and function-cell binding after materialization

#### Scenario: Loading a plugin does not rewrite unrelated bindings

- **WHEN** a CL Engine with the stdlib loaded then loads json
- **THEN** no binding registered by the stdlib SHALL be rewritten or journaled by the json operation

### Requirement: Special-form shape errors are typed on both paths

A malformed `function` or `funcall` form SHALL produce the same typed error code under the compiler as under the evaluator.

#### Scenario: function with two arguments

- **WHEN** a Lisp-2 Dialect evaluates `(function a b)` on each execution path
- **THEN** both SHALL return a `*LispicoError` with the same code
