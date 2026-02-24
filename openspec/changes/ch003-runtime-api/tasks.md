# Tasks: Runtime API

**Change ID:** 003-runtime-api  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1-2 weeks  
**Depends On:** Changes 1-2 (core-engine, stdlib-plugin)

---

## Phase 1: Engine Foundation (Days 1-2)

### Task 1.1: Engine Structure
- [ ] Define Engine struct
- [ ] Implement engineConfig
- [ ] Define EngineOption functions
- [ ] Write tests
- **Acceptance**: Engine can be created with options

### Task 1.2: Engine Lifecycle
- [ ] Implement NewEngine()
- [ ] Implement Close()
- [ ] Add structured logging
- [ ] Write tests
- **Acceptance**: Engine lifecycle works correctly

---

## Phase 2: Plugin Management (Days 3-4)

### Task 2.1: Plugin Loading
- [ ] Implement LoadPlugin()
- [ ] Check capabilities before loading
- [ ] Handle load errors
- [ ] Write tests
- **Acceptance**: Plugins load correctly

### Task 2.2: Plugin Unloading
- [ ] Implement UnloadPlugin()
- [ ] Handle cleanup
- [ ] Write tests
- **Acceptance**: Plugins unload correctly

### Task 2.3: Plugin Reload
- [ ] Implement ReloadPlugin()
- [ ] Ensure atomicity
- [ ] Handle failures gracefully
- [ ] Write tests
- **Acceptance**: Reload is atomic and safe

### Task 2.4: Plugin Introspection
- [ ] Implement ListPlugins()
- [ ] Implement plugin status tracking
- [ ] Write tests
- **Acceptance**: Can query plugin status

---

## Phase 3: Evaluation API (Days 5-6)

### Task 3.1: Basic Evaluation
- [ ] Implement Eval()
- [ ] Handle tokenization errors
- [ ] Handle parse errors
- [ ] Write tests
- **Acceptance**: Eval works correctly

### Task 3.2: File Loading
- [ ] Implement EvalFile()
- [ ] Implement LoadDir()
- [ ] Handle file not found
- [ ] Write tests
- **Acceptance**: File loading works

### Task 3.3: Function Calls
- [ ] Implement Call()
- [ ] Support context cancellation
- [ ] Handle timeouts
- [ ] Write tests
- **Acceptance**: Call respects context

### Task 3.4: Bindings
- [ ] Implement Bind()
- [ ] Check namespace conflicts
- [ ] Write tests
- **Acceptance**: Bindings work correctly

### Task 3.5: Isolated Evaluation
- [ ] Implement EvalWithBindings()
- [ ] Ensure isolation
- [ ] Write tests
- **Acceptance**: Bindings don't leak

---

## Phase 4: Hot Reload (Days 7-9)

### Task 4.1: File Watcher
- [ ] Implement fileWatcher struct
- [ ] Implement scan()
- [ ] Track file mtimes
- [ ] Write tests
- **Acceptance**: Watcher detects changes

### Task 4.2: Polling Loop
- [ ] Implement watchLoop()
- [ ] Configurable interval
- [ ] Clean shutdown
- [ ] Write tests
- **Acceptance**: Polling works correctly

### Task 4.3: Reload Logic
- [ ] Implement reloadFile()
- [ ] Parse-before-eval safety
- [ ] Error isolation
- [ ] Write tests
- **Acceptance**: Errors don't crash

### Task 4.4: Engine Integration
- [ ] Implement Watch()
- [ ] Implement Stop()
- [ ] Handle concurrent access
- [ ] Write tests
- **Acceptance**: Hot reload works end-to-end

---

## Phase 5: REPL (Days 10-11)

### Task 5.1: Basic REPL
- [ ] Implement REPL loop
- [ ] Handle input/output
- [ ] Print prompt
- [ ] Write tests
- **Acceptance**: REPL runs

### Task 5.2: Multi-line Input
- [ ] Implement isBalanced()
- [ ] Handle incomplete expressions
- [ ] Continue prompt
- [ ] Write tests
- **Acceptance**: Multi-line works

### Task 5.3: Special Commands
- [ ] Handle (exit) command
- [ ] Handle Ctrl+D
- [ ] Handle ,quit
- [ ] Write tests
- **Acceptance**: Exit works

### Task 5.4: Error Recovery
- [ ] Recover from errors
- [ ] Print error messages
- [ ] Continue after error
- [ ] Write tests
- **Acceptance**: REPL is resilient

---

## Phase 6: Observability (Days 12-13)

### Task 6.1: Statistics
- [ ] Implement Stats struct
- [ ] Track evaluations
- [ ] Track errors
- [ ] Write tests
- **Acceptance**: Stats are accurate

### Task 6.2: Event Callbacks
- [ ] Implement OnEval()
- [ ] Implement OnPluginCall()
- [ ] Fire events correctly
- [ ] Write tests
- **Acceptance**: Callbacks work

### Task 6.3: Structured Logging
- [ ] Add slog throughout
- [ ] Log levels (DEBUG, INFO, WARN, ERROR)
- [ ] Consistent field names
- [ ] Write tests
- **Acceptance**: Logging works

---

## Phase 7: Integration (Days 14)

### Task 7.1: Integration Tests
- [ ] Test full workflow
- [ ] Test hot reload under load
- [ ] Test concurrent access
- [ ] **Acceptance**: Integration tests pass

### Task 7.2: Performance Tests
- [ ] Benchmark engine creation
- [ ] Benchmark file loading
- [ ] Benchmark hot reload
- [ ] **Acceptance**: Performance targets met

---

## Acceptance Criteria

- [ ] Engine created with functional options
- [ ] Plugins loaded/unloaded/reloaded correctly
- [ ] Eval respects context cancellation
- [ ] LoadDir loads files alphabetically
- [ ] Hot-reload detects changes and updates
- [ ] Syntax errors in reload don't crash
- [ ] REPL works over stdin/stdout
- [ ] Stats and callbacks working
- [ ] Structured logging throughout
- [ ] No goroutine leaks (race detector)
- [ ] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)

---

**Begin implementation after Changes 1-2 are complete**
