## Context

See `proposal.md` for the verified round-trip failure at review commit `3bcf9c1`. `json/decode` unmarshals into `any`, then `fromJSONValue` sees only rounded `float64` numbers. `TestDecodeLargeIntegers` explicitly expects `Float` at `9007199254740992`, while the accepted spec promises whole-number `Int` detection without defining its domain.

## Goals / Non-Goals

**Goals:** classify the exact decimal value before float conversion; preserve every integral value within `int64`; retain finite float fallback and existing parsing behavior.

**Non-Goals:** bigint Lisp values, external dependencies, lossless arbitrary-precision fractions, changes to container/key conversion, and the separate exactly-once result-metering fix.

## Decisions

### Preserve numeric tokens through decoding

Retain each validated JSON number's original decimal spelling until numeric classification finishes. Plain integer-token parsing alone is insufficient: `42.0`, `4.2e1`, and `9007199254740993.0` represent integers too. Do not use a rounded float to decide whether the exact number has a fractional part or fits in `int64`.

Classify sign, significant digits, fractional scale, exponent, and trailing zeroes without expanding powers of ten. Compare the normalized integer magnitude against the signed endpoint before accumulation or conversion. Exponent accumulation must saturate at a bound derived from token length and the fixed integer domain; storage and work remain proportional to token length, not exponent magnitude. Zero remains exact regardless of a valid exponent spelling.

Numbers outside the exact integer case use the existing float conversion semantics: a finite value, including underflow to zero, returns `core.Float`; conversion overflow still returns an error. A fractional token that rounds to an integer float remains a `Float`. Rejecting every out-of-`int64` whole number was considered and not selected; finite fallback remains supported.

### Preserve the parsing boundary

Continue requiring exactly one JSON value, followed only by whitespace. If decoding becomes stream-based, explicitly check that no second value or trailing garbage remains. Preserve malformed-input errors, key conversion, recursive array/object conversion, depth checks, and the linear `HashMap` construction path.

### Own the numeric requirement updates

This change modifies all three existing JSON requirements to replace unconditional integer promises with the agreed `int64` domain and finite float fallback. The existing deep-allocation requirement stays in force; `json-result-metering` adds its exactly-once refinement after this change is implemented and archived.

## Verification

Testing mode: existing-service-strict. Add failing regressions before implementation and replace the contradictory outside-safe-range expectation in `TestDecodeLargeIntegers`.

Assert exact `core.Int` values for both `int64` endpoints and adjacent integers around positive and negative `2^53`, at the root and inside arrays/maps. Cover decimal and exponent equivalents, including `9223372036854775807.0`, `-9.223372036854775808e18`, and `9007199254740993.0`. Assert finite `core.Float` for fractional inputs, near-integer fractions, and `9223372036854775808` / `-9223372036854775809`; the latter cases are fallback checks, not exact integer guarantees.

Cover positive exponent overflow, negative exponent underflow, zero with a large exponent, long exponent digit strings, malformed JSON, trailing whitespace, a second value, and trailing garbage. Include true integer encode/decode round trips through `Engine.Call` and `Engine.Eval` under both explicit execution modes. Keep `TestDecodeHashMap_Scaling`, round-trip, immutability, construction-depth, and allocation tests valid; update any test-only decoded intermediate representation to match the production decoder.

## Risks / Trade-offs

- Existing callers may branch on `core.Float` for large whole numbers → mark the type change in the existing changelog and numeric documentation.
- Arbitrary exponent expansion can turn a short token into huge work → use bounded digit/scale classification with no power expansion.
- A stream decoder can accept a valid prefix → retain the single-value end-of-input check.
- Fractional or out-of-range values remain approximate → make finite float fallback explicit in every affected guarantee.

## Migration Plan

No predecessor or stored-data migration. Update existing JSON documentation and the capability purpose to state the integer domain when implementing this change; do not create a new architecture document. Archive this change before starting `json-result-metering`. Rollback restores the former numeric type and precision behavior.

## Implementation plan

Tier heavy. Testing mode existing-service-strict. Base `ae8f8bd`. Review lenses: `spec`, `quality`, `sec`, `perf` — `sec` because decoding is an untrusted-input path with an explicit work bound, `perf` because the linear-decode and scaling guarantees stay in force.

Production shape, closed here so no coder reopens it: `json.Decoder` with `UseNumber()`, one `Decode`, then a second `Decode` that must return `io.EOF`. The second-`Decode` check reproduces the current accept/reject set exactly. `fromJSONValue`'s signature is unchanged, so no field-first member chunk is needed.

Every chunk pairs its red tasks with the code that turns them green; a red-only chunk cannot close, because `waive()` is gated on empty `redTasks` and a tester cannot land another chunk's fix.

### exact-int-endpoints — tasks 1.1, 2.1

Red 1.1, code 2.1, integration worktree, no predecessor. Sites: `TestDecodeLargeIntegers` (`plugins/json/json_test.go`), `Plugin.decode` and the `fromJSONValue` number branch (`plugins/json/plugin.go`), and `timeDecode` in `TestDecodeHashMap_Scaling`.

`timeDecode`'s fixture edit is lifted out of task 2.3 and owned by the **red writer** of this chunk: `guard()` runs with `allowSites=false` on any chunk carrying `redTasks`, so a coder editing `json_test.go` is a hard escalation. Once the fixture hands `fromJSONValue` a `json.Number`, HEAD's type switch has no case for it and falls to `default:` — so `TestDecodeHashMap_Scaling` is **expected red between this chunk's red and code stages**, and green once 2.1 lands in the same chunk.

Scope bound: this chunk implements exact integrality and signed `int64` range classification only. Exponent saturation, the no-power-expansion rule and the token-length work bound belong to 2.2.

- redTests: `TestDecodeLargeIntegers`
- redRun: `go test -timeout 2m -run 'TestDecodeLargeIntegers|TestDecodeErrors' ./plugins/json/` — widened to the existing parser pins so the single-value boundary is guarded at the moment the parser changes.

### numeric-matrix — tasks 1.2, 2.2

Red 1.2, code 2.2, `prev: exact-int-endpoints`, shared package `plugins/json`. The wider matrix: decimal and exponent spellings, fractional values that round to integral floats, finite out-of-`int64` fallback, overflow, underflow, zero with large exponents, long exponent digit strings.

This chunk owns the bounding proper. A chunk closing with no code change means the boundedness requirement went unimplemented.

- redTests: `TestDecodeNumericClassification`

### structure-and-boundary — tasks 1.4, 2.3

Red 1.4, code 2.3, `prev: numeric-matrix`. Task 1.4 adds a **new top-level** `TestDecodeParsingBoundary` — not an extension of `TestDecodeErrors`, because `redTests`, `guard` and `verifyRun` all require the top-level name. Its four pins pass at HEAD and are characterization: they belong in `alreadyGreen`, never as manufactured failures.

### parity-boundary — task 1.3, shard `parity`

`prev: structure-and-boundary`, packages `./runtime`. New file `runtime/json_integer_parity_test.go`. No existing helper builds a stdlib+json engine under both `WithTreeWalker()` and `WithBytecode()` — the only dual-mode construction is inline at `runtime/dialect_native_op_test.go:220-231 `, so this chunk introduces its own builder.

Scheduled after the fix, so its assertions are green on arrival and close through `verifyRun`'s already-green path. The cost is real and accepted: the run never demonstrates the Engine-boundary reproduction failing.

### docs-disclosure — task 3.2, shard `docs`, zpatcher

`README.md` and `CHANGELOG.md` only. The change breaks in **two** directions and the entry must name both:

- `Float` → `Int` for integral values from `9007199254740992` upward within `int64`;
- `Int` → `Float` for fractional values that round to an integral float and for underflow to zero — at HEAD `1.0000000000000000001` decodes to `Int 1` and `1e-400` to `Int 0`, because `ParseFloat` underflows to zero with a nil error and the narrowing at `plugins/json/plugin.go:113` accepts it. `proposal.md` discloses only the first direction.

The capability `Purpose` at `openspec/specs/json-plugin/spec.md:5` is **outside the delta** — archiving rewrites only the requirement blocks, and workers never touch `openspec/`. It is edited by the orchestrator in the archive commit, pinned to task 3.2's tick; its current unconditional claim is exactly what this change narrows.

### Floor, ordering, and what owns no stage

Floor: `make test GOTESTFLAGS="-timeout 2m -p 2 -parallel 2" && make lint`.

Task 3.1 **is** that floor and owns no stage. Task 3.3 is an ordering constraint on `json-result-metering` and owns no stage either. Task 0.1 is orchestrator recon.

`TestDecodeHashMap_Scaling` sits on the critical path: its ratio threshold is 3.0 with roughly 0.1 headroom and it is known to fire under load. Re-run same-machine before calling it a regression.

Plan review: pass by zarchitect, two rounds.

## Plan appendix

