## Why

yagel ADR 0105: "Retained state is charged by owned allocation identity and actual backing capacity, not logical length. Each Routine child environment caps at 32 MiB / 100,000 allocated-capacity slots. Deleting entries releases unreachable children but not surviving buckets / slots; only a metered atomic rebuild can replace and release backing." go-lispico currently has no `Env` capacity accounting; a Routine child env can grow unbounded in retained slots even with reduction and allocation metering.

## What Changes

- Extend `runtime.ResourceLimits` with `MaxRetainedBytesPerEnv int` and `MaxRetainedSlotsPerEnv int` (zero → default; defaults `32 * 1024 * 1024`, `100_000`).
- Each `core.Env` carries an owned-capacity counter (bytes + slot count). `Env.Child()` produces a fresh counter bounded by the configured limit. Binding a new `Cell` charges the actual backing bytes (map slot + `Cell` struct) + 1 slot.
- Deleting a binding tombstones the slot but does NOT release the backing map slot (matches Go map semantics); only `(*Env).Rebuild()` — a new public method — constructs a fresh env from the current binding set and atomically swaps it in, releasing the old backing.
- Closure capture does not transfer ownership: a `Lambda` capturing an `Env` does not debit that env's counter again. The Env's backing stays owned by the Env until `Rebuild` or env-tree teardown.
- Introduces ADR 0012.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-api`: new `ResourceLimits` fields + new requirement `Retained state is charged by owned capacity`.
- `core-engine`: new requirement `Env owned-capacity accounting`.

## Impact

- Code: `core/env.go` (capacity tracking + new `Rebuild`), `core/eval.go` (charge on new binding in `let` / `fn` / `defn` / `defmacro`), `runtime/engine.go` (thread limits), `vm/vm.go` (charge on per-call frame env alloc).
