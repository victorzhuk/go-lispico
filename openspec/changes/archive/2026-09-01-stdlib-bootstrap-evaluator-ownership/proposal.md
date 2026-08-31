## Why

The canonical stdlib contract requires Lisp-source bootstrap definitions to use
the environment's evaluator, but eager and lazy bootstrap paths construct a
separate identity Evaluator. That bypasses the Engine's namespace axis and makes
the two startup modes depend on manual binding repair.

Blocked by: none.

## What Changes

- Add a host-facing `core.BootstrapDefiner` capability to environment-owned
  evaluators for trusted stdlib `defn`/`defmacro` source.
- Route eager and lazy bootstrap definitions through the same owned path while
  preserving the Engine's Dialect axes and full-kernel definition privilege.
- When stdlib is initialized directly on an environment with no evaluator,
  install the default Evaluator on that environment before defining source rather
  than evaluating through an unowned temporary value.
- Remove manual evaluator substitution as a production bootstrap path; keep
  function-cell publication an explicit Lisp-2 concern of the owned definition.
- State the actual trust boundary: host-installed Go plugins are trusted and may
  discover the capability; evaluated Lisp code cannot invoke it.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `core-engine`: define the trusted-host bootstrap capability, grammar, and
  tree/bytecode implementor contract.
- `stdlib-plugin`: make evaluator ownership and eager/lazy bootstrap definition
  behavior explicit and independently testable.

## Impact

- Affects stdlib bootstrap loading, lazy source materialization, environment
  evaluator setup, Lisp-2 publication tests, and restricted-Dialect startup.
- Adds an exported Go capability implemented by the tree evaluator and runtime
  bytecode evaluator; it is not a Lisp binding or a security boundary between Go
  plugins.
- Does not change the names or bodies of bootstrap macros/functions.
- Unblocks `stdlib-nil-lookup-semantics`, which removes `get-in` from the
  bootstrap set only after the remaining definitions have one correct owner.
