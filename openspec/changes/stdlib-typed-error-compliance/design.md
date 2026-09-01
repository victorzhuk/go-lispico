## Context

GoFunc apply sites in both execution paths return callee errors unchanged. Core
has typed constructors for simple arity and type failures, but stdlib messages
often describe arity ranges, allowed sets, argument positions, bounds, malformed
formats, or incomparable values. Replacing every message with the current generic
constructors would satisfy type recovery while creating unnecessary diagnostic
regressions.

## Goals / Non-Goals

**Goals:**

- Make every stdlib- and CL-adapter-originated evaluation failure recoverable
  with `errors.As`.
- Give arity, type, domain/evaluation, resource, and context failures stable codes.
- Preserve useful operation-specific message text.
- Keep higher-order callback and Terminal errors intact.

**Non-Goals:**

- Introduce a stdlib-specific error hierarchy or numeric subcodes.
- Change successful semantics, add argument coercion, or normalize all wording.
- Convert errors from frozen world-touching plugins in the same change.
- Add source positions. `core.Value` carries no position, so a future
  positional-form change must define storage and propagation before stdlib can
  promise them.

## Decisions

### Classify by violated contract

Wrong argument counts use `ArityError`, including exact, ranged, and variadic
minimum shapes. A value of the wrong runtime type uses `TypeError`. A correctly
typed value that violates an operation domain—out-of-bounds indexing, division by
zero, invalid format syntax, or incomparable values—uses `EvalError` unless a
more specific existing code already governs it. Resource and context failures
remain Terminal and are never wrapped into a catchable class.

### Preserve messages through local typed construction helpers

Use the existing exported `core.LispicoError` fields and constructors through
small unexported stdlib helpers for repetitive exact/ranged/variadic arity,
positional type, domain-message, and external-cause shapes. Do not add a parallel
public error hierarchy or a generic constructor that accepts arbitrary codes.
Existing messages become characterization data: wording may be cleaned only when
tests show it was not part of a documented contract.

Wrapping plain errors at the central apply site was rejected because it cannot
reliably distinguish arity, type, and domain failures and would hide which plugin
still violates the contract. Migrating one Builtin at a time without a complete
inventory was rejected because it perpetuates partial host behavior.

### Pass through errors from callbacks and shared checkpoints

Higher-order Builtins return errors from `Evaluator.Apply` unchanged. Builtin
resource checkpoints and explicit resource helpers likewise return their typed or
Terminal errors directly. Only errors originated by stdlib validation/conversion
are classified in this change.

### Verify direct and public boundaries separately

Direct Builtin tests pin code and diagnostic meaning. Engine behavior goldens
under Evaluator and VM additionally assert `errors.As` and code equivalence.
Equivalent strings alone are insufficient evidence.

### Freeze the reachable error inventory

The table below is the approval baseline. Each row includes the GoFuncs and
non-GoFunc helpers reachable from them. Before migration, an executable inventory
SHALL enumerate every concrete return site in these rows and record its target
classification. A static test SHALL compare the active registration surface and
reachable package functions against that inventory.

| Family | GoFuncs | Reachable validation/helper sites | Target classes |
| --- | --- | --- | --- |
| Arithmetic | `+`, `-`, `*`, `/`, `mod`, `quot`, `pow`, `sqrt`, `abs`, `floor`, `ceil`, `zero?`, `pos?`, `neg?`, `max`, `min` | numeric conversion, unary numeric and min/max factories | arity, type, domain for zero divisor |
| Comparison | `=`, `<`, `>`, `<=`, `>=` | ordering factory, numeric comparison, value equality | arity, type/incomparability; no flattening of typed helper errors |
| Types | `type`, `nil?`, `bool?`, `int?`, `float?`, `string?`, `keyword?`, `symbol?`, `list?`, `vector?`, `map?`, `fn?`, `macro?`, `str->keyword`, `keyword->str`, `int->float`, `float->int` | conversion assertions | arity, type |
| Collections | `list`, `concat`, `reverse`, `vector`, `hash-map`, `first`, `rest`, `last`, `nth`, `count`, `cons`, `conj`, `empty?`, `get`, `get-in`, `assoc`, `keys`, `vals`, `contains?`, `merge`, `dissoc`, `sort`, `range` | collection extraction/append, lookup cursor, result charging, natural comparator, bounds and size helpers | arity, type, domain, Terminal/resource pass-through |
| Higher order | `map`, `filter`, `reduce`, `apply` | sequence extraction and evaluator callback application | arity, type, callback/Terminal pass-through |
| Strings | `str`, `format`, `string/join`, `string/split`, `string/trim`, `string/upper`, `string/lower`, `string/replace`, `string/contains?`, `string/starts-with?`, `string/ends-with?`, `string/length`, `string/lines`, `string->int`, `string->float` | unary-string factory, formatting, parse conversion | arity, type, domain or wrapped external parse error |
| Control | `assert` | assertion message formatting | arity, domain/evaluation |
| CL adapters | CL `nth`, `mapcar`, `sort` | option grammar, shared kernels, evaluator callbacks | arity, type, domain/evaluation, callback/Terminal pass-through |

Bootstrap read/evaluation wrapping is not a registered Builtin failure and is
excluded from this requirement; it must preserve an already typed cause under
the bootstrap ownership contract. The only allowed plain external errors are
those immediately converted into a typed stdlib error with the original cause
available to `errors.Is`/`errors.As` where applicable.

## Risks / Trade-offs

- [Risk] A domain error is mislabeled as a type error → inventory each failure by
  violated contract before changing constructors.
- [Risk] Wrapping destroys Terminal identity → pass context/resource errors
  through and test non-catchability under higher-order Builtins.
- [Risk] Message cleanup expands scope → characterize existing diagnostics and
  defer unrelated wording changes.
- [Risk] A helper called by several GoFuncs retains a plain error → scan stdlib
  and CL-adapter functions reachable from registered GoFuncs, not only closure
  bodies, and keep reviewed exceptions only for immediate external conversion.

## Migration Plan

Freeze the executable inventory first, land local helpers and classification tests,
migrate Builtin families in small batches, then enable the package-wide
completeness check. Rollback restores the old constructors; valid program values
and stored data are unaffected.
