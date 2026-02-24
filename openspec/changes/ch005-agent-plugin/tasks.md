# Tasks: Agent Plugin

**Change ID:** 005-agent-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1 week  
**Depends On:** Changes 1-4 (core-engine, stdlib-plugin, runtime-api, llm-plugin)

---

## Phase 1: Registry and Types (Days 1-2)

### Task 1.1: Agent Types
- [ ] Define Agent struct
- [ ] Define Registry struct
- [ ] Define LLMCaller interface
- [ ] Write tests
- **Acceptance**: Types defined

### Task 1.2: Registry Implementation
- [ ] Implement Register()
- [ ] Implement Get()
- [ ] Implement List()
- [ ] Thread safety
- [ ] Write tests
- **Acceptance**: Registry works

---

## Phase 2: Agent Execution (Days 3-4)

### Task 2.1: Plugin Structure
- [ ] Define Plugin struct
- [ ] Implement Name() and Metadata()
- [ ] Implement Init()
- [ ] Write tests
- **Acceptance**: Plugin skeleton

### Task 2.2: agent/run
- [ ] Implement run function
- [ ] Lookup agent by ID
- [ ] Call LLM
- [ ] Write tests
- **Acceptance**: Single agent works

### Task 2.3: agent/run-parallel
- [ ] Implement run-parallel
- [ ] Use errgroup
- [ ] Configure max parallel
- [ ] Collect results
- [ ] Write tests
- **Acceptance**: Parallel execution works

### Task 2.4: agent/run-with-ctx
- [ ] Implement run-with-ctx
- [ ] Merge context into prompt
- [ ] Write tests
- **Acceptance**: Context injection works

---

## Phase 3: Agent Management (Day 5)

### Task 3.1: agent/list
- [ ] Implement list function
- [ ] Return agent IDs
- [ ] Write tests
- **Acceptance**: Listing works

### Task 3.2: agent/info
- [ ] Implement info function
- [ ] Return agent config map
- [ ] Write tests
- **Acceptance**: Info retrieval works

---

## Phase 4: Routing and Delegation (Days 6-7)

### Task 4.1: agent/route
- [ ] Implement route function
- [ ] Invoke routing function
- [ ] Write tests
- **Acceptance**: Routing works

### Task 4.2: agent/delegate
- [ ] Implement delegate function
- [ ] Check whitelist
- [ ] Enforce depth limit
- [ ] Write tests
- **Acceptance**: Delegation safe

---

## Acceptance Criteria

- [ ] defagent macro registers agents
- [ ] agent/run executes single agent
- [ ] agent/run-parallel runs concurrently
- [ ] Parallelism limit enforced
- [ ] Context cancellation works
- [ ] agent/route invokes routing
- [ ] agent/delegate respects whitelist
- [ ] Delegation depth limit enforced
- [ ] Agent registry introspection works
- [ ] Example workflows documented
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)
- Change 004-llm-plugin (required)

---

**Begin implementation after Changes 1-4 are complete**
