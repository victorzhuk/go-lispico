# AGENTS.md - go-lispico

Guide for agentic coding agents working on this Go-based Lisp interpreter.

## Project Overview

**go-lispico** is a zero-dependency Lisp interpreter in Go, designed for embeddability and hot-reload scripting. Uses OpenSpec workflow for structured development.

## Build/Test/Lint Commands

### Building
```bash
# Build the entire project
go build ./...

# Build specific package
go build ./core/...

# Build with race detector
go build -race ./...
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run specific test by name
go test -run TestName ./core/...

# Run specific test function in a file
go test -run TestFunctionName ./path/to/package

# Run tests with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html

# Run benchmarks
go test -bench=. ./...
go test -bench=BenchmarkName -benchmem ./...
```

### Linting & Formatting
```bash
# Format code
go fmt ./...

# Use gofumpt for stricter formatting (if installed)
gofumpt -w .

# Run go vet
go vet ./...

# Run golangci-lint (if installed)
golangci-lint run

# Tidy modules
go mod tidy

# Verify dependencies
go mod verify
```

## Code Style Guidelines

### Naming Conventions

**Variables:** Use natural, concise names (NOT verbose AI-style)
```go
// Good
cfg, repo, srv, pool, ctx, req, resp, err, tx

// Bad
applicationConfiguration, userRepositoryService
```

**Constructors:**
- `New()` = public API
- `new*()` = internal only

**Structs:**
- Private by default: `type engine struct`
- Public only for domain: `type Value interface`

**Receivers:** Short - `e *engine`, `v Value`, `l *Lambda`

### Imports

Group with blank lines:
```go
import (
    "context"
    "fmt"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"

    "github.com/lispico/core"
)
```

### Error Handling

**Format:** lowercase, no trailing punctuation, always wrap with context

```go
// Good
return fmt.Errorf("query user %s: %w", id, err)
return fmt.Errorf("create order: %w", err)

// Bad
return fmt.Errorf("Failed to query user: %w", err)  // uppercase, verbose
return err  // no context
return fmt.Errorf("error: %w", err)  // useless context
```

**Domain errors:** Custom types (`ErrUserNotFound`)
**Repo errors:** Wrap with operation (`query user: %w`)
**UseCase errors:** Business context (`create order: %w`)

### Types & Interfaces

**Value interface** is the core abstraction:
```go
type Value interface {
    Type() Keyword
    String() string
    Equals(other Value) bool
}
```

**Interface naming:** Consumer-side, small (1-2 methods ideally)

### Code Organization

Happy path left:
```go
item, ok := cache[key]
if !ok {
    return ErrNotFound
}
return item
```

**File organization:**
1. Package declaration
2. Imports
3. Public types/interfaces
4. Public functions
5. Private helpers

### Testing

**Use table-driven tests with `t.Run`:**
```go
func TestParseNumber(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected Value
        wantErr  bool
    }{
        {"integer", "42", Int{V: 42}, false},
        {"float", "3.14", Float{V: 3.14}, false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got, err := parseNumber(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("parseNumber() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.expected) {
                t.Errorf("parseNumber() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

### Zero Comments Rule

Comments explaining WHAT code does = BAD NAMING. Fix the name instead.

```go
// Bad: comment explains obvious behavior
// Increment the counter
counter++

// Good: code is self-documenting
counter++

// Good: comment explains WHY, not what
// Compensate for off-by-one in legacy protocol v1
counter++
```

### Architecture Principles

**Dependencies flow inward only:**
```
Transport → UseCase → Domain ← Repository ← Infrastructure
```

**Key Principles:**
- Private by default (prefer pointer to private struct)
- Accept interfaces, return structs
- Interfaces at consumer side
- `ctx context.Context` first param
- Wrap errors with `%w`
- No panics in production code

### OpenSpec Workflow

This project uses OpenSpec for structured development:

```bash
# Check status of current change
openspec status

# Create new change
openspec new change "change-name"

# Continue working on change
openspec continue --change "change-name"

# Apply tasks from change
openspec apply --change "change-name"
```

Changes are located in `openspec/changes/<change-name>/`

### Pre-Commit Checklist

Before completing any task:
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] `go fmt ./...` run
- [ ] ZERO comments except WHY
- [ ] Natural variable names (not verbose AI-style)
- [ ] Errors wrapped with context (lowercase)
- [ ] Context propagation (`ctx` first)
- [ ] No magic numbers (use named constants)
- [ ] Happy path left

## Project Structure

```
core/           # Core interpreter (zero deps)
├── types.go    # Value interface and implementations
├── env.go      # Environment chain
├── reader.go   # Tokenizer and parser
├── eval.go     # Evaluator and special forms
├── plugin.go   # Plugin interface
└── error.go    # Error types

openspec/       # OpenSpec workflow
└── changes/    # Change proposals
```

## Key Constraints

1. **Zero external dependencies** in `core/` package
2. **Thread-safe** environment with RWMutex
3. **Deterministic** evaluation (no randomness, no I/O in core)
4. **Tail-call optimization** implemented via trampoline
5. **Macro expansion depth limit** (100)
