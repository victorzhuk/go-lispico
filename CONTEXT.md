# go-lispico

Ubiquitous language for go-lispico, an embeddable Lisp interpreter for Go. This
glossary fixes the meaning of terms that were used ambiguously across the code
and docs, so that "evaluator", "plugin", and "sandbox" mean one thing.

## Language

### Execution

**Evaluator**:
The tree-walking interpreter in `core/eval.go` — the default and complete execution path for all special forms.
_Avoid_: interpreter, VM, runtime

**VM**:
An opt-in stack machine in `core/vm` that runs bytecode for a subset of forms to speed up hot loops; not the default and not at full parity.
_Avoid_: evaluator, interpreter

**Compiler**:
Translates the AST into bytecode for the VM (`core/compiler`); unrelated to native-code compilation.
_Avoid_: transpiler

**Special form**:
A form the Evaluator handles directly without pre-evaluating its arguments, e.g. `if`, `let`, `quote`.
_Avoid_: builtin, keyword, macro

**Builtin**:
A Go function (`GoFunc`) callable from Lisp whose arguments are evaluated before the call; supplied by plugins, never core.
_Avoid_: special form, primitive, native function

**Macro**:
A form that rewrites its unevaluated arguments into new code at expansion time, before evaluation.
_Avoid_: special form, function

**Literal**:
A vector `[...]` or map `{...}` written in source; its elements are evaluated when the literal is evaluated.
_Avoid_: constant, data literal

**Equality**:
`=` is structural equality via `Equals`, strict across types — `(= 1 1.0)` is false. Ordering (`<`, `>`, `<=`, `>=`) is numeric-only, variadic monotonic, mixing int and float by the same promotion arithmetic uses. A numeric `==` does not exist until something needs it.
_Avoid_: numeric equality (as a meaning for `=`), identity

### Dialect

**Dialect**:
A named language configuration an Engine runs, fixed at Engine construction — a Delta over a declared base, plus Semantic axes, reader feature flags, and a vocabulary name map. Common Lisp flavor is the default; Clojure-style and restricted rule subsets (yagel) are alternative dialects.
_Avoid_: profile, subset, flavor, language mode

**Semantic axis**:
A kernel evaluation rule a Dialect may set. v1 axes: symbol namespaces (Lisp-1 vs Lisp-2) and truthiness (nil-only vs nil+false falsy). Data representation (immutable List/Vector/HashMap, no cons cells) and data immutability are fixed, not axes.
_Avoid_: feature flag, mode, option

**Kernel table**:
The canonical set of special forms the Evaluator implements under neutral names; every Dialect is expressed against it.
_Avoid_: specialForms map, syntax table, builtin set

**Delta**:
A Dialect's changes against its base — renames, additions, removals — covering special forms and vocabulary alike. The base is either the full Kernel table (language dialects) or empty (fail-closed restricted dialects).
_Avoid_: patch, overlay, override

### Embedding

**Engine**:
The public embedding handle in `runtime` that owns an environment, loads plugins, and evaluates source.
_Avoid_: interpreter, VM, context

**Plugin**:
A bundle of builtins registered into the environment under a namespace; the namespace is chosen independently of the plugin's `Name()`.
_Avoid_: module, extension, package

**Namespace**:
The prefix on a builtin's name, e.g. `http/`, `json/`; groups a plugin's functions and need not equal the plugin `Name()`.
_Avoid_: package, scope

**Sandbox**:
A real security boundary in the `lio` plugin that confines file access to a root, resolving symlinks before the check.
_Avoid_: path guard, jail

**Trust domain**:
One Engine is the unit of trust isolation. Code from different trust levels runs on separate Engines; a fresh Engine is cheap (~124B, ~123µs boot with stdlib). Persistent top-level `def`/`set!` across `Eval` calls is intended REPL state within a single Engine, not a cross-call isolation break — so an Engine must not be shared across a trust boundary.
_Avoid_: session, tenant, per-Eval isolation

**Resource limits**:
Per-Engine, construction-time ceilings that keep adversarial or accidental input from exhausting the host — reader nesting depth, evaluator structural depth (Vector/HashMap/quasiquote descent), collection length (`range`), and chunk-cache size. Fail-closed with a clean error, never a fatal stack overflow. Distinct from `MaxDepth`, which bounds function-call/macro depth per evaluation.
_Avoid_: quota, timeout (that is `WithTimeout`), sandbox

## Relationships

- An **Engine** runs exactly one **Dialect**, fixed at construction; a **Dialect** is a **Delta** over the **Kernel table** (or an empty base) plus **Semantic axes**, reader flags, and a name map over shared **Builtins**.
- An **Engine** loads **Plugins**; each **Plugin** registers **Builtins** under a **Namespace**.
- The **Evaluator** handles **Special forms** and expands **Macros** directly, and calls **Builtins**.
- The **Compiler** turns forms into bytecode; the **VM** runs it for a subset and otherwise defers to the **Evaluator**.

## Example dialogue

> **Dev:** "Is `+` a special form?"
> **Designer:** "No — `+` is a **Builtin** from the stdlib **Plugin**; its arguments are evaluated first. `if` is a **Special form**: the **Evaluator** decides which branch to run. `defmacro` defines a **Macro**, which rewrites code before evaluation."
> **Dev:** "When I turn on the **VM**, does it run everything?"
> **Designer:** "It runs a subset, but it now covers all dialects and caches compiled chunks per `Engine`. Anything it still can't compile defers to the **Evaluator** — the VM is an optimization for hot loops and repeated loads, not a replacement, and it stays opt-in via `WithBytecode()`."

## Flagged ambiguities

- "evaluator" was used for both the tree-walker and the VM — resolved: the **Evaluator** is the tree-walker (default, complete); the **VM** is an opt-in subset executor. See `docs/adr/0002-bytecode-vm-disposition.md`.
- "immutable" was used unqualified for Values — resolved: immutability holds at the `Equals` level; `HashMap.Set` is an internal-only construction escape hatch, not public mutation.
- "thread-safe" was read as concurrent evaluation on one **Engine** — resolved: that is the contract; per-evaluation depth and `recur` state must not be shared engine fields. See `docs/adr/0003-concurrency-model.md`.
- "sandbox" was left undefined between a convenience guard and a boundary — resolved: it is a real security **boundary**.
- **Literal** element evaluation was inconsistent (quasiquote evaluated inside vectors, plain literals did not) — resolved: literal elements are evaluated. See `docs/adr/0001-literal-evaluation-semantics.md`.
