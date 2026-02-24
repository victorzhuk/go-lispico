# Tasks: FSM Plugin

**Change ID:** 007-fsm-plugin  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 4-5 days  
**Depends On:** Changes 1-3 (core-engine, stdlib-plugin, runtime-api)

---

## Phase 1: Core Types (Days 1-2)

### Task 1.1: State Machine Types
- [ ] Define State type
- [ ] Define Event type
- [ ] Define StateMachine struct
- [ ] Define StateConfig struct
- [ ] Define Transition struct
- [ ] Write tests
- **Acceptance**: Types defined

### Task 1.2: Plugin Structure
- [ ] Define Plugin struct
- [ ] Implement Registry
- [ ] Implement Name() and Metadata()
- [ ] Write tests
- **Acceptance**: Structure ready

---

## Phase 2: Machine Creation (Day 2)

### Task 2.1: Machine Parsing
- [ ] Implement createMachine()
- [ ] Parse initial state
- [ ] Parse states map
- [ ] Parse transitions
- [ ] Write tests
- **Acceptance**: Machines created

### Task 2.2: Plugin Init
- [ ] Register all functions
- [ ] Write tests
- **Acceptance**: Plugin initialized

---

## Phase 3: Transitions (Days 3-4)

### Task 3.1: sm/transition
- [ ] Implement transition function
- [ ] Validate transitions
- [ ] Execute guards
- [ ] Execute actions
- [ ] Write tests
- **Acceptance**: Transitions work

### Task 3.2: Validation Functions
- [ ] Implement sm/valid?
- [ ] Implement sm/current-state
- [ ] Implement sm/reset
- [ ] Write tests
- **Acceptance**: Validation works

### Task 3.3: Introspection
- [ ] Implement sm/reachable
- [ ] Implement sm/state-machine
- [ ] Implement sm/list
- [ ] Write tests
- **Acceptance**: Introspection works

---

## Phase 4: Events (Day 4)

### Task 4.1: Event Broadcasting
- [ ] Implement StateEvent struct
- [ ] Implement broadcast()
- [ ] Global event channel
- [ ] Write tests
- **Acceptance**: Events broadcast

### Task 4.2: Callbacks
- [ ] Implement sm/on-transition
- [ ] Store callbacks
- [ ] Fire callbacks
- [ ] Write tests
- **Acceptance**: Callbacks work

---

## Phase 5: Persistence (Day 5)

### Task 5.1: Serialization
- [ ] Define PersistentState
- [ ] Implement Serialize()
- [ ] Handle version
- [ ] Write tests
- **Acceptance**: Serialization works

### Task 5.2: Deserialization
- [ ] Implement Deserialize()
- [ ] Version checking
- [ ] State restoration
- [ ] Write tests
- **Acceptance**: Deserialization works

---

## Acceptance Criteria

- [ ] State machine definition and transitions
- [ ] Transition validation works
- [ ] Guards and actions executed
- [ ] Event subscription (Go + Lisp)
- [ ] Reachability analysis
- [ ] Persistence round-trip
- [ ] Invalid transitions blocked
- [ ] Concurrent access safe
- [ ] Documentation with examples
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)
- Change 003-runtime-api (required)

---

**Begin implementation after Changes 1-3 are complete**
