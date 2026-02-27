# Tasks: Exec/Crypto Plugin

**Change ID:** 006c-exec-crypto-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 2-3 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Process Execution (Days 1-2)

### Task 1.1: Plugin Structure
- [x] Define Plugin struct
- [x] Implement Name() and Metadata()
- [x] Implement Init()
- [x] Write tests
- **Acceptance**: Plugin ready

### Task 1.2: exec/run
- [x] Implement run function
- [x] Handle arguments
- [x] Handle options (timeout, dir, env)
- [x] Capture stdout/stderr
- [x] Write tests
- **Acceptance**: Run works

### Task 1.3: exec/pipe
- [x] Implement pipe function
- [x] Chain commands
- [x] Connect stdin/stdout
- [x] Write tests
- **Acceptance**: Pipes work

### Task 1.4: exec/which
- [x] Implement which function
- [x] Search PATH
- [x] Write tests
- **Acceptance**: Which works

---

## Phase 2: Crypto Functions (Day 3)

### Task 2.1: crypto/sha256
- [x] Implement sha256 function
- [x] Return hex string
- [x] Write tests
- **Acceptance**: SHA256 works

### Task 2.2: crypto/uuid
- [x] Import uuid library
- [x] Implement uuid function
- [x] Return string
- [x] Write tests
- **Acceptance**: UUID generation works

---

## Phase 3: Safety (Day 3)

### Task 3.1: Timeout Handling
- [x] Default timeout
- [x] Configurable timeout
- [x] Process cleanup
- [x] Write tests
- **Acceptance**: No zombies

### Task 3.2: Context Support
- [x] Context cancellation
- [x] Process kill on cancel
- [x] Write tests
- **Acceptance**: Cancellation works

---

## Acceptance Criteria

- [x] exec/run with options working
- [x] exec/pipe chains commands
- [x] exec/which finds executables
- [x] Context cancellation kills processes
- [x] Timeout handling correct
- [x] crypto/sha256 produces correct hashes
- [x] crypto/uuid generates valid UUIDs
- [x] No zombie processes (verified)
- [x] Documentation with security notes
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
