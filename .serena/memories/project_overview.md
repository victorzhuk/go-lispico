# Project Overview: go-lispico

**Purpose:** Zero-dependency Lisp interpreter in Go for embeddable scripting

**Tech Stack:**
- Language: Go (pure stdlib, zero external deps in core)
- Workflow: OpenSpec for structured development
- Testing: Standard Go testing with table-driven patterns

**Architecture:**
- `core/` - Core interpreter (types, env, reader, eval, plugin)
- `openspec/changes/` - Change proposals and specs

**Key Principles:**
- Zero external dependencies in core
- Thread-safe with RWMutex
- Deterministic evaluation
- Tail-call optimization
- No panics in production code
