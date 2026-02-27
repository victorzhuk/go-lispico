# Change Proposal: Data Plugin

**Change ID:** 006d-data-plugin
**Status:** Ready for Development
**Created:** 2026-02-24
**Updated:** 2026-02-26
**Author:** AI Assistant
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the Data plugin for JSON encoding/decoding. Every production use case (config, HTTP, LLM, codegen) requires JSON interop.

**Key Characteristics:**
- JSON encode/decode leveraging existing `core.ToGoValue`/`core.FromGoValue`
- Pretty-print support via `json/pretty-encode`
- No external dependencies (stdlib `encoding/json`)
- No duplication — reuses stdlib's `get-in`, `map?`, `vector?`

---

## 2. Motivation

### Problem
All production use cases require JSON:
- Config loading: read JSON config files
- Net responses: decode HTTP JSON responses
- LLM requests: encode tool parameters as JSON
- IaC/codegen: emit JSON artifacts

Without JSON support, none of these use cases work end-to-end.

### Solution
A slim data plugin wrapping stdlib `encoding/json` with idiomatic Lisp API, reusing existing `core.ToGoValue`/`core.FromGoValue` converters.

### Success Metrics
- JSON round-trip preserves all value types
- Existing `get-in` works on decoded JSON structures
- encode/decode under 1ms for 10KB payloads

---

## 3. Scope

### In Scope

**JSON Operations**
- `json/encode` — Value → JSON string (compact)
- `json/decode` — JSON string → Value
- `json/pretty-encode` — Value → JSON string (indented)

### Out of Scope
- `json/get-in` — **already exists** as stdlib `get-in` (bootstrap.lisp)
- `json/object?` / `json/array?` — **already exist** as stdlib `map?` / `vector?`
- JSON Schema validation → Future
- JSONPath → Future (use `get-in`)
- Streaming JSON → Future

---

## 4. Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| D6d.1 | `json/encode` converts Value to compact JSON string | P0 |
| D6d.2 | `json/decode` parses JSON string to Value | P0 |
| D6d.3 | `json/pretty-encode` converts Value to indented JSON string | P1 |
| D6d.4 | Keyword keys serialized without colon (`Keyword{V: "foo"}` → `"foo"`) | P0 |
| D6d.5 | JSON object keys decoded as Keywords | P0 |
| D6d.6 | JSON null ↔ Lisp nil | P0 |
| D6d.7 | JSON booleans ↔ Lisp booleans | P0 |
| D6d.8 | JSON integers → Lisp Int, JSON floats → Lisp Float | P0 |
| D6d.9 | Lisp maps → JSON objects | P0 |
| D6d.10 | Lisp vectors AND lists → JSON arrays | P0 |
| D6d.11 | Context cancellation checked before encode/decode | P0 |

---

## 5. Value Mapping

Leverages existing `core.ToGoValue` and `core.FromGoValue`.

### Lisp → JSON (encode direction)

| Lisp Value | Go intermediate (`ToGoValue`) | JSON |
|------------|-------------------------------|------|
| `Nil{}` | `nil` | `null` |
| `Bool{V: true}` | `true` | `true` |
| `Int{V: 42}` | `int64(42)` | `42` |
| `Float{V: 3.14}` | `float64(3.14)` | `3.14` |
| `String{V: "hi"}` | `"hi"` | `"hi"` |
| `Keyword{V: "foo"}` | `"foo"` (V has no colon) | `"foo"` |
| `Symbol{V: "bar"}` | `"bar"` | `"bar"` |
| `*HashMap` | `map[string]any` | `{"k": v}` |
| `Vector{Items: [...]}` | `[]any` | `[...]` |
| `List{Items: [...]}` | `[]any` | `[...]` |

**Note:** `core.ToGoValue` already handles all these cases. Keyword/Symbol produce bare strings (no colon prefix) because `Keyword.V` = `"foo"`, not `":foo"`.

### JSON → Lisp (decode direction)

| JSON | Go `json.Unmarshal` | Lisp Value (`FromGoValue`) |
|------|---------------------|----------------------------|
| `null` | `nil` | `Nil{}` |
| `true`/`false` | `bool` | `Bool{V: x}` |
| `42` | `float64(42)` | **See note below** |
| `3.14` | `float64(3.14)` | `Float{V: 3.14}` |
| `"str"` | `string` | `String{V: "str"}` |
| `{...}` | `map[string]any` | `*HashMap` (keys as Keywords) |
| `[...]` | `[]any` | `Vector{Items: [...]}` |

