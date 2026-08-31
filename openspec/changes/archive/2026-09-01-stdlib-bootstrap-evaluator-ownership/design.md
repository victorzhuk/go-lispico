## Context

The eager loader and lazy materializer currently use fresh identity Evaluators
for non-reusable source. This grants the trusted source access to `defn` and
`defmacro`, but it also discards the Engine evaluator's namespace configuration.
The eager path then mirrors bindings manually into the Lisp-2 function cell.

An empty-base Dialect still needs trusted stdlib bootstrap source to be definable
even when untrusted program code cannot call `defn` or `defmacro`. Definition
privilege and Dialect ownership are therefore separate concerns.

## Goals / Non-Goals

**Goals:**

- Give every bootstrap definition exactly one environment-owned evaluator.
- Preserve full-kernel privilege for trusted definitions without broadening the
  running Dialect's user-visible Kernel table.
- Make eager and lazy publication follow the same binding rules.
- Preserve direct stdlib initialization on a plain environment.

**Non-Goals:**

- Change bootstrap source bodies, lazy first-touch policy, or plugin vocabulary.
- Expose privileged definition evaluation to evaluated Lisp code.
- Sandbox one host-installed Go plugin from another; Go plugins are trusted code
  and can discover exported structural interfaces on the evaluator they receive.
- Remove the reusable bootstrap artifact cache; that lifecycle changes only
  after `get-in` leaves the source set.

## Decisions

### Add one explicit trusted-host capability

Add `core.BootstrapDefiner` with the operation
`DefineBootstrap(context.Context, string, *core.Env) (core.Value, error)`.
Both `*core.engine` and `*runtime.bytecodeEvaluator` implement it. The bytecode
evaluator delegates to the privileged operation on its dialect-configured tree
evaluator, so it cannot substitute an identity evaluator or create a second
binding owner. The stdlib loader requires this capability from `env.Evaluator()`;
after adopting the default evaluator for a standalone environment, absence of the
capability is an initialization error rather than a fallback to ordinary `Eval`.

The operation uses the trusted full bootstrap reader, independent of the active
Dialect's reader flags, because shipped source contains vector binding forms that
the CL reader deliberately rejects. It accepts exactly one top-level proper list
headed by `defn` or `defmacro`; any other form or multiple forms fail with a typed
bootstrap evaluation error. Only dispatch of that top-level definition uses the
full kernel. Evaluation state, namespace mode, truthiness, limits, and binding
ownership remain those of the installed evaluator.

The interface is exported because `core.Env` and `core.Evaluator` cross package
boundaries. Go structural interfaces cannot make it available to stdlib while
hiding it from another plugin holding the same evaluator. The security boundary
is therefore precise: trusted host Go code may call this capability; evaluated
Lisp has no binding, Special form, reflection primitive, or value that exposes
it. Constructing a fresh identity Evaluator inside the loader was rejected
because it recreates the ownership violation. Temporarily changing the Engine
Dialect was rejected because Dialects are immutable per Engine and are a
restriction boundary.

### Use one helper for eager and lazy source

The eager loader and lazy materializer call `BootstrapDefiner.DefineBootstrap`.
The helper publishes both value and function-cell bindings according to the
environment evaluator, so lazy first touch cannot diverge from eager startup.
Manual post-definition mirroring may remain only as an implementation detail if
the owned evaluator explicitly performs it; callers do not repair bindings.

### Adopt a fallback evaluator into standalone environments

Direct plugin tests and embedders may initialize stdlib on an environment before
assigning an evaluator. In that case the loader constructs the normal default
Evaluator, installs it on the environment, and then invokes the owned operation.
Failing initialization was rejected as an unnecessary compatibility break; using
the fallback without installing it would preserve the defect.

## Risks / Trade-offs

- [Risk] The trusted-host capability is mistaken for a plugin sandbox → document
  that all host-installed Go plugins share trust and separately test that no Lisp
  binding or first-class value exposes the capability.
- [Risk] CL reader flags reject shipped bootstrap vector syntax → parse through
  the trusted bootstrap reader while preserving the evaluator's remaining
  Dialect axes.
- [Risk] Lazy and eager paths publish different cells → share one helper and run
  the same Lisp-1/Lisp-2 behavior goldens in both modes.
- [Risk] A standalone environment silently changes evaluator later → install the
  fallback only when none exists and test that an existing evaluator is never
  replaced.

## Migration Plan

Add failing ownership/publication/trust-boundary tests, introduce the owned
definition seam,
then switch eager and lazy callers together. Rollback restores the previous
callers and manual mirroring; no persisted data is involved.
