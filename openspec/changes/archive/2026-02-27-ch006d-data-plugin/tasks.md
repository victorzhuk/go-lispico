# Tasks: Data Plugin

**Change ID:** 006d-data-plugin  
**Status:** Ready for Implementation  
**Created:** 2026-02-24  
**Estimated Effort:** 1-2 hours  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Plugin Structure

### Task 1.1: Create plugin.go
- [x] Define Plugin struct
- [x] Implement New() constructor
- [x] Implement Name() returning "json"
- [x] Implement Metadata() with version, description, author
- [x] Implement Init() registering json/encode, json/decode, json/pretty-encode
- **Acceptance**: Plugin initializes and registers all three functions

---

## Phase 2: JSON Functions

### Task 2.1: Implement encode and prettyEncode
- [x] Implement encode using core.ToGoValue + json.Marshal
- [x] Implement prettyEncode using core.ToGoValue + json.MarshalIndent
- [x] Context cancellation check at entry
- [x] Arity validation (exactly 1 argument)
- [x] Error wrapping with function name prefix
- **Acceptance**: All Lisp value types encode to correct JSON

### Task 2.2: Implement decode and fromJSONValue
- [x] Implement decode using json.Unmarshal + fromJSONValue
- [x] Implement fromJSONValue with integer detection from float64
- [x] Object keys decoded as Keywords
- [x] Arrays decoded as Vectors
- [x] Context cancellation check at entry
- [x] Arity and type validation
- [x] Error wrapping with function name prefix
- **Acceptance**: All JSON types decode to correct Lisp values, integers detected

---

## Phase 3: Testing

### Task 3.1: Create data_test.go
- [x] Setup helper with stdlib + data plugin initialization
- [x] Table-driven encode tests (nil, bool, int, float, string, vector, list, map, nested)
- [x] Table-driven decode tests (null, bool, int, float, string, array, object, nested)
- [x] Round-trip tests (encode→decode preserves structure)
- [x] Integration tests (get-in, map?, vector? on decoded JSON)
- [x] Pretty-encode test
- [x] Error tests (bad JSON, wrong type, unencodable, arity)
- **Acceptance**: All tests pass, coverage ≥ 85%

---

## Acceptance Criteria

- [x] `json/encode` produces valid JSON for all Lisp value types
- [x] `json/decode` parses JSON to correct Lisp values
- [x] `json/pretty-encode` produces indented output
- [x] Round-trip encode→decode preserves structure
- [x] JSON integers decoded as Int (not Float)
- [x] Context cancellation respected
- [x] All error messages follow `funcname: message` pattern
- [x] Test coverage ≥ 85%
- [x] `go test ./plugins/data/...` passes
- [x] `go vet ./plugins/data/...` clean

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