```json
{
  "v": 2,
  "change": "json-int64-decoding",
  "baseSha": "ae8f8bd7ed81f4da3837b8db6810b711a8799813",
  "generatedAt": "2026-09-08T10:12:31.399Z",
  "tier": "heavy",
  "mode": "existing-service-strict",
  "lenses": [
    "spec",
    "quality",
    "sec",
    "perf"
  ],
  "chunks": [
    {
      "id": "exact-int-endpoints",
      "taskIds": [
        "1.1",
        "2.1"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": false,
      "shard": "",
      "seam": "S1-exact-integer-classification",
      "pkgDirs": [
        "plugins/json"
      ],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "1.1",
          "file": "plugins/json/json_test.go",
          "symbol": "TestDecodeLargeIntegers",
          "anchor": "\tt.Run(\"float for large int outside safe range\", func(t *testing.T) {",
          "change": "Replace the outside-safe-range Float subtest with an exact core.Int assertion at 9007199254740992, and extend the test with exact-value rows for math.MinInt64, math.MaxInt64, +/-9007199254740992 and +/-9007199254740993, asserted at the root, inside a decoded array and inside a decoded object. Assert concrete type and exact value (type assertion to core.Int plus .V equality), not only .Equals, so a float64 round trip goes red."
        },
        {
          "task": "2.1",
          "file": "plugins/json/plugin.go",
          "symbol": "Plugin.decode",
          "anchor": "\tif err := stdjson.Unmarshal([]byte(s.V), &raw); err != nil {",
          "change": "Stop discarding the numeric lexeme: decode with a stdjson.Decoder over strings.NewReader(s.V) with UseNumber(), so numbers reach fromJSONValue as stdjson.Number (their validated decimal spelling), and keep the single-value contract by checking that nothing but whitespace follows (decoder.More() or a second Decode returning io.EOF). Preserve the 'json/decode: %w' error prefix for every parse failure."
        },
        {
          "task": "2.1",
          "file": "plugins/json/plugin.go",
          "symbol": "fromJSONValue - number branch",
          "anchor": "\tcase float64:",
          "change": "Replace the float64 branch with a stdjson.Number branch that classifies the exact decimal value before any float conversion: sign, significant digits, fractional scale, exponent; return core.BoxInt for every mathematically integral value in [-9223372036854775808, 9223372036854775807], otherwise fall back to Number.Float64() - a finite result (including underflow to zero) returns core.Float, a range error returns the existing 'json/decode: %w' error. Keep a float64 case only if some path can still deliver one; otherwise the default branch reports it as an unsupported type. SCOPE BOUND: this chunk implements exact integrality and signed int64 range classification only. Exponent saturation, the no-power-expansion rule and the token-length work bound are task 2.2, in the next chunk — do not implement them here."
        },
        {
          "task": "1.1",
          "file": "plugins/json/json_test.go",
          "symbol": "TestDecodeHashMap_Scaling.timeDecode",
          "anchor": "\t\tif err := stdjson.Unmarshal([]byte(jsonStr), &raw); err != nil {",
          "change": "RED-WRITER OWNS THIS EDIT (a coder touching a sealed _test.go is a guard escalation; guard runs allowSites=false on any chunk with redTasks). Expected red between the red and code stages of this chunk — json.Number reaches HEAD's type switch, which has no case for it, so TestDecodeHashMap_Scaling fails until task 2.1 lands in this same chunk. rewrite the fixture's intermediate representation to the production decode path (json.Decoder + UseNumber) so the package stays green once task 2.1 drops the float64 branch. This is the test-only decoded intermediate representation task 2.3 names: the scaling test builds its 'any' tree with stdjson.Unmarshal (float64 numbers) and calls the unexported fromJSONValue directly. Once decode moves to UseNumber, this fixture feeds a representation production no longer produces. Update it to build the same intermediate the plugin now builds (a stdjson.Decoder with UseNumber, or the plugin's own decode path) so the measured work is the real decode path; keep the 2000/4000-key shape, 5 samples, best-of, and the ratio < 3.0 assertion."
        }
      ],
      "contract": {
        "states": [
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "42",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:185; plugins/json/plugin.go:113-115"
          },
          {
            "input": "-17, 0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:186-187"
          },
          {
            "input": "9007199254740991",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:480-484"
          },
          {
            "input": "9007199254740992",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/json_test.go:486-490 is the contradictory expectation and must be replaced; spec: 'either of 9007199254740992 and 9007199254740993 ... SHALL be an exact Int'"
          },
          {
            "input": "9007199254740993",
            "state": "exact-int",
            "effect": "set",
            "evidence": "proposal.md:3 reproduction returns Float 9007199254740992; probed live at HEAD ae8f8bd through Engine.Eval and Engine.Call in both modes"
          },
          {
            "input": "9007199254740992 and 9007199254740993 decoded separately must not be equal",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: 'with distinct adjacent values remaining distinct'"
          },
          {
            "input": "-9007199254740992, -9007199254740993",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: 'adjacent integers around positive and negative 2^53' (design.md:33)"
          },
          {
            "input": "9223372036854775807",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: inclusive range endpoint"
          },
          {
            "input": "-9223372036854775808",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: inclusive range endpoint; magnitude 9223372036854775808 does not fit int64 -- accumulate in uint64"
          },
          {
            "input": "42.0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD already returns Int 42 via plugins/json/plugin.go:113; spec: 'regardless of integer, decimal, or exponent spelling'"
          },
          {
            "input": "4.2e1",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD already returns Int 42; spec: exponent spelling"
          },
          {
            "input": "9223372036854775807.0",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "-9.223372036854775808e18",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "9007199254740993.0",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "9.2233720368547758e18 (scale > 0 accumulation, value 9223372036854775800)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: exponent spelling within the inclusive range"
          },
          {
            "input": "-0, -0.0, 0.0, 0e0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD returns Int 0 (plugins/json/plugin.go:113, -0.0 == float64(int64(-0.0))); design.md:17 'Zero remains exact regardless of a valid exponent spelling'. Sign is dropped: the result is Int{V:0}, not a signed zero"
          },
          {
            "input": "3.14, -2.5",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:188-189"
          },
          {
            "input": "1.0000000000000000001",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "HEAD rounds to float64 1 and returns Int 1 (plugins/json/plugin.go:113); spec scenario 'Float rounding does not change numeric classification'. THE EXACT VALUE HAS A NONZERO FRACTIONAL PART -- 'fractional' means the exact value, never the presence of a '.' in the token"
          },
          {
            "input": "1e-400 (underflow to zero)",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "HEAD returns Int 0 (strconv.ParseFloat probed: 0 with nil error, so encoding/json publishes float64 0 and plugin.go:113 narrows it); spec: 'including underflow to zero -- THEN the result SHALL remain a Float'"
          },
          {
            "input": "9223372036854775808",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec scenario 'Finite float fallback remains supported'; ParseFloat probed finite 9.223372036854776e18"
          },
          {
            "input": "-9223372036854775809",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec scenario 'Finite float fallback remains supported'"
          },
          {
            "input": "1e19 (20 integral digits, out of int64)",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "len(sig)+scale = 20 > 19; HEAD already returns Float"
          },
          {
            "input": "1e400, -1e400",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "HEAD errors through encoding/json (probed: 'cannot unmarshal number 1e400'); spec scenario 'Nonfinite conversion overflow remains an error'"
          },
          {
            "input": "[9007199254740993] (inside array)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:131-140 recursion; spec: 'at the root or inside an array or object'"
          },
          {
            "input": "{\"v\":9223372036854775807} (inside object)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:119-130; spec: 'at the root or inside an array or object'"
          },
          {
            "input": "(json/decode (json/encode 9007199254740993)) round trip",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Round-trip preserves structure ... every integer's exact value'; proposal.md:3"
          }
        ],
        "forbidden": [
          "Deciding integrality or range from the rounded float -- the x == float64(int64(x)) test at plugins/json/plugin.go:113 must not survive in any form.",
          "Narrowing an out-of-int64 whole number into Int (9223372036854775808 must never become an Int).",
          "Accumulating the negative endpoint in int64: 9223372036854775808 overflows; magnitude accumulates in uint64 and converts as int64(-mag).",
          "Publishing a nonfinite Float (+Inf, -Inf, NaN) instead of the overflow error.",
          "Leaving two live numeric branches in fromJSONValue -- the float64 case is removed when the stdjson.Number case lands.",
          "A red test naming any new internal production symbol (classifier function, decoder type, constant)."
        ],
        "seeding": [
          "exact-int / float-fallback: env := setupEnv(t) (plugins/json/json_test.go:19-31), then eval(t, env, `(json/decode `+strconv.Quote(src)+`)`) (json_test.go:33-43). Quote the JSON with strconv.Quote as json_test.go:719 does -- never hand-escape.",
          "overflow-error: evalErr(t, env, ...) (plugins/json/json_test.go:45-57); assert the error is non-nil and contains 'json/decode:'.",
          "round trip: eval the composed form `(json/decode (json/encode 9007199254740993))` -- json/encode of core.Int marshals exactly.",
          "Forbidden seeding: constructing core.Int/core.Float directly and asserting on it, or calling any unexported classifier from a test."
        ],
        "budgets": [
          "int64 domain: -9223372036854775808 .. 9223372036854775807 inclusive.",
          "Maximum integral decimal digit count on the Int path: 19. len(sig)+scale > 19 refuses to float fallback before any accumulation.",
          "2^53 = 9007199254740992; the superseded safe bound is 9007199254740991 (plugins/json/plugin.go:113).",
          "Classifier: O(len(token)) time, O(1) extra storage beyond the token."
        ]
      },
      "redTasks": [
        "1.1"
      ],
      "codeTasks": [
        "2.1"
      ],
      "redTests": [
        "TestDecodeLargeIntegers"
      ],
      "redRun": "go test -timeout 2m -run 'TestDecodeLargeIntegers|TestDecodeErrors' ./plugins/json/",
      "verify": "go build ./plugins/json/ && go vet ./plugins/json/ && go test -timeout 2m ./plugins/json/ && golangci-lint run ./plugins/json/...",
      "coder": "go-coder"
    },
    {
      "id": "numeric-matrix",
      "taskIds": [
        "1.2",
        "2.2"
      ],
      "prev": "exact-int-endpoints",
      "sharedPkg": "plugins/json",
      "parallel": false,
      "shard": "",
      "seam": "S2-bounded-token-classification",
      "pkgDirs": [
        "plugins/json"
      ],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "1.2",
          "file": "plugins/json/json_test.go",
          "symbol": "new numeric classification matrix (add after TestDecodeLargeIntegers)",
          "anchor": "func TestDecodeLargeIntegers(t *testing.T) {",
          "change": "Add a table-driven test in the file's existing t.Run/t.Parallel shape covering: whole decimal and exponent spellings (9223372036854775807.0, -9.223372036854775808e18, 9007199254740993.0, 4.2e1) as exact core.Int; fractional values that round to an integral float (1e-400 underflow to zero, near-integer fractions) as core.Float; finite out-of-int64 fallback (9223372036854775808, -9223372036854775809) as core.Float; positive-exponent overflow (1e400) as an error; zero with a large exponent (0e1000, 0.0e-1000) as exact zero; a long exponent digit string as a bounded-work case that still returns the specified Int, Float or error."
        },
        {
          "task": "2.2",
          "file": "plugins/json/plugin.go",
          "symbol": "new numeric classifier helper (file-local, next to fromJSONValue)",
          "anchor": "func fromJSONValue(v any) (core.Value, error) {",
          "change": "Add the bounded classifier: walk the token once, tracking digit count, fractional scale and exponent with saturating accumulation clamped to a bound derived from token length and the int64 domain - never expand powers of ten, never allocate per exponent unit. Zero stays exact for any valid exponent spelling. Magnitude comparison against the signed endpoint happens before int64 accumulation so no intermediate overflows. This chunk owns the bounding proper: exponent accumulation saturates at a bound derived from token length and the fixed integer domain, with no expansion of powers of ten. If task 2.1 already satisfied the matrix, the remaining work is still this bound plus its assertions — a chunk closing with no code change means the boundedness requirement went unimplemented."
        }
      ],
      "contract": {
        "states": [
          "bounded-classification",
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "0e999999999999999999999 (21 exponent digits)",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "design.md:17 'Zero remains exact regardless of a valid exponent spelling'; probed: HEAD returns Int 0. Result: exact-int 0, reached without expanding 10^exp"
          },
          {
            "input": "1e999999999999999999999",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "probed: HEAD errors. Saturated exponent -> len(sig)+scale > 19 -> float fallback -> ParseFloat +Inf/ErrRange -> overflow-error"
          },
          {
            "input": "'1e' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "spec scenario 'Exponent magnitude does not amplify parsing work'; terminal state overflow-error, completing far inside -timeout 2m"
          },
          {
            "input": "'0e' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "spec scenario 'Exponent magnitude does not amplify parsing work'; terminal state exact-int 0"
          },
          {
            "input": "'1e-' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "strconv.ParseFloat probed: 1e-999999999999999999999 -> 0 with nil error; terminal state float-fallback Float{V:0}"
          },
          {
            "input": "'1' + 400 '0' digits (long integer digit string, no exponent)",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "401 digits > 19 -> float fallback -> ParseFloat +Inf/ErrRange; spec: 'work and storage bounded by token length'"
          },
          {
            "input": "'0.' + 400 '0' digits",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "trailing-zero stripping drives scale to 0 with sig empty -> Int 0; HEAD also returns Int 0"
          },
          {
            "input": "'0.' + 399 '0' + '1'",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "exact value has a nonzero fractional part; ParseFloat underflows to 0 -> Float{V:0}. HEAD returns Int 0"
          }
        ],
        "forbidden": [
          "Expanding 10^exponent -- no loop, allocation, or big-integer scaling proportional to exponent magnitude.",
          "Accumulating an exponent into a fixed-width integer without a saturation guard (overflow would flip a huge positive exponent negative and misclassify).",
          "Allocating storage proportional to anything but token length."
        ],
        "seeding": [
          "Build the long tokens with strings.Repeat in the test body, wrap with strconv.Quote, and drive them through eval / evalErr exactly as S1 does. No separate harness.",
          "Timing is NOT asserted -- these fixtures prove boundedness by completing inside the package's -timeout 2m; do not add a wall-clock assertion (see the TestDecodeHashMap_Scaling headroom note in S3)."
        ],
        "budgets": [
          "Exponent saturation cap = len(token) + 20. Any |exp| at or beyond it cannot change a decision, because the Int path requires len(sig)+scale <= 19 and len(sig) <= len(token).",
          "Long-exponent fixtures: 10000 exponent digits.",
          "Classifier work O(len(token)); extra storage O(1) beyond the token.",
          "Package wall clock: -timeout 2m (Makefile GOTESTFLAGS)."
        ]
      },
      "redTasks": [
        "1.2"
      ],
      "codeTasks": [
        "2.2"
      ],
      "redTests": [
        "TestDecodeNumericClassification"
      ],
      "redRun": "go test -timeout 2m -run 'TestDecodeNumericClassification' ./plugins/json/",
      "verify": "go build ./plugins/json/ && go vet ./plugins/json/ && go test -timeout 2m ./plugins/json/ && golangci-lint run ./plugins/json/...",
      "coder": "go-coder"
    },
    {
      "id": "structure-and-boundary",
      "taskIds": [
        "1.4",
        "2.3"
      ],
      "prev": "numeric-matrix",
      "sharedPkg": "plugins/json",
      "parallel": false,
      "shard": "",
      "seam": "S3-parsing-boundary-and-structure",
      "pkgDirs": [
        "plugins/json"
      ],
      "pkgs": [
        "./plugins/json"
      ],
      "sites": [
        {
          "task": "1.4",
          "file": "plugins/json/json_test.go",
          "symbol": "TestDecodeErrors",
          "anchor": "func TestDecodeErrors(t *testing.T) {",
          "change": "Add a NEW top-level test function `TestDecodeParsingBoundary` (not an extension of TestDecodeErrors — redTests, guard and verifyRun all require the top-level name) pinning: malformed JSON, trailing whitespace accepted, a second top-level value rejected, trailing garbage rejected. These pass at HEAD and are characterization: report them in alreadyGreen, never as manufactured failures. Extend with parser-boundary subtests pinning current behavior before the decoder changes: malformed JSON, a value followed only by whitespace (must succeed), a second top-level value ('1 2'), and trailing garbage ('1 x') - the last two must fail with the 'json/decode:' prefix. These guard the single-value contract if the implementation moves to stdjson.Decoder."
        },
        {
          "task": "2.3",
          "file": "plugins/json/plugin.go",
          "symbol": "Plugin.decode - depth check and deep charge",
          "anchor": "\tif err := core.CheckConstructionDepthContextEnv(ctx, res, env); err != nil {",
          "change": "Leave the post-construction sequence intact and in order: CheckConstructionDepthContextEnv, then core.ValueDeepBytesContext, then core.ChargeEvalAllocBytes on the constructed value. The exactly-once metering refinement belongs to json-result-metering, not here."
        },
        {
          "task": "2.3",
          "file": "plugins/json/plugin.go",
          "symbol": "fromJSONValue - object and array construction",
          "anchor": "\t\t\tif err := m.Set(core.Keyword{V: k}, lv); err != nil {",
          "change": "Preserve the linear single-copy HashMap builder (core.NewHashMap() plus in-place Set before publication) and the recursive []any conversion into core.NewVector; only the numeric leaf conversion changes."
        }
      ],
      "contract": {
        "states": [
          "exact-int",
          "malformed-error",
          "trailing-input-error"
        ],
        "transitions": [
          {
            "input": "'1' followed by spaces / tab / newline",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "probed: Unmarshal accepts '1   '; Decoder second Decode returns io.EOF. spec: 'exactly one JSON value followed only by optional whitespace'"
          },
          {
            "input": "'[1,2] '",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "probed: both paths accept"
          },
          {
            "input": "'1 2' (second top-level value)",
            "state": "trailing-input-error",
            "effect": "no-op",
            "evidence": "probed: Unmarshal 'invalid character 2 after top-level value'; Decoder's second Decode returns the value, so the check must reject on a non-io.EOF outcome of ANY kind"
          },
          {
            "input": "'{\"a\":1}garbage'",
            "state": "trailing-input-error",
            "effect": "no-op",
            "evidence": "probed: Decoder's second Decode returns 'invalid character g looking for beginning of value'"
          },
          {
            "input": "'not json'",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:424-427"
          },
          {
            "input": "'{' (truncated)",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:613-617. Message text changes from 'unexpected end of JSON input' to 'unexpected EOF'; the test asserts only the 'json/decode:' prefix, so it stays green"
          },
          {
            "input": "'[1, 2,]'",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:619-623"
          },
          {
            "input": "12-key object",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:717-735 -- keyword keys, promoted map form, re-encode round trip preserved"
          },
          {
            "input": "4000-key object",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:738-790; core/types.go:947-949 names json/decode as the HashMap.Set bulk-construction path"
          },
          {
            "input": "1524-deep array nest",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:93-103 -- core.CodeResourceLimit through core.CheckConstructionDepthContextEnv (plugins/json/plugin.go:75-77), unchanged by the Decoder switch (encoding/json's own 10000 cap is identical for both entry points)"
          },
          {
            "input": "nested payload under a byte budget",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:68-91 -- deep charge and the ResourceLimit refusal stay exactly as they are; the exactly-once metering fix is out of scope"
          },
          {
            "input": "TestDecodeHashMap_Scaling's test-built intermediate representation",
            "state": "trailing-input-error",
            "effect": "forced",
            "evidence": "plugins/json/json_test.go:757-767 feeds fromJSONValue a float64 from stdjson.Unmarshal; once fromJSONValue accepts only stdjson.Number the helper MUST become stdjson.NewDecoder(strings.NewReader(jsonStr)) + d.UseNumber() + d.Decode(&raw). This edit is genuinely red (t.Fatal on 'unsupported JSON type') and belongs in the red chunk; the state name here marks the seam, not a decode result"
          }
        ],
        "forbidden": [
          "Accepting a valid prefix with trailing garbage -- exactly one io.EOF from the second Decode is the only accepting outcome.",
          "Replacing the single-copy HashMap.Set builder with Assoc-per-key (reintroduces the quadratic path core/types.go:947-949 exists to avoid).",
          "Mutating a HashMap after it has been returned to the caller.",
          "Adding per-key work to the decode path -- TestDecodeHashMap_Scaling has roughly 0.1 headroom on its 3.0 ratio.",
          "Touching the charge site (plugins/json/plugin.go:78-84): the exactly-once result-metering fix belongs to json-result-metering."
        ],
        "seeding": [
          "malformed-error / trailing-input-error: evalErr(t, env, `(json/decode `+strconv.Quote(src)+`)`) (plugins/json/json_test.go:45-57).",
          "Accepted-with-whitespace: eval(...) and assert the value, so the pin distinguishes accept from reject.",
          "depth refusal: fn := decodeGoFunc(t, env) (json_test.go:59-66) called with t.Context(), asserting core.CodeResourceLimit via errors.As -- json_test.go:93-103.",
          "deep charge: core.WithEvalResourceLimits + core.EvalMeterFrom as in json_test.go:73-90.",
          "scaling: stdjson.NewDecoder(strings.NewReader(jsonStr)) with d.UseNumber(), then fromJSONValue(raw) -- the only legal way for a test to reach the production intermediate representation."
        ],
        "budgets": [
          "TestDecodeHashMap_Scaling: 2000 vs 4000 keys, 5 samples, best-of, ratio < 3.0 (plugins/json/json_test.go:769-789). Roughly 0.1 headroom and known to fire under load and on CI runners -- do not tighten it and do not add per-key decode work. Any failure of this test during this change is a load flake until a same-machine re-run says otherwise.",
          "core.DefaultMaxStructuralDepth = 1024 (core/depth.go:9); over-deep fixture 1524 brackets (json_test.go:96).",
          "encoding/json nesting cap: 10000, identical for Unmarshal and Decoder.",
          "Accepting outcomes of the trailing check: exactly 1 (io.EOF)."
        ]
      },
      "redTasks": [
        "1.4"
      ],
      "codeTasks": [
        "2.3"
      ],
      "redTests": [
        "TestDecodeParsingBoundary"
      ],
      "redRun": "go test -timeout 2m -run 'TestDecodeParsingBoundary|TestDecodeErrors' ./plugins/json/",
      "verify": "go build ./plugins/json/ && go vet ./plugins/json/ && go test -timeout 2m ./plugins/json/ && golangci-lint run ./plugins/json/...",
      "coder": "go-coder"
    },
    {
      "id": "parity-boundary",
      "taskIds": [
        "1.3"
      ],
      "prev": "structure-and-boundary",
      "sharedPkg": null,
      "parallel": true,
      "shard": "parity",
      "seam": "S4-public-dispatch-parity",
      "pkgDirs": [
        "runtime"
      ],
      "pkgs": [
        "./runtime"
      ],
      "sites": [
        {
          "task": "1.3",
          "file": "runtime/json_integer_parity_test.go",
          "symbol": "NEW FILE - TestJSON_ExactIntegersAcrossDispatchModes",
          "anchor": "NEW FILE",
          "change": "Add a runtime-package parity test modelled on runtime/minmax_integer_parity_test.go: iterate goldenEvaluatorModes (runtime/cl_adapters_golden_test.go:94) and, per mode, build an engine loading stdlib.New() and json.New(), then drive each case through Engine.Call(ctx, \"json/decode\", core.String{...}) and Engine.Eval(ctx, \"json\", \"(json/decode ...)\"), plus a true encode/decode round trip (json/encode of a core.Int then json/decode of the result). Assert concrete numeric type (want.Type().V) and exact value in both modes and cross-check vm against tree-walker. No existing helper loads stdlib+json under both modes - newReentrantCallDepthEngine (runtime/vm_reentrant_call_depth_test.go:29) loads stdlib only; a local builder must add eng.Use(json.New()) the way runtime/dialect_native_op_test.go:230-231 does inline.",
          "new": true
        }
      ],
      "contract": {
        "states": [
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "Engine.Eval, WithTreeWalker, `(json/decode \"9007199254740993\")`",
            "state": "exact-int",
            "effect": "set",
            "evidence": "probed at HEAD ae8f8bd: returns core.Float 9007199254740992 in all four (dialect x mode) combinations"
          },
          {
            "input": "Engine.Eval, WithBytecode, same source",
            "state": "exact-int",
            "effect": "set",
            "evidence": "same probe; spec scenario 'Execution modes share numeric behavior'"
          },
          {
            "input": "Engine.Call(ctx, \"json/decode\", core.String{V:\"9223372036854775807\"}), both modes",
            "state": "exact-int",
            "effect": "set",
            "evidence": "runtime/eval.go:838 resolves the plugin binding by name; probed working in both modes at HEAD"
          },
          {
            "input": "Engine.Eval round trip `(json/decode (json/encode 9007199254740993))`, both modes",
            "state": "exact-int",
            "effect": "set",
            "evidence": "tasks.md:9 'exact integer results and concrete numeric types at both dispatch boundaries'"
          },
          {
            "input": "Engine.Eval `(json/decode \"9223372036854775808\")`, both modes",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec: both SHALL return the same value and numeric type"
          },
          {
            "input": "Engine.Eval `(json/decode \"1e400\")`, both modes",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "spec: '...or equivalent conversion error'; assert both error, not identical message text"
          }
        ],
        "forbidden": [
          "Asserting equal error strings across modes -- the spec promises an equivalent error, not identical text.",
          "Sharing one Engine across the two modes or across subtests (each arm constructs its own and registers t.Cleanup(Close)).",
          "Depending on the default dialect implicitly: pass WithDialect(clojure.Dialect()) explicitly, matching runtime/value_walk_publication_test.go:127-137. (CL was probed working too; the explicit option keeps the test out of the dialect-resolution blast radius.)"
        ],
        "seeding": [
          "eng, err := New(nil, WithTreeWalker()|WithBytecode(), WithDialect(clojure.Dialect())); require.NoError; t.Cleanup(func(){ _ = eng.Close() }); require.NoError(t, eng.Use(json.New())) -- the pattern at runtime/dialect_native_op_test.go:39-47 plus runtime/value_walk_publication_test.go:129.",
          "Mode naming: evalModeName(bytecode) (runtime/resource_limits_test.go:107-112), iterating for _, bytecode := range []bool{false, true}.",
          "stdlib is NOT required: json/encode and json/decode are self-contained. Add stdlib.New() only if a fixture needs an stdlib form.",
          "New file: runtime/json_numeric_parity_test.go, package runtime. plugins/json is already an accepted runtime test import (runtime/value_walk_publication_test.go:12, runtime/dialect_native_op_test.go:11)."
        ],
        "budgets": [
          "4 arms per case: {tree, bytecode} x {Eval, Call}.",
          "int64 endpoints and the 2^53 pair as in S1; no new numeric constants introduced here."
        ]
      },
      "redTasks": [
        "1.3"
      ],
      "codeTasks": [],
      "redTests": [
        "TestJSON_ExactIntegersAcrossDispatchModes"
      ],
      "redRun": "go test -timeout 2m -run 'TestJSON_ExactIntegersAcrossDispatchModes' ./runtime/",
      "verify": "go build ./runtime/ && go vet ./runtime/ && go test -timeout 2m ./runtime/ && golangci-lint run ./runtime/...",
      "coder": "go-coder"
    },
    {
      "id": "docs-disclosure",
      "taskIds": [
        "3.2"
      ],
      "prev": null,
      "sharedPkg": null,
      "parallel": true,
      "shard": "docs",
      "seam": "S5-type-change-disclosure",
      "pkgDirs": [],
      "pkgs": [],
      "sites": [
        {
          "task": "3.2",
          "file": "README.md",
          "symbol": "Plugins table - json row",
          "anchor": "| `json`   | active | JSON encode/decode (`plugins/json`)                              |",
          "change": "Extend the json description to state exact Int decoding for integral numbers within int64 and finite Float fallback outside that case; keep the table column alignment."
        },
        {
          "task": "3.2",
          "file": "CHANGELOG.md",
          "symbol": "[Unreleased]",
          "anchor": "## [Unreleased]",
          "change": "Add the breaking numeric type change under [Unreleased] (Keep a Changelog 1.1.0): json/decode now returns core.Int for every mathematically integral JSON number within int64, so values from 9007199254740992 upward and their negative counterparts change type from Float to Int; fractional tokens that round to a whole float stay Float; out-of-int64 whole numbers keep the finite Float fallback. Place it under the existing '### Changed' heading in the [Unreleased] section."
        }
      ],
      "contract": {
        "states": [],
        "transitions": [
          {
            "input": "openspec/specs/json-plugin/spec.md:5 (Purpose)",
            "state": "",
            "effect": "forced",
            "evidence": "'detecting whole-number JSON numbers as Int rather than Float' is the last unconditional arbitrary-range integer guarantee in the repo (grep over README.md, docs/, openspec/specs/, CLAUDE.md returns only spec.md:5, :44, :69). Lines 44 and 69 sit inside requirement blocks the delta replaces, so archive rewrites them; line 5 is outside the delta and must be edited by hand, before archive."
          },
          {
            "input": "README.md:212 (json plugin table row)",
            "state": "",
            "effect": "forced",
            "evidence": "the only README JSON description; state exact int64 integer decoding plus finite float fallback"
          },
          {
            "input": "CHANGELOG.md [Unreleased] (file line 8)",
            "state": "",
            "effect": "forced",
            "evidence": "tasks.md:21. MUST disclose BOTH breaking directions: (a) integral values from 9007199254740992 through the int64 endpoints flip Float -> Int; (b) tokens whose exact value is fractional but rounds to an integral float within +/-2^53 (e.g. 1.0000000000000000001, 1e-400) flip Int -> Float. proposal.md:8 names only direction (a)."
          }
        ],
        "forbidden": [
          "Leaving any unconditional 'whole-number JSON numbers decode as Int' claim in the repo.",
          "Disclosing only the Float -> Int direction in the changelog.",
          "Editing the delta files under openspec/changes/json-int64-decoding/ as part of this task."
        ],
        "seeding": [],
        "budgets": []
      },
      "redTasks": [],
      "codeTasks": [
        "3.2"
      ],
      "redTests": [],
      "redRun": "",
      "verify": "grep -q 'int64' CHANGELOG.md && grep -q 'json' README.md",
      "coder": "zpatcher"
    }
  ],
  "seams": [
    {
      "id": "S1-exact-integer-classification",
      "tasks": [
        "1.1",
        "1.2",
        "2.1"
      ],
      "summary": "The Int/Float decision itself: classify the exact decimal value of the token before any float conversion. Owns both breaking directions -- large integral tokens Float -> Int, and exactly-fractional tokens that round to an integral float Int -> Float.",
      "contract": {
        "states": [
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "42",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:185; plugins/json/plugin.go:113-115"
          },
          {
            "input": "-17, 0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:186-187"
          },
          {
            "input": "9007199254740991",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:480-484"
          },
          {
            "input": "9007199254740992",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/json_test.go:486-490 is the contradictory expectation and must be replaced; spec: 'either of 9007199254740992 and 9007199254740993 ... SHALL be an exact Int'"
          },
          {
            "input": "9007199254740993",
            "state": "exact-int",
            "effect": "set",
            "evidence": "proposal.md:3 reproduction returns Float 9007199254740992; probed live at HEAD ae8f8bd through Engine.Eval and Engine.Call in both modes"
          },
          {
            "input": "9007199254740992 and 9007199254740993 decoded separately must not be equal",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: 'with distinct adjacent values remaining distinct'"
          },
          {
            "input": "-9007199254740992, -9007199254740993",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: 'adjacent integers around positive and negative 2^53' (design.md:33)"
          },
          {
            "input": "9223372036854775807",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: inclusive range endpoint"
          },
          {
            "input": "-9223372036854775808",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: inclusive range endpoint; magnitude 9223372036854775808 does not fit int64 -- accumulate in uint64"
          },
          {
            "input": "42.0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD already returns Int 42 via plugins/json/plugin.go:113; spec: 'regardless of integer, decimal, or exponent spelling'"
          },
          {
            "input": "4.2e1",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD already returns Int 42; spec: exponent spelling"
          },
          {
            "input": "9223372036854775807.0",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "-9.223372036854775808e18",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "9007199254740993.0",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Whole decimal and exponent spellings remain exact'"
          },
          {
            "input": "9.2233720368547758e18 (scale > 0 accumulation, value 9223372036854775800)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec: exponent spelling within the inclusive range"
          },
          {
            "input": "-0, -0.0, 0.0, 0e0",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "HEAD returns Int 0 (plugins/json/plugin.go:113, -0.0 == float64(int64(-0.0))); design.md:17 'Zero remains exact regardless of a valid exponent spelling'. Sign is dropped: the result is Int{V:0}, not a signed zero"
          },
          {
            "input": "3.14, -2.5",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:188-189"
          },
          {
            "input": "1.0000000000000000001",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "HEAD rounds to float64 1 and returns Int 1 (plugins/json/plugin.go:113); spec scenario 'Float rounding does not change numeric classification'. THE EXACT VALUE HAS A NONZERO FRACTIONAL PART -- 'fractional' means the exact value, never the presence of a '.' in the token"
          },
          {
            "input": "1e-400 (underflow to zero)",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "HEAD returns Int 0 (strconv.ParseFloat probed: 0 with nil error, so encoding/json publishes float64 0 and plugin.go:113 narrows it); spec: 'including underflow to zero -- THEN the result SHALL remain a Float'"
          },
          {
            "input": "9223372036854775808",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec scenario 'Finite float fallback remains supported'; ParseFloat probed finite 9.223372036854776e18"
          },
          {
            "input": "-9223372036854775809",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec scenario 'Finite float fallback remains supported'"
          },
          {
            "input": "1e19 (20 integral digits, out of int64)",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "len(sig)+scale = 20 > 19; HEAD already returns Float"
          },
          {
            "input": "1e400, -1e400",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "HEAD errors through encoding/json (probed: 'cannot unmarshal number 1e400'); spec scenario 'Nonfinite conversion overflow remains an error'"
          },
          {
            "input": "[9007199254740993] (inside array)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:131-140 recursion; spec: 'at the root or inside an array or object'"
          },
          {
            "input": "{\"v\":9223372036854775807} (inside object)",
            "state": "exact-int",
            "effect": "set",
            "evidence": "plugins/json/plugin.go:119-130; spec: 'at the root or inside an array or object'"
          },
          {
            "input": "(json/decode (json/encode 9007199254740993)) round trip",
            "state": "exact-int",
            "effect": "set",
            "evidence": "spec scenario 'Round-trip preserves structure ... every integer's exact value'; proposal.md:3"
          }
        ],
        "forbidden": [
          "Deciding integrality or range from the rounded float -- the x == float64(int64(x)) test at plugins/json/plugin.go:113 must not survive in any form.",
          "Narrowing an out-of-int64 whole number into Int (9223372036854775808 must never become an Int).",
          "Accumulating the negative endpoint in int64: 9223372036854775808 overflows; magnitude accumulates in uint64 and converts as int64(-mag).",
          "Publishing a nonfinite Float (+Inf, -Inf, NaN) instead of the overflow error.",
          "Leaving two live numeric branches in fromJSONValue -- the float64 case is removed when the stdjson.Number case lands.",
          "A red test naming any new internal production symbol (classifier function, decoder type, constant)."
        ],
        "seeding": [
          "exact-int / float-fallback: env := setupEnv(t) (plugins/json/json_test.go:19-31), then eval(t, env, `(json/decode `+strconv.Quote(src)+`)`) (json_test.go:33-43). Quote the JSON with strconv.Quote as json_test.go:719 does -- never hand-escape.",
          "overflow-error: evalErr(t, env, ...) (plugins/json/json_test.go:45-57); assert the error is non-nil and contains 'json/decode:'.",
          "round trip: eval the composed form `(json/decode (json/encode 9007199254740993))` -- json/encode of core.Int marshals exactly.",
          "Forbidden seeding: constructing core.Int/core.Float directly and asserting on it, or calling any unexported classifier from a test."
        ],
        "budgets": [
          "int64 domain: -9223372036854775808 .. 9223372036854775807 inclusive.",
          "Maximum integral decimal digit count on the Int path: 19. len(sig)+scale > 19 refuses to float fallback before any accumulation.",
          "2^53 = 9007199254740992; the superseded safe bound is 9007199254740991 (plugins/json/plugin.go:113).",
          "Classifier: O(len(token)) time, O(1) extra storage beyond the token."
        ]
      }
    },
    {
      "id": "S2-bounded-token-classification",
      "tasks": [
        "2.2"
      ],
      "summary": "NO-RED-WAIVER: the bounded-classification property is asserted by S1's numeric matrix (task 1.2 -> TestDecodeNumericClassification: large exponent, long exponent digit string, zero with a large exponent), which is written and sealed by the numeric-red chunk; S2 adds no test of its own. NO-TESTER-WAIVER: its verify is the same plugins/json run. Exponent and digit-string processing bounded by token length, with saturating exponent accumulation and no power-of-ten expansion.",
      "contract": {
        "states": [
          "bounded-classification",
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "0e999999999999999999999 (21 exponent digits)",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "design.md:17 'Zero remains exact regardless of a valid exponent spelling'; probed: HEAD returns Int 0. Result: exact-int 0, reached without expanding 10^exp"
          },
          {
            "input": "1e999999999999999999999",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "probed: HEAD errors. Saturated exponent -> len(sig)+scale > 19 -> float fallback -> ParseFloat +Inf/ErrRange -> overflow-error"
          },
          {
            "input": "'1e' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "spec scenario 'Exponent magnitude does not amplify parsing work'; terminal state overflow-error, completing far inside -timeout 2m"
          },
          {
            "input": "'0e' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "spec scenario 'Exponent magnitude does not amplify parsing work'; terminal state exact-int 0"
          },
          {
            "input": "'1e-' + 10000 '9' digits",
            "state": "bounded-classification",
            "effect": "forced",
            "evidence": "strconv.ParseFloat probed: 1e-999999999999999999999 -> 0 with nil error; terminal state float-fallback Float{V:0}"
          },
          {
            "input": "'1' + 400 '0' digits (long integer digit string, no exponent)",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "401 digits > 19 -> float fallback -> ParseFloat +Inf/ErrRange; spec: 'work and storage bounded by token length'"
          },
          {
            "input": "'0.' + 400 '0' digits",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "trailing-zero stripping drives scale to 0 with sig empty -> Int 0; HEAD also returns Int 0"
          },
          {
            "input": "'0.' + 399 '0' + '1'",
            "state": "float-fallback",
            "effect": "clear",
            "evidence": "exact value has a nonzero fractional part; ParseFloat underflows to 0 -> Float{V:0}. HEAD returns Int 0"
          }
        ],
        "forbidden": [
          "Expanding 10^exponent -- no loop, allocation, or big-integer scaling proportional to exponent magnitude.",
          "Accumulating an exponent into a fixed-width integer without a saturation guard (overflow would flip a huge positive exponent negative and misclassify).",
          "Allocating storage proportional to anything but token length."
        ],
        "seeding": [
          "Build the long tokens with strings.Repeat in the test body, wrap with strconv.Quote, and drive them through eval / evalErr exactly as S1 does. No separate harness.",
          "Timing is NOT asserted -- these fixtures prove boundedness by completing inside the package's -timeout 2m; do not add a wall-clock assertion (see the TestDecodeHashMap_Scaling headroom note in S3)."
        ],
        "budgets": [
          "Exponent saturation cap = len(token) + 20. Any |exp| at or beyond it cannot change a decision, because the Int path requires len(sig)+scale <= 19 and len(sig) <= len(token).",
          "Long-exponent fixtures: 10000 exponent digits.",
          "Classifier work O(len(token)); extra storage O(1) beyond the token.",
          "Package wall clock: -timeout 2m (Makefile GOTESTFLAGS)."
        ]
      },
      "redTasks": []
    },
    {
      "id": "S3-parsing-boundary-and-structure",
      "tasks": [
        "1.4",
        "2.3"
      ],
      "summary": "NO-RED-WAIVER for task 1.4: the parser-compatibility pins (malformed input, trailing whitespace, a second value, trailing garbage) already PASS at HEAD through stdjson.Unmarshal, so they are characterization tests and must not be filed as failing red -- filing them in a red chunk trips the red-sanity gate. They are authored before C3 and must stay green across it. Task 2.3's structural invariants (linear builder, recursion, depth, deep charge) are likewise preserved, not changed. The one genuinely red item here is the test-only intermediate representation in TestDecodeHashMap_Scaling.",
      "contract": {
        "states": [
          "exact-int",
          "malformed-error",
          "trailing-input-error"
        ],
        "transitions": [
          {
            "input": "'1' followed by spaces / tab / newline",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "probed: Unmarshal accepts '1   '; Decoder second Decode returns io.EOF. spec: 'exactly one JSON value followed only by optional whitespace'"
          },
          {
            "input": "'[1,2] '",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "probed: both paths accept"
          },
          {
            "input": "'1 2' (second top-level value)",
            "state": "trailing-input-error",
            "effect": "no-op",
            "evidence": "probed: Unmarshal 'invalid character 2 after top-level value'; Decoder's second Decode returns the value, so the check must reject on a non-io.EOF outcome of ANY kind"
          },
          {
            "input": "'{\"a\":1}garbage'",
            "state": "trailing-input-error",
            "effect": "no-op",
            "evidence": "probed: Decoder's second Decode returns 'invalid character g looking for beginning of value'"
          },
          {
            "input": "'not json'",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:424-427"
          },
          {
            "input": "'{' (truncated)",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:613-617. Message text changes from 'unexpected end of JSON input' to 'unexpected EOF'; the test asserts only the 'json/decode:' prefix, so it stays green"
          },
          {
            "input": "'[1, 2,]'",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:619-623"
          },
          {
            "input": "12-key object",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:717-735 -- keyword keys, promoted map form, re-encode round trip preserved"
          },
          {
            "input": "4000-key object",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:738-790; core/types.go:947-949 names json/decode as the HashMap.Set bulk-construction path"
          },
          {
            "input": "1524-deep array nest",
            "state": "malformed-error",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:93-103 -- core.CodeResourceLimit through core.CheckConstructionDepthContextEnv (plugins/json/plugin.go:75-77), unchanged by the Decoder switch (encoding/json's own 10000 cap is identical for both entry points)"
          },
          {
            "input": "nested payload under a byte budget",
            "state": "exact-int",
            "effect": "no-op",
            "evidence": "plugins/json/json_test.go:68-91 -- deep charge and the ResourceLimit refusal stay exactly as they are; the exactly-once metering fix is out of scope"
          },
          {
            "input": "TestDecodeHashMap_Scaling's test-built intermediate representation",
            "state": "trailing-input-error",
            "effect": "forced",
            "evidence": "plugins/json/json_test.go:757-767 feeds fromJSONValue a float64 from stdjson.Unmarshal; once fromJSONValue accepts only stdjson.Number the helper MUST become stdjson.NewDecoder(strings.NewReader(jsonStr)) + d.UseNumber() + d.Decode(&raw). This edit is genuinely red (t.Fatal on 'unsupported JSON type') and belongs in the red chunk; the state name here marks the seam, not a decode result"
          }
        ],
        "forbidden": [
          "Accepting a valid prefix with trailing garbage -- exactly one io.EOF from the second Decode is the only accepting outcome.",
          "Replacing the single-copy HashMap.Set builder with Assoc-per-key (reintroduces the quadratic path core/types.go:947-949 exists to avoid).",
          "Mutating a HashMap after it has been returned to the caller.",
          "Adding per-key work to the decode path -- TestDecodeHashMap_Scaling has roughly 0.1 headroom on its 3.0 ratio.",
          "Touching the charge site (plugins/json/plugin.go:78-84): the exactly-once result-metering fix belongs to json-result-metering."
        ],
        "seeding": [
          "malformed-error / trailing-input-error: evalErr(t, env, `(json/decode `+strconv.Quote(src)+`)`) (plugins/json/json_test.go:45-57).",
          "Accepted-with-whitespace: eval(...) and assert the value, so the pin distinguishes accept from reject.",
          "depth refusal: fn := decodeGoFunc(t, env) (json_test.go:59-66) called with t.Context(), asserting core.CodeResourceLimit via errors.As -- json_test.go:93-103.",
          "deep charge: core.WithEvalResourceLimits + core.EvalMeterFrom as in json_test.go:73-90.",
          "scaling: stdjson.NewDecoder(strings.NewReader(jsonStr)) with d.UseNumber(), then fromJSONValue(raw) -- the only legal way for a test to reach the production intermediate representation."
        ],
        "budgets": [
          "TestDecodeHashMap_Scaling: 2000 vs 4000 keys, 5 samples, best-of, ratio < 3.0 (plugins/json/json_test.go:769-789). Roughly 0.1 headroom and known to fire under load and on CI runners -- do not tighten it and do not add per-key decode work. Any failure of this test during this change is a load flake until a same-machine re-run says otherwise.",
          "core.DefaultMaxStructuralDepth = 1024 (core/depth.go:9); over-deep fixture 1524 brackets (json_test.go:96).",
          "encoding/json nesting cap: 10000, identical for Unmarshal and Decoder.",
          "Accepting outcomes of the trailing check: exactly 1 (io.EOF)."
        ]
      }
    },
    {
      "id": "S4-public-dispatch-parity",
      "tasks": [
        "1.3"
      ],
      "summary": "NO-TESTER-WAIVER: task 1.3 adds a public-boundary regression whose fix lands in S1 (task 2.1); scheduled after it, the test is green on arrival and closes through verifyRun's alreadyGreen path, not through a coder of its own. Tree-walker and VM must agree on numeric decoding through the public Engine boundary. Red-stage only: no dedicated coder work -- the behavior is delivered by S1 and S2, and this seam proves neither dispatch path diverges.",
      "contract": {
        "states": [
          "exact-int",
          "float-fallback",
          "overflow-error"
        ],
        "transitions": [
          {
            "input": "Engine.Eval, WithTreeWalker, `(json/decode \"9007199254740993\")`",
            "state": "exact-int",
            "effect": "set",
            "evidence": "probed at HEAD ae8f8bd: returns core.Float 9007199254740992 in all four (dialect x mode) combinations"
          },
          {
            "input": "Engine.Eval, WithBytecode, same source",
            "state": "exact-int",
            "effect": "set",
            "evidence": "same probe; spec scenario 'Execution modes share numeric behavior'"
          },
          {
            "input": "Engine.Call(ctx, \"json/decode\", core.String{V:\"9223372036854775807\"}), both modes",
            "state": "exact-int",
            "effect": "set",
            "evidence": "runtime/eval.go:838 resolves the plugin binding by name; probed working in both modes at HEAD"
          },
          {
            "input": "Engine.Eval round trip `(json/decode (json/encode 9007199254740993))`, both modes",
            "state": "exact-int",
            "effect": "set",
            "evidence": "tasks.md:9 'exact integer results and concrete numeric types at both dispatch boundaries'"
          },
          {
            "input": "Engine.Eval `(json/decode \"9223372036854775808\")`, both modes",
            "state": "float-fallback",
            "effect": "no-op",
            "evidence": "spec: both SHALL return the same value and numeric type"
          },
          {
            "input": "Engine.Eval `(json/decode \"1e400\")`, both modes",
            "state": "overflow-error",
            "effect": "no-op",
            "evidence": "spec: '...or equivalent conversion error'; assert both error, not identical message text"
          }
        ],
        "forbidden": [
          "Asserting equal error strings across modes -- the spec promises an equivalent error, not identical text.",
          "Sharing one Engine across the two modes or across subtests (each arm constructs its own and registers t.Cleanup(Close)).",
          "Depending on the default dialect implicitly: pass WithDialect(clojure.Dialect()) explicitly, matching runtime/value_walk_publication_test.go:127-137. (CL was probed working too; the explicit option keeps the test out of the dialect-resolution blast radius.)"
        ],
        "seeding": [
          "eng, err := New(nil, WithTreeWalker()|WithBytecode(), WithDialect(clojure.Dialect())); require.NoError; t.Cleanup(func(){ _ = eng.Close() }); require.NoError(t, eng.Use(json.New())) -- the pattern at runtime/dialect_native_op_test.go:39-47 plus runtime/value_walk_publication_test.go:129.",
          "Mode naming: evalModeName(bytecode) (runtime/resource_limits_test.go:107-112), iterating for _, bytecode := range []bool{false, true}.",
          "stdlib is NOT required: json/encode and json/decode are self-contained. Add stdlib.New() only if a fixture needs an stdlib form.",
          "New file: runtime/json_numeric_parity_test.go, package runtime. plugins/json is already an accepted runtime test import (runtime/value_walk_publication_test.go:12, runtime/dialect_native_op_test.go:11)."
        ],
        "budgets": [
          "4 arms per case: {tree, bytecode} x {Eval, Call}.",
          "int64 endpoints and the 2^53 pair as in S1; no new numeric constants introduced here."
        ]
      }
    },
    {
      "id": "S5-type-change-disclosure",
      "tasks": [
        "3.2"
      ],
      "summary": "NO-RED-WAIVER: documentation and changelog only, no observable runtime contract. NO-TESTER-WAIVER: nothing to assert; verified by reading. Fully-specified mechanical edits -- a zpatcher chunk.",
      "contract": {
        "states": [],
        "transitions": [
          {
            "input": "openspec/specs/json-plugin/spec.md:5 (Purpose)",
            "state": "",
            "effect": "forced",
            "evidence": "'detecting whole-number JSON numbers as Int rather than Float' is the last unconditional arbitrary-range integer guarantee in the repo (grep over README.md, docs/, openspec/specs/, CLAUDE.md returns only spec.md:5, :44, :69). Lines 44 and 69 sit inside requirement blocks the delta replaces, so archive rewrites them; line 5 is outside the delta and must be edited by hand, before archive."
          },
          {
            "input": "README.md:212 (json plugin table row)",
            "state": "",
            "effect": "forced",
            "evidence": "the only README JSON description; state exact int64 integer decoding plus finite float fallback"
          },
          {
            "input": "CHANGELOG.md [Unreleased] (file line 8)",
            "state": "",
            "effect": "forced",
            "evidence": "tasks.md:21. MUST disclose BOTH breaking directions: (a) integral values from 9007199254740992 through the int64 endpoints flip Float -> Int; (b) tokens whose exact value is fractional but rounds to an integral float within +/-2^53 (e.g. 1.0000000000000000001, 1e-400) flip Int -> Float. proposal.md:8 names only direction (a)."
          }
        ],
        "forbidden": [
          "Leaving any unconditional 'whole-number JSON numbers decode as Int' claim in the repo.",
          "Disclosing only the Float -> Int direction in the changelog.",
          "Editing the delta files under openspec/changes/json-int64-decoding/ as part of this task."
        ],
        "seeding": [],
        "budgets": []
      }
    },
    {
      "id": "S6-run-floor-and-ordering",
      "tasks": [
        "3.1",
        "3.3"
      ],
      "summary": "NO-RED-WAIVER / NO-TESTER-WAIVER: neither task owns a stage and neither has an observable contract. 3.1 is closed by the run-level floor -- it is the floor command itself, not a chunk; recording command, result and new regression names is run bookkeeping. 3.3 is an ordering constraint on the NEXT change (json-result-metering must start only after this change is archived, per proposal.md:25 and design.md:46) and produces no code, no test and no edit inside this run; do not invent work for it.",
      "contract": {
        "states": [],
        "transitions": [],
        "forbidden": [
          "Folding the json-result-metering exactly-once fix into this run.",
          "Archiving before the floor passes."
        ],
        "seeding": [],
        "budgets": []
      }
    }
  ],
  "requirements": [
    {
      "shall": "The system SHALL implement JSON encoding and decoding with its existing container and key conversions.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "`json/decode` SHALL return an exact `Int` for every mathematically integral JSON number in the inclusive range `-9223372036854775808` through `9223372036854775807`, including decimal and exponent spellings.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "Classification SHALL use the number's exact decimal value before any rounding to a float.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "A fractional number or a number outside that integer range SHALL return a `Float` when the existing float conversion yields a finite value.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "Conversion overflow SHALL remain an error.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "Numeric-token work and storage SHALL be bounded by token length rather than the magnitude of its exponent.",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "Input SHALL contain exactly one JSON value followed only by optional whitespace; malformed input, a second value, and trailing garbage SHALL fail.",
      "tests": [
        "TestDecodeParsingBoundary",
        "TestDecodeErrors"
      ]
    },
    {
      "shall": "Decoding a JSON object SHALL scale linearly in the number of keys — it SHALL NOT rebuild-and-copy the accumulating map once per key.",
      "tests": [
        "TestDecodeHashMap_Scaling"
      ]
    },
    {
      "shall": "`json/decode` SHALL construct the resulting `HashMap` with a single-copy builder, so an n-key object decodes in O(n) rather than O(n²), while preserving the structure conversions and numeric rules in `json-plugin implementation`.",
      "tests": [
        "TestDecodeHashMap_Scaling"
      ]
    },
    {
      "shall": "Immutability SHALL hold: the in-place builder is used only before the finished map is returned to the caller, never to mutate a map that has already been exposed.",
      "tests": [
        "TestDecodeHashMap_Immutability"
      ]
    },
    {
      "shall": "`json/decode` SHALL charge the evaluation allocation ledger for the full constructed value it returns, measured deeply so nested `Vector`/`HashMap` structure counts — not by the outer container's shallow slot count.",
      "tests": [
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "A payload whose decoded structure exceeds the remaining allocation budget SHALL fail with a `ResourceLimitError` rather than returning an uncharged large structure.",
      "tests": [
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "The existing linear-decode and structure-conversion guarantees SHALL hold, with integer detection and finite float fallback governed by the numeric conversion rules in this capability.",
      "tests": [
        "TestDecodeHashMap_Scaling"
      ]
    },
    {
      "shall": "- **THEN** a valid JSON string SHALL be returned",
      "tests": [
        "TestEncode",
        "TestRoundTrip"
      ]
    },
    {
      "shall": "- **THEN** the corresponding Lisp value SHALL be returned",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** the result SHALL preserve the existing container/key conversion semantics and every integer's exact value",
      "tests": [
        "TestEncode",
        "TestRoundTrip",
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** it SHALL be returned as the exact `Int`, not `Float`, regardless of integer, decimal, or exponent spelling",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** the corresponding value SHALL be an exact `Int`, with distinct adjacent values remaining distinct",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** each SHALL produce its mathematically exact signed 64-bit `Int`",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** decoding SHALL return that `Float` rather than reject it or narrow it into `Int`",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** the result SHALL remain a `Float`",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** decoding SHALL return an error rather than publish a nonfinite numeric value",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** classification SHALL use work and storage bounded by token length, while retaining the specified integer, finite-float, or overflow result",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** only the whitespace-only suffix SHALL be accepted",
      "tests": [
        "TestDecodeParsingBoundary",
        "TestDecodeErrors"
      ]
    },
    {
      "shall": "- **THEN** both SHALL return the same value and numeric type or equivalent conversion error",
      "tests": [
        "TestJSON_ExactIntegersAcrossDispatchModes"
      ]
    },
    {
      "shall": "- **THEN** it SHALL return the corresponding `HashMap` in time proportional to the key count, not its square",
      "tests": [
        "TestDecodeHashMap_Scaling"
      ]
    },
    {
      "shall": "- **THEN** the existing container/key conversions SHALL be preserved, integral values within `int64` SHALL decode as exact `Int`, and other numbers SHALL follow the specified float fallback",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    },
    {
      "shall": "- **THEN** the ledger SHALL be charged approximately the deep size of the decoded value, not the outer container's shallow size",
      "tests": [
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "- **THEN** `json/decode` SHALL return a `ResourceLimitError`",
      "tests": [
        "TestDecodeChargesDeepResultBytes"
      ]
    },
    {
      "shall": "- **THEN** it SHALL return the corresponding value, with exact in-range integral numbers as `Int` and other supported numbers following the finite float fallback",
      "tests": [
        "TestDecodeLargeIntegers",
        "TestDecodeNumericClassification"
      ]
    }
  ],
  "testHarness": [
    "setupEnv - plugins/json/json_test.go:19 - *core.Env with stdlib.New() then json.New() initialized into it; the base fixture for every plugin-level test",
    "eval - plugins/json/json_test.go:33 - core.Read plus core.NewEvaluator().Eval of the first form, require.NoError; returns core.Value (tree-walking evaluator only, no Engine)",
    "evalErr - plugins/json/json_test.go:45 - same path but returns the error for refusal assertions",
    "decodeGoFunc - plugins/json/json_test.go:59 - pulls core.GoFunc 'json/decode' out of the env so a test can call fn.Fn(ctx, ...) with a custom metered context",
    "TestDecodeChargesDeepResultBytes - plugins/json/json_test.go:68 - ledger fixture: core.WithEvalResourceLimits(ctx, 1<<20, 1<<20), direct fn.Fn call, core.ValueDeepBytes(got) vs core.EvalMeterFrom(ctx).Snapshot().AllocationBytes, then a re-run at wantBytes-1 expecting core.CodeResourceLimit",
    "TestDecodeRejectsOverDeepJSON - plugins/json/json_test.go:93 - depth fixture: strings.Repeat(\"[\", DefaultMaxStructuralDepth+500) payload, expects a typed *core.LispicoError with CodeResourceLimit",
    "TestDecode table - plugins/json/json_test.go:176 - struct{name, jsonInput, lispExpr, expected core.Value} rows, each subtest t.Parallel, encode-then-decode compared with .Equals",
    "TestRoundTrip table - plugins/json/json_test.go:266 - struct{name, value string} rows; original vs decoded compared with .Equals",
    "TestErrors table - plugins/json/json_test.go:419 - struct{name, input, errContains string} rows asserting substring match on plain (untyped) plugin errors",
    "TestDecodeLargeIntegers - plugins/json/json_test.go:477 - two subtests, safe-range Int and outside-safe-range Float; the contradictory expectation task 1.1 replaces",
    "TestDecodeErrors - plugins/json/json_test.go:610 - parser refusal subtests (truncated object, invalid array) asserting the 'json/decode:' prefix; host for the task 1.4 boundary cases",
    "TestDecodeHashMap_Scaling buildJSON - plugins/json/json_test.go:743 - strings.Builder producing an n-key {\"k0\":0,...} object",
    "TestDecodeHashMap_Scaling timeDecode - plugins/json/json_test.go:756 - TEST-ONLY INTERMEDIATE REPRESENTATION: stdjson.Unmarshal into 'any' then a direct fromJSONValue(raw) call, timed; best-of-5 at 2000 and 4000 keys, require.Less(ratio, 3.0)",
    "TestDecodeHashMap_Immutability - plugins/json/json_test.go:792 - decoded *core.HashMap driven through Assoc/Dissoc to prove the builder never escapes",
    "goldenEvaluatorModes - runtime/cl_adapters_golden_test.go:94 - []struct{name string; opts []EngineOption} = {tree-walker: WithTreeWalker()}, {vm: WithBytecode()}; the canonical dual-mode driver",
    "newReentrantCallDepthEngine - runtime/vm_reentrant_call_depth_test.go:29 - Engine under clojure.Dialect() plus caller opts, loads stdlib.New() ONLY (no json), t.Cleanup Close",
    "newMeteringStdlibEngine - runtime/resource_limits_test.go:29 - Engine with ResourceLimits, clojure dialect and explicit WithBytecode/WithTreeWalker, stdlib only",
    "newLimitsEngine - runtime/resource_limits_test.go:22 - same shape without the metering wiring",
    "inline stdlib+json engine construction - runtime/dialect_native_op_test.go:220-231 - tw/vmEng built with New(nil, WithTreeWalker()/WithBytecode(), WithDialect(cl.Dialect())), Use(stdlib.New()) then Use(json.New()); the only existing dual-mode json engine construction, and it is inline, not a helper",
    "json engine under a metering ceiling - runtime/value_walk_publication_test.go:129 - eng.Use(json.New()) layered onto newMeteringStdlibEngine",
    "minMaxParityValueCase / minMaxParityValueCases - runtime/minmax_integer_parity_test.go:15,23 - the sibling precedent table: {name, fn, args []core.Value, src string, want core.Value} driven through Engine.Call and Engine.Eval per mode",
    "assertMinMaxValue / assertMinMaxRefusal - runtime/minmax_integer_parity_test.go:148,157 - non-aborting assertions comparing want.Type().V and .Equals, and typed *core.LispicoError code plus message"
  ],
  "floor": "make test GOTESTFLAGS=\"-timeout 2m -p 2 -parallel 2\" && make lint",
  "planReview": {
    "verdict": "pass",
    "reviewer": "zarchitect",
    "rounds": 2
  }
}
```
