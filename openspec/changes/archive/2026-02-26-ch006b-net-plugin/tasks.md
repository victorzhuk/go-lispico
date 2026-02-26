# Tasks: Net Plugin

**Change ID:** 006b-net-plugin  
**Status:** Implementation Complete  
**Created:** 2026-02-23  
**Completed:** 2026-02-26  
**Estimated Effort:** 2-3 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: HTTP Client (Day 1)

### Task 1.1: Plugin Structure
- [x] Define Plugin struct
- [x] Configure http.Client
- [x] Implement Name() and Metadata()
- [x] Write tests
- **Acceptance**: Client ready

### Task 1.2: Request Building
- [x] Implement buildRequest()
- [x] Handle body (string and map)
- [x] Handle query params
- [x] Handle headers
- [x] Write tests
- **Acceptance**: Requests build correctly

---

## Phase 2: HTTP Functions (Day 2)

### Task 2.1: http/get
- [x] Implement get function
- [x] Handle options
- [x] Write tests
- **Acceptance**: GET works

### Task 2.2: http/post
- [x] Implement post function
- [x] Handle JSON body
- [x] Write tests
- **Acceptance**: POST works

### Task 2.3: http/fetch
- [x] Implement fetch function
- [x] Handle all HTTP methods
- [x] Write tests
- **Acceptance**: Fetch works

---

## Phase 3: Response Handling (Day 3)

### Task 3.1: Response Parsing
- [x] Implement doRequest()
- [x] Parse JSON responses
- [x] Return response map
- [x] Write tests
- **Acceptance**: Responses parsed

### Task 3.2: Error Handling
- [x] Network error handling
- [x] Timeout handling
- [x] HTTP error handling
- [x] Write tests
- **Acceptance**: Errors handled

---

## Acceptance Criteria

- [x] http/get working with options
- [x] http/post with JSON body
- [x] http/fetch with full control
- [x] Context cancellation respected
- [x] JSON responses parsed
- [x] Connection reuse verified
- [x] Timeout handling tested
- [x] Documentation with examples
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

## Implementation Summary

Files created:
- `plugins/net/plugin.go` - Main plugin with New(), Name(), Metadata(), Init(), get/post/fetch functions
- `plugins/net/request.go` - buildRequest() helper
- `plugins/net/response.go` - doRequest() helper
- `plugins/net/helpers.go` - extractString(), extractInt(), extractMap(), lispToGo(), goToLisp()
- `plugins/net/net_test.go` - Comprehensive test suite

All tests pass: `go test ./plugins/net/...`
