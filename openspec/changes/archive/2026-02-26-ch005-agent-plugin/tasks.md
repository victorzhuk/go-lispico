# Tasks: Agent Plugin

**Change ID:** 005-agent-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1 week  
**Depends On:** Changes 1-4 (core-engine, stdlib-plugin, runtime-api, llm-plugin)

---

## Phase 1: Registry and Types (Days 1-2)

### Task 1.1: Agent Types
- [x] Define Agent struct
- [x] Define Registry struct
- [x] Define LLMCaller interface
- [x] Write tests
- **Acceptance**: Types defined

### Task 1.2: Registry Implementation
- [x] Implement Register()
- [x] Implement Get()
- [x] Implement List()
- [x] Thread safety
- [x] Write tests
- **Acceptance**: Registry works

---

## Phase 2: Agent Execution (Days 3-4)

### Task 2.1: Plugin Structure
- [x] Define Plugin struct
- [x] Implement Name() and Metadata()
- [x] Implement Init()
- [x] Write tests
- **Acceptance**: Plugin skeleton

### Task 2.2: agent/run
- [x] Implement run function
- [x] Lookup agent by ID
- [x] Call LLM
- [x] Write tests
- **Acceptance**: Single agent works

### Task 2.3: agent/run-parallel
- [x] Implement run-parallel
- [x] Use errgroup
- [x] Configure max parallel
- [x] Collect results
- [x] Write tests
- **Acceptance**: Parallel execution works

### Task 2.4: agent/run-with-ctx
- [x] Implement run-with-ctx
- [x] Merge context into prompt
- [x] Write tests
- **Acceptance**: Context injection works

---

## Phase 3: Agent Management (Day 5)

### Task 3.1: agent/list
- [x] Implement list function
- [x] Return agent IDs
- [x] Write tests
- **Acceptance**: Listing works

### Task 3.2: agent/info
- [x] Implement info function
- [x] Return agent config map
- [x] Write tests
- **Acceptance**: Info retrieval works

---

## Phase 4: Routing and Delegation (Days 6-7)

### Task 4.1: agent/route
- [x] Implement route function
- [x] Invoke routing function
- [x] Write tests
- **Acceptance**: Routing works

### Task 4.2: agent/delegate
- [x] Implement delegate function
- [x] Check whitelist
- [x] Enforce depth limit
- [x] Write tests
- **Acceptance**: Delegation safe

---

## Acceptance Criteria

- [x] defagent macro registers agents
- [x] agent/run executes single agent
- [x] agent/run-parallel runs concurrently
- [x] Parallelism limit enforced
- [x] Context cancellation works
- [x] agent/route invokes routing
- [x] agent/delegate respects whitelist
- [x] Delegation depth limit enforced
- [x] Agent registry introspection works
- [x] Example workflows documented
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)
- Change 004-llm-plugin (required)

---

**Begin implementation after Changes 1-4 are complete**