**Integer detection:** Go's `encoding/json` unmarshals all numbers as `float64`. The plugin must detect integers: if `x == float64(int64(x))` and `x` fits in int64, produce `Int{V: int64(x)}`. The existing `core.FromGoValue` already does NOT handle this — it expects `int` or `int64` directly. The plugin adds a `fromJSONValue` wrapper that performs integer detection before delegating to `FromGoValue` for non-numeric types.

**Object key handling:** `core.FromGoValue` for `map[string]any` already produces `Keyword{V: k}` keys — exactly what we want.

---

## 6. Lisp API Reference

### json/encode

```lisp
(json/encode value) → string

(json/encode {:name "Alice" :age 30})
; => "{\"age\":30,\"name\":\"Alice\"}"

(json/encode [1 2 3])
; => "[1,2,3]"

(json/encode nil)
; => "null"

(json/encode '(1 2 3))
; => "[1,2,3]"
```

### json/decode

```lisp
(json/decode str) → Value

(json/decode "{\"name\":\"Alice\",\"age\":30}")
; => {:name "Alice" :age 30}

(json/decode "[1,2,3]")
; => [1 2 3]

(json/decode "null")
; => nil
```

### json/pretty-encode

```lisp
(json/pretty-encode value) → string

(json/pretty-encode {:name "Alice" :age 30})
; => "{\n  \"age\": 30,\n  \"name\": \"Alice\"\n}"
```

### Using existing stdlib with decoded JSON

```lisp
;; Deep access — use stdlib get-in
(get-in (json/decode "{\"a\":{\"b\":{\"c\":42}}}") [:a :b :c])
; => 42

;; Type checks — use stdlib predicates
(map? (json/decode "{\"a\":1}"))     ; => true
(vector? (json/decode "[1,2,3]"))   ; => true
```

---

## 7. File Structure

```
plugins/data/
├── plugin.go       # Plugin struct, Init, Metadata — registers json/* functions
├── json.go         # encode, decode, prettyEncode implementations
└── data_test.go    # Table-driven tests for all functions
```

---

## 8. Error Handling

All errors are lowercase, wrapped with function name context:

```
json/decode: invalid character '}' looking for beginning of value
json/encode: unsupported Lisp type: *core.Macro
json/pretty-encode: requires 1 argument, got 2
```

No custom error types needed — stdlib `fmt.Errorf` wrapping is sufficient.

---

## 9. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| json/encode 1KB | < 500µs | `ToGoValue` + `json.Marshal` |
| json/decode 1KB | < 500µs | `json.Unmarshal` + `fromJSONValue` |
| json/pretty-encode 1KB | < 500µs | `ToGoValue` + `json.MarshalIndent` |

---

## 10. Dependencies

### External Dependencies
- `encoding/json` (stdlib)

### Internal Dependencies
- **core** package: `Value`, `ToGoValue`, `FromGoValue`, `GoFunc`, `Env`, `Evaluator`
- **stdlib plugin**: required for `get-in`, `map?`, `vector?` (test-time dependency)

### Dependent Changes
- **ch006b** (net-plugin): `json/decode` for HTTP responses
- **ch006c** (exec-crypto-plugin): JSON config parsing
- **ch007** (fsm-plugin): JSON state serialization

---

## 11. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Large JSON DoS | Low | Medium | Caller sets context timeout |
| Number precision | Low | Medium | int64 for integers ≤ 2^53, float64 for rest |
| Map key ordering | N/A | None | JSON object key order is unspecified |

---

## 12. Acceptance Criteria

- [ ] `json/encode` produces valid JSON for all Lisp value types
- [ ] `json/decode` parses JSON to correct Lisp values
- [ ] `json/pretty-encode` produces indented output
- [ ] Round-trip encode→decode preserves structure
- [ ] Keyword keys serialized without colon
- [ ] JSON object keys decoded as Keywords
- [ ] JSON integers decoded as `Int` (not `Float`)
- [ ] null ↔ nil, booleans ↔ Bool
- [ ] Lists encode to JSON arrays (same as Vectors)
- [ ] Context cancellation respected
- [ ] Existing `get-in` works on decoded JSON
- [ ] Existing `map?`/`vector?` work on decoded JSON
- [ ] All error messages follow `funcname: message` pattern
- [ ] Test coverage ≥ 85%
- [ ] `go test ./plugins/data/...` passes
- [ ] `go vet ./plugins/data/...` clean

---

**Status: Ready for Development** — design and tasks are complete.
