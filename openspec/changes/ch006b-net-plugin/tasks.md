# Tasks: Net Plugin

**Change ID:** 006b-net-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 2-3 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: HTTP Client (Day 1)

### Task 1.1: Plugin Structure
- [ ] Define Plugin struct
- [ ] Configure http.Client
- [ ] Implement Name() and Metadata()
- [ ] Write tests
- **Acceptance**: Client ready

### Task 1.2: Request Building
- [ ] Implement buildRequest()
- [ ] Handle body (string and map)
- [ ] Handle query params
- [ ] Handle headers
- [ ] Write tests
- **Acceptance**: Requests build correctly

---

## Phase 2: HTTP Functions (Day 2)

### Task 2.1: http/get
- [ ] Implement get function
- [ ] Handle options
- [ ] Write tests
- **Acceptance**: GET works

### Task 2.2: http/post
- [ ] Implement post function
- [ ] Handle JSON body
- [ ] Write tests
- **Acceptance**: POST works

### Task 2.3: http/fetch
- [ ] Implement fetch function
- [ ] Handle all HTTP methods
- [ ] Write tests
- **Acceptance**: Fetch works

---

## Phase 3: Response Handling (Day 3)

### Task 3.1: Response Parsing
- [ ] Implement doRequest()
- [ ] Parse JSON responses
- [ ] Return response map
- [ ] Write tests
- **Acceptance**: Responses parsed

### Task 3.2: Error Handling
- [ ] Network error handling
- [ ] Timeout handling
- [ ] HTTP error handling
- [ ] Write tests
- **Acceptance**: Errors handled

---

## Acceptance Criteria

- [ ] http/get working with options
- [ ] http/post with JSON body
- [ ] http/fetch with full control
- [ ] Context cancellation respected
- [ ] JSON responses parsed
- [ ] Connection reuse verified
- [ ] Timeout handling tested
- [ ] Documentation with examples
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
