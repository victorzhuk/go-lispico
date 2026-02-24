# Tasks: Exec/Crypto Plugin

**Change ID:** 006c-exec-crypto-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 2-3 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Process Execution (Days 1-2)

### Task 1.1: Plugin Structure
- [ ] Define Plugin struct
- [ ] Implement Name() and Metadata()
- [ ] Implement Init()
- [ ] Write tests
- **Acceptance**: Plugin ready

### Task 1.2: exec/run
- [ ] Implement run function
- [ ] Handle arguments
- [ ] Handle options (timeout, dir, env)
- [ ] Capture stdout/stderr
- [ ] Write tests
- **Acceptance**: Run works

### Task 1.3: exec/pipe
- [ ] Implement pipe function
- [ ] Chain commands
- [ ] Connect stdin/stdout
- [ ] Write tests
- **Acceptance**: Pipes work

### Task 1.4: exec/which
- [ ] Implement which function
- [ ] Search PATH
- [ ] Write tests
- **Acceptance**: Which works

---

## Phase 2: Crypto Functions (Day 3)

### Task 2.1: crypto/sha256
- [ ] Implement sha256 function
- [ ] Return hex string
- [ ] Write tests
- **Acceptance**: SHA256 works

### Task 2.2: crypto/uuid
- [ ] Import uuid library
- [ ] Implement uuid function
- [ ] Return string
- [ ] Write tests
- **Acceptance**: UUID generation works

---

## Phase 3: Safety (Day 3)

### Task 3.1: Timeout Handling
- [ ] Default timeout
- [ ] Configurable timeout
- [ ] Process cleanup
- [ ] Write tests
- **Acceptance**: No zombies

### Task 3.2: Context Support
- [ ] Context cancellation
- [ ] Process kill on cancel
- [ ] Write tests
- **Acceptance**: Cancellation works

---

## Acceptance Criteria

- [ ] exec/run with options working
- [ ] exec/pipe chains commands
- [ ] exec/which finds executables
- [ ] Context cancellation kills processes
- [ ] Timeout handling correct
- [ ] crypto/sha256 produces correct hashes
- [ ] crypto/uuid generates valid UUIDs
- [ ] No zombie processes (verified)
- [ ] Documentation with security notes
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
