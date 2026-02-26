# Tasks: Data Plugin

**Change ID:** 006d-data-plugin  
**Status:** Ready for Implementation  
**Created:** 2026-02-24  
**Estimated Effort:** 1-2 hours  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Plugin Structure

### Task 1.1: Create plugin.go
- [ ] Define Plugin struct
- [ ] Implement New() constructor
- [ ] Implement Name() returning "json"
- [ ] Implement Metadata() with version, description, author
- [ ] Implement Init() registering json/encode, json/decode, json/pretty-encode
- **Acceptance**: Plugin initializes and registers all three functions

---

## Phase 2: JSON Functions

### Task 2.1: Implement encode and prettyEncode
- [ ] Implement encode using core.ToGoValue + json.Marshal
- [ ] Implement prettyEncode using core.ToGoValue + json.MarshalIndent
- [ ] Context cancellation check at entry
- [ ] Arity validation (exactly 1 argument)
- [ ] Error wrapping with function name prefix
- **Acceptance**: All Lisp value types encode to correct JSON

### Task 2.2: Implement decode and fromJSONValue
- [ ] Implement decode using json.Unmarshal + fromJSONValue
- [ ] Implement fromJSONValue with integer detection from float64
- [ ] Object keys decoded as Keywords
- [ ] Arrays decoded as Vectors
- [ ] Context cancellation check at entry
- [ ] Arity and type validation
- [ ] Error wrapping with function name prefix
- **Acceptance**: All JSON types decode to correct Lisp values, integers detected

---

## Phase 3: Testing

### Task 3.1: Create data_test.go
- [ ] Setup helper with stdlib + data plugin initialization
- [ ] Table-driven encode tests (nil, bool, int, float, string, vector, list, map, nested)
- [ ] Table-driven decode tests (null, bool, int, float, string, array, object, nested)
- [ ] Round-trip tests (encode→decode preserves structure)
- [ ] Integration tests (get-in, map?, vector? on decoded JSON)
- [ ] Pretty-encode test
- [ ] Error tests (bad JSON, wrong type, unencodable, arity)
- **Acceptance**: All tests pass, coverage ≥ 85%

---

## Acceptance Criteria

- [ ] `json/encode` produces valid JSON for all Lisp value types
- [ ] `json/decode` parses JSON to correct Lisp values
- [ ] `json/pretty-encode` produces indented output
- [ ] Round-trip encode→decode preserves structure
- [ ] JSON integers decoded as Int (not Float)
- [ ] Context cancellation respected
- [ ] All error messages follow `funcname: message` pattern
- [ ] Test coverage ≥ 85%
- [ ] `go test ./plugins/data/...` passes
- [ ] `go vet ./plugins/data/...` clean

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
