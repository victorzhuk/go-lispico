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

## Relationships

- An **Engine** loads **Plugins**; each **Plugin** registers **Builtins** under a **Namespace**.
- The **Evaluator** handles **Special forms** and expands **Macros** directly, and calls **Builtins**.
- The **Compiler** turns forms into bytecode; the **VM** runs it for a subset and otherwise defers to the **Evaluator**.

## Example dialogue

> **Dev:** "Is `+` a special form?"
> **Designer:** "No — `+` is a **Builtin** from the stdlib **Plugin**; its arguments are evaluated first. `if` is a **Special form**: the **Evaluator** decides which branch to run. `defmacro` defines a **Macro**, which rewrites code before evaluation."
> **Dev:** "When I turn on the **VM**, does it run everything?"
> **Designer:** "It runs a subset. Anything it can't compile defers to the **Evaluator** — the VM is an optimization for hot loops, not a replacement."

## Flagged ambiguities

- "evaluator" was used for both the tree-walker and the VM — resolved: the **Evaluator** is the tree-walker (default, complete); the **VM** is an opt-in subset executor. See `docs/adr/0002-bytecode-vm-disposition.md`.
- "immutable" was used unqualified for Values — resolved: immutability holds at the `Equals` level; `HashMap.Set` is an internal-only construction escape hatch, not public mutation.
- "thread-safe" was read as concurrent evaluation on one **Engine** — resolved: that is the contract; per-evaluation depth and `recur` state must not be shared engine fields. See `docs/adr/0003-concurrency-model.md`.
- "sandbox" was left undefined between a convenience guard and a boundary — resolved: it is a real security **boundary**.
- **Literal** element evaluation was inconsistent (quasiquote evaluated inside vectors, plain literals did not) — resolved: literal elements are evaluated. See `docs/adr/0001-literal-evaluation-semantics.md`.
