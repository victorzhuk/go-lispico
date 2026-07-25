# Design

## The current function

```go
func (e *engine) resolveHead(ctx context.Context, head Value, env *Env) (Value, bool) {
	if sym, ok := head.(Symbol); ok && e.lisp2 {
		if fn, ok := env.GetFunc(sym.V); ok {
			return fn, true
		}
	}
	fn, err := e.Eval(ctx, head, env)
	if err != nil {
		return nil, false
	}
	return fn, true
}
```

The contract is "resolve this head to a value if it names one, otherwise report that it
does not." For a special-form head the answer is already "it does not" — but the function
arrives there by evaluating, failing, allocating an error, and discarding it.

## Placement is the whole design

The check must sit **after** the Lisp-2 function-cell lookup and **before** `e.Eval`.

Putting it first would be wrong. Under a Lisp-2 dialect — the default CL surface — a user
can bind a macro in the function cell under a name that also happens to be a special form.
Today `env.GetFunc` runs first and that binding wins. A short-circuit ahead of it would
silently stop honouring the rebind, which is a behavior change, not an optimization.

Putting it after preserves every existing resolution order and only removes work on the
path that was already destined to return `nil, false`.

The `e.lisp2` guard currently sits in the same condition as the symbol type assertion, so
the restructure needs care: the symbol assertion is needed by both checks, the `lisp2`
guard by only one.

## Why `e.forms` is the right table

`evalList` (`core/eval.go:471`) dispatches on `e.forms[sym.V]`, and `core/eval.go:1147`
already gates on `if _, special := e.forms[head.V]; special`. Using the same map keeps one
source of truth for what a special form is. It also means dialect renaming is handled for
free — `e.forms` is populated per dialect, so `progn` under CL and `do` under the kernel
both resolve correctly without the fix knowing anything about dialects.

## What must not change

- A genuinely undefined head still reports as unresolved. The short-circuit fires only for
  names present in `e.forms`; an undefined symbol is not, so it still takes the `e.Eval`
  path and still fails. `TestEval_MacroExpand_UndefinedHead` covers this.
- Lisp-2 function-cell rebinds still win, including for special-form names.
- Macro expansion results are unchanged. This changes how the "is it a macro" question is
  answered for one class of head, not what the answer is.

## Verification

The allocation is the observable, so measure it rather than reason about it:

- Gold-set paired capture before and after. `guard-nil` and `rule-load` should lose
  allocations per iteration in proportion to their special-form-headed top-level forms —
  two and eleven respectively — and both should move toward or past parity with the
  tree-walker.
- `safe-parse` and `registry-fold` pay the same tax and should improve slightly; `pipeline`
  has no special-form head and should not move. That pattern is the check that the fix
  targets what it claims to.
- allocs/op is deterministic and is the signal to judge on; timing on this machine is noisy
  enough that a few percent means nothing.

## Rejected alternatives

- **Skip `MacroExpand` when the chunk cache hits.** Larger and riskier: expansion can depend
  on bindings that changed since the chunk was cached, and the cache keys on macro epoch
  precisely because of that. Not a change to make while chasing an allocation.
- **Cache the "is this head a macro" answer.** Adds an invalidation problem — the very one
  the macro epoch exists to solve — to save work that the short-circuit removes outright.
- **Make the error cheaper to construct.** Treats the symptom. The error should never be
  built at all on this path.
