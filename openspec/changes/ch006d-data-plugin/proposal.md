# Change Proposal: Data Plugin

**Change ID:** 006d-data-plugin
**Status:** Proposed → Ready for Design
**Created:** 2026-02-24
**Author:** AI Assistant
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the Data plugin for JSON encoding/decoding, deep data access, and data transformation. Every production use case requires JSON interop.

**Key Characteristics:**
- JSON encode/decode with Go value mapping
- Deep key access via `json/get-in`
- Value type predicates
- No external dependencies (stdlib `encoding/json`)

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
A slim data plugin wrapping stdlib `encoding/json` with idiomatic Lisp API.

### Success Metrics
- JSON round-trip preserves all value types
- get-in works on nested maps
- encode/decode under 1ms for 10KB payloads

---

## 3. Scope

### In Scope

**JSON Operations**
- `json/encode` - Value → JSON string
- `json/decode` - JSON string → Value
- `json/get-in` - deep access with key path

**Type Predicates**
- `json/object?` - is a HashMap
- `json/array?` - is a Vector

### Out of Scope
- JSON Schema validation → Future
- JSONPath → Future (use get-in)
- Streaming JSON → Future

---

## 4. Functional Requirements

| ID | Requirement | Priority |
|----|-------------|----------|
| D6d.1 | json/encode converts Value to JSON string | P0 |
| D6d.2 | json/decode parses JSON string to Value | P0 |
| D6d.3 | Lisp maps → JSON objects (keyword keys serialized without colon) | P0 |
| D6d.4 | Lisp vectors → JSON arrays | P0 |
| D6d.5 | JSON null → Lisp nil | P0 |
| D6d.6 | JSON booleans → Lisp booleans | P0 |
| D6d.7 | JSON numbers → Lisp int or float64 | P0 |
| D6d.8 | json/get-in returns nested value or nil | P0 |
| D6d.9 | All ops accept context | P0 |

---

## 5. Design Philosophy

### Value Mapping

| Lisp Value | JSON |
|------------|------|
| nil | null |
| Bool | boolean |
| Int | number (integer) |
| Float | number (float) |
| String | string |
| Keyword | string (without colon prefix) |
| Symbol | string |
| *HashMap | object |
| *Vector | array |

**Key serialization**: Keyword `:foo` → `"foo"` (colon stripped). On decode, JSON object keys become Keywords.

### No External Dependencies

Use only `encoding/json` from stdlib. Map Lisp values to `any` for marshaling.

---

## 6. Lisp API Reference

### json/encode

```lisp
(json/encode value) → string

(json/encode {:name "Alice" :age 30})
; => "{\"name\":\"Alice\",\"age\":30}"

(json/encode [1 2 3])
; => "[1,2,3]"

(json/encode nil)
; => "null"
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

### json/get-in

```lisp
(json/get-in value [keys]) → Value

(json/get-in {:a {:b {:c 42}}} [:a :b :c])
; => 42

(json/get-in {:a 1} [:missing])
; => nil
```

### json/object?

```lisp
(json/object? value) → bool

(json/object? {:a 1})
; => true
(json/object? [1 2])
; => false
```

### json/array?

```lisp
(json/array? value) → bool

(json/array? [1 2 3])
; => true
(json/array? {:a 1})
; => false
```

---

## 7. Implementation Notes

### Value → JSON mapping

```go
func toJSON(v core.Value) (any, error) {
    switch x := v.(type) {
    case core.Nil:
        return nil, nil
    case core.Bool:
        return bool(x), nil
    case core.Int:
        return int64(x.V), nil
    case core.Float:
        return float64(x.V), nil
    case core.String:
        return x.V, nil
    case core.Keyword:
        return strings.TrimPrefix(x.Name, ":"), nil
    case core.Symbol:
        return x.Name, nil
    case *core.HashMap:
        m := make(map[string]any)
        for k, val := range x.Pairs {
            key := fmt.Sprintf("%v", k)
            key = strings.TrimPrefix(key, ":")
            mv, err := toJSON(val)
            if err != nil {
                return nil, err
            }
            m[key] = mv
        }
        return m, nil
    case *core.Vector:
        arr := make([]any, len(x.Items))
        for i, item := range x.Items {
            mv, err := toJSON(item)
            if err != nil {
                return nil, fmt.Errorf("vector index %d: %w", i, err)
            }
            arr[i] = mv
        }
        return arr, nil
    default:
        return nil, fmt.Errorf("json/encode: unsupported type %T", v)
    }
}
```

### JSON → Value mapping

```go
func fromJSON(v any) core.Value {
    switch x := v.(type) {
    case nil:
        return core.Nil{}
    case bool:
        return core.Bool(x)
    case float64:
        if x == float64(int64(x)) {
            return core.Int{V: int64(x)}
        }
        return core.Float{V: x}
    case string:
        return core.String{V: x}
    case map[string]any:
        hm := core.NewHashMap()
        for k, val := range x {
            hm.Set(core.Keyword{Name: ":" + k}, fromJSON(val))
        }
        return hm
    case []any:
        items := make([]core.Value, len(x))
        for i, item := range x {
            items[i] = fromJSON(item)
        }
        return core.NewVector(items)
    default:
        return core.String{V: fmt.Sprintf("%v", v)}
    }
}
```

---

## 8. Error Handling

```
JSONError: json/decode: invalid character at position 5
  → Malformed JSON input

JSONError: json/encode: unsupported type *SomeType
  → Unencodable Lisp value
```

---

## 9. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| json/encode 1KB | < 500µs | stdlib encoding |
| json/decode 1KB | < 500µs | stdlib decoding |
| json/get-in depth 5 | < 10µs | map lookups |

---

## 10. Dependencies

### External Dependencies

- `encoding/json` - JSON encoding (stdlib)
- `strings` - Key manipulation (stdlib)

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required

### Dependent Changes

- **Change 4** (llm-plugin): json/encode for tool parameters
- **Change 6a** (io-plugin): json/decode for config files
- **Change 6b** (net-plugin): json/decode for HTTP responses

---

## 11. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Large JSON DoS | Low | Medium | Caller sets context timeout |
| Keyword collision | Low | Low | Document key stripping |
| Number precision | Low | Medium | int64 for integers, float64 for floats |

---

## 12. Acceptance Criteria

- [ ] json/encode produces valid JSON
- [ ] json/decode parses JSON to Lisp values
- [ ] Round-trip encode→decode preserves structure
- [ ] json/get-in navigates nested maps
- [ ] Keyword keys serialized without colon
- [ ] JSON object keys decoded as Keywords
- [ ] null ↔ nil
- [ ] Context cancellation respected
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with full encode/decode implementation and error handling.
