## 1. Behavior contracts

- [x] 1.1 Red test (both evaluators): the closures-in-loop repro returns
  `(0 1 2)`, not `(3 3 3)`. Add as a crossval case.
- [x] 1.2 Characterization: a non-capturing accumulate loop (`loop-sum` shape)
  returns the same result and — asserted via the allocation gate — allocates the
  same as before.
- [x] 1.3 Interaction test: a loop body that `set!`s a captured loop variable
  mid-iteration observes the `set!` value in a closure created after it, while
  `recur` still starts a fresh cell next iteration.
- [x] 1.4 Nested-loop / multiple-captured-slot cases behave per-iteration.

## 2. Implementation

- [x] 2.1 Compiler: in `compileRecur` / `finalize`, emit a fresh-cell bind
  (`OpBindCell`) for captured loop slots (per `markCaptures`) and keep
  `OpSetLocal` / write-through for non-captured slots.
- [x] 2.2 Tree-walker: in `evalLoop`, install a fresh `*Cell` for captured loop
  slots each iteration; keep in-place overwrite for non-captured slots. Share
  the capture analysis with the compiler rather than hand-rolling a second scan.
- [x] 2.3 Ensure `set!` on a captured loop slot still mutates the current
  iteration's cell (write-through), leaving `recur`'s fresh-cell behavior
  orthogonal.

## 3. Integration

- [x] 3.1 `go test ./... -race` green.
- [x] 3.2 `GOLDSET_MODE=vm` goldset gate non-increasing — verify `loop-sum` and
  other non-capturing loop cells show no allocation increase.
- [x] 3.3 Crossval suite green, including the new closures-in-loop and
  set!-interaction cases in both modes.

## 4. Verification

- [x] 4.1 `openspec validate --strict loop-recur-per-iteration-binding`.
- [x] 4.2 CHANGELOG `[Unreleased]` under Fixed: closures created in a `loop` body
  now capture per-iteration loop-variable values.
