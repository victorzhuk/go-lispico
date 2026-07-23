## Why

Allocation metering charges a `GoFunc`'s result shallowly and after the fact:
`vm.chargeValue(result)` → `chargeAllocBytes(ValueShallowBytes(result))`
(`core/vm/vm.go:540`, tree-walker mirror `core/eval.go:499`). That keeps the
common scalar-returning builtin cheap, but it under-accounts builtins that turn
small input into large allocation:

- `json/decode` (`plugins/data/plugin.go:56-130`) builds a fully nested
  `Vector`/`HashMap` via `fromJSONValue`, then only the outer container's
  shallow slot count is charged. `ValueShallowBytes` does not recurse, so a
  compact payload — a few million-element nested arrays — is charged a few
  hundred bytes while allocating tens to hundreds of MB. The ledger never sees
  it.
- `format` (`plugins/stdlib/strings.go:26-48`) runs `fmt.Sprintf` to completion
  before any charge. Go caps one width/precision verb near 1 MB but not the
  verb count, so a few-KB source with hundreds of `%999999d` verbs allocates
  hundreds of MB transiently before the post-hoc charge can react.

`resolveLimits` sets `MaxAllocationBytes` = 64 MiB even for `New(nil)`, so the
allocation governor is active by default — and these are live bypasses of it,
the subsystem the shared-engine consumer relies on for tenant isolation.

## What Changes

- Amplifying builtins — those that can construct output disproportionate to
  their input — SHALL charge their constructed allocation against the
  evaluation ledger deeply and before returning, using the existing
  `core.ChargeEvalAllocBytes`, rather than relying on the shallow post-hoc
  result charge.
- `json/decode` charges `core.ValueDeepBytes` of the decoded structure so
  nested structure counts against the ledger.
- `format` charges an upper-bound estimate of its output — derived from the
  parsed width/precision specifiers — before calling `fmt.Sprintf`, failing
  closed before the large allocation.
- Audit the remaining small-input/large-output builtins in the same pass
  (string-repeat / `make-string`-style constructors, count-driven collection
  builders) and give them the same eager-charge treatment or confirm they are
  already bounded.
- No API change; the common scalar-returning `GoFunc` path keeps its cheap
  shallow charge.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `stdlib-plugin`: new requirement `Amplifying builtins charge output
  allocation eagerly`.
- `data-plugin`: new requirement `JSON decode charges constructed allocation`.

## Impact

- Code: `plugins/stdlib/strings.go` (`format`), `plugins/data/plugin.go`
  (`decode` / `fromJSONValue`), and any other amplifying stdlib builder found in
  the audit.
- Downstream: the allocation governor bounds these builtins, closing a
  tenant-isolation gap for the shared-engine consumer.
- Revisits, narrowly, the metering program's "no per-plugin charge edits"
  decision: the generic shallow charge stays for scalar returns; only the few
  amplifying builtins opt into explicit charging.
