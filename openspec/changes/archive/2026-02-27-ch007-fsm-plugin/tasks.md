# Tasks: FSM Plugin

**Change ID:** 007-fsm-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 4-5 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Core Types (Days 1-2)

### Task 1.1: State Machine Types
- [x] Define State type
- [x] Define Event type
- [x] Define StateMachine struct
- [x] Define StateConfig struct
- [x] Define Transition struct
- [x] Write tests
- **Acceptance**: Types defined

### Task 1.2: Plugin Structure
- [x] Define Plugin struct
- [x] Implement Registry
- [x] Implement Name() and Metadata()
- [x] Write tests
- **Acceptance**: Structure ready

---

## Phase 2: Machine Creation (Day 2)

### Task 2.1: Machine Parsing
- [x] Implement createMachine()
- [x] Parse initial state
- [x] Parse states map
- [x] Parse transitions
- [x] Write tests
- **Acceptance**: Machines created

### Task 2.2: Plugin Init
- [x] Register all functions
- [x] Write tests
- **Acceptance**: Plugin initialized

---

## Phase 3: Transitions (Days 3-4)

### Task 3.1: sm/transition
- [x] Implement transition function
- [x] Validate transitions
- [x] Execute guards
- [x] Execute actions
- [x] Write tests
- **Acceptance**: Transitions work

### Task 3.2: Validation Functions
- [x] Implement sm/valid?
- [x] Implement sm/current-state
- [x] Implement sm/reset
- [x] Write tests
- **Acceptance**: Validation works

### Task 3.3: Introspection
- [x] Implement sm/reachable
- [x] Implement sm/state-machine
- [x] Implement sm/list
- [x] Write tests
- **Acceptance**: Introspection works

---

## Phase 4: Events (Day 4)

### Task 4.1: Event Broadcasting
- [x] Implement StateEvent struct
- [x] Implement broadcast()
- [x] Global event channel
- [x] Write tests
- **Acceptance**: Events broadcast

### Task 4.2: Callbacks
- [x] Implement sm/on-transition
- [x] Store callbacks
- [x] Fire callbacks
- [x] Write tests
- **Acceptance**: Callbacks work

---

## Phase 5: Persistence (Day 5)

### Task 5.1: Serialization
- [x] Define PersistentState
- [x] Implement Serialize()
- [x] Handle version
- [x] Write tests
- **Acceptance**: Serialization works

### Task 5.2: Deserialization
- [x] Implement Deserialize()
- [x] Version checking
- [x] State restoration
- [x] Write tests
- **Acceptance**: Deserialization works

---

## Acceptance Criteria

- [x] State machine definition and transitions
- [x] Transition validation works
- [x] Guards and actions executed
- [x] Event subscription (Go + Lisp)
- [x] Reachability analysis
- [x] Persistence round-trip
- [x] Invalid transitions blocked
- [x] Concurrent access safe
- [x] Documentation with examples
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
