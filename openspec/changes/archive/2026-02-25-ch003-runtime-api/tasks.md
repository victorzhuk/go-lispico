# Tasks: Runtime API

**Change ID:** 003-runtime-api  
**Status:** Design Complete → Ready for Implementation  
**Created:** 2026-02-23  
**Estimated Effort:** 1-2 weeks  
**Depends On:** Changes 1-2 (core-engine, stdlib-plugin)

---

## Phase 1: Engine Foundation (Days 1-2)

### Task 1.1: Engine Structure
- [x] Define Engine struct
- [x] Implement engineConfig
- [x] Define EngineOption functions
- [x] Write tests
- **Acceptance**: Engine can be created with options

### Task 1.2: Engine Lifecycle
- [x] Implement NewEngine()
- [x] Implement Close()
- [x] Add structured logging
- [x] Write tests
- **Acceptance**: Engine lifecycle works correctly

---

## Phase 2: Plugin Management (Days 3-4)

### Task 2.1: Plugin Loading
- [x] Implement LoadPlugin()
- [x] Check capabilities before loading
- [x] Handle load errors
- [x] Write tests
- **Acceptance**: Plugins load correctly

### Task 2.2: Plugin Unloading
- [x] Implement UnloadPlugin()
- [x] Handle cleanup
- [x] Write tests
- **Acceptance**: Plugins unload correctly

### Task 2.3: Plugin Reload
- [x] Implement ReloadPlugin()
- [x] Ensure atomicity
- [x] Handle failures gracefully
- [x] Write tests
- **Acceptance**: Reload is atomic and safe

### Task 2.4: Plugin Introspection
- [x] Implement ListPlugins()
- [x] Implement plugin status tracking
- [x] Write tests
- **Acceptance**: Can query plugin status

---

## Phase 3: Evaluation API (Days 5-6)

### Task 3.1: Basic Evaluation
- [x] Implement Eval()
- [x] Handle tokenization errors
- [x] Handle parse errors
- [x] Write tests
- **Acceptance**: Eval works correctly

### Task 3.2: File Loading
- [x] Implement EvalFile()
- [x] Implement LoadDir()
- [x] Handle file not found
- [x] Write tests
- **Acceptance**: File loading works

### Task 3.3: Function Calls
- [x] Implement Call()
- [x] Support context cancellation
- [x] Handle timeouts
- [x] Write tests
- **Acceptance**: Call respects context

### Task 3.4: Bindings
- [x] Implement Bind()
- [x] Check namespace conflicts
- [x] Write tests
- **Acceptance**: Bindings work correctly

### Task 3.5: Isolated Evaluation
- [x] Implement EvalWithBindings()
- [x] Ensure isolation
- [x] Write tests
- **Acceptance**: Bindings don't leak

---

## Phase 4: Hot Reload (Days 7-9)

### Task 4.1: File Watcher
- [x] Implement fileWatcher struct
- [x] Implement scan()
- [x] Track file mtimes
- [x] Write tests
- **Acceptance**: Watcher detects changes

### Task 4.2: Polling Loop
- [x] Implement watchLoop()
- [x] Configurable interval
- [x] Clean shutdown
- [x] Write tests
- **Acceptance**: Polling works correctly

### Task 4.3: Reload Logic
- [x] Implement reloadFile()
- [x] Parse-before-eval safety
- [x] Error isolation
- [x] Write tests
- **Acceptance**: Errors don't crash

### Task 4.4: Engine Integration
- [x] Implement Watch()
- [x] Implement Stop()
- [x] Handle concurrent access
- [x] Write tests
- **Acceptance**: Hot reload works end-to-end

---

## Phase 5: REPL (Days 10-11)

### Task 5.1: Basic REPL
- [x] Implement REPL loop
- [x] Handle input/output
- [x] Print prompt
- [x] Write tests
- **Acceptance**: REPL runs

### Task 5.2: Multi-line Input
- [x] Implement isBalanced()
- [x] Handle incomplete expressions
- [x] Continue prompt
- [x] Write tests
- **Acceptance**: Multi-line works

### Task 5.3: Special Commands
- [x] Handle (exit) command
- [x] Handle Ctrl+D
- [x] Handle ,quit
- [x] Write tests
- **Acceptance**: Exit works

### Task 5.4: Error Recovery
- [x] Recover from errors
- [x] Print error messages
- [x] Continue after error
- [x] Write tests
- **Acceptance**: REPL is resilient

---

## Phase 6: Observability (Days 12-13)

### Task 6.1: Statistics
- [x] Implement Stats struct
- [x] Track evaluations
- [x] Track errors
- [x] Write tests
- **Acceptance**: Stats are accurate

### Task 6.2: Event Callbacks
- [x] Implement OnEval()
- [x] Implement OnPluginCall()
- [x] Fire events correctly
- [x] Write tests
- **Acceptance**: Callbacks work

### Task 6.3: Structured Logging
- [x] Add slog throughout
- [x] Log levels (DEBUG, INFO, WARN, ERROR)
- [x] Consistent field names
- [x] Write tests
- **Acceptance**: Logging works

---

## Phase 7: Integration (Days 14)

### Task 7.1: Integration Tests
- [x] Test full workflow
- [x] Test hot reload under load
- [x] Test concurrent access
- [x] **Acceptance**: Integration tests pass

### Task 7.2: Performance Tests
- [x] Benchmark engine creation
- [x] Benchmark file loading
- [x] Benchmark hot reload
- [x] **Acceptance**: Performance targets met

---

## Acceptance Criteria

- [x] Engine created with functional options
- [x] Plugins loaded/unloaded/reloaded correctly
- [x] Eval respects context cancellation
- [x] LoadDir loads files alphabetically
- [x] Hot-reload detects changes and updates
- [x] Syntax errors in reload don't crash
- [x] REPL works over stdin/stdout
- [x] Stats and callbacks working
- [x] Structured logging throughout
- [x] No goroutine leaks (race detector)
- [x] Test coverage ≥ 85%

---

## Dependencies

- Change 001-core-engine (required)
- Change 002-stdlib-plugin (required)

---

**Begin implementation after Changes 1-2 are complete**
