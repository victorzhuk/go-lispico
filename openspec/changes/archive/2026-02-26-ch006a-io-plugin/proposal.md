# Change Proposal: IO Plugin

**Change ID:** 006a-io-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the IO plugin for filesystem and environment variable operations. Provides configurable sandboxing for security-conscious deployments.

**Key Characteristics:**
- Filesystem operations with path sandboxing
- Environment variable read/write
- Capability-gated: `io:read`, `io:write`, `io:env-read`, `io:env-write` (capability enforcement deferred to future change; these capability names document intent)
- Three sandbox modes: strict, relaxed, none
- Path traversal protection

---

## 2. Motivation

### Problem
Production services need controlled file I/O:
- Read configuration files
- Write generated code/output
- Access environment variables
- Prevent path traversal attacks

### Solution
A capability-gated IO plugin with:
- Configurable sandbox (strict/relaxed/none)
- Explicit allow/deny lists
- Path traversal protection
- Environment variable access

### Success Metrics
- Sandboxed paths cannot escape root
- Path traversal attempts return clear errors
- All operations respect context cancellation
- Zero security vulnerabilities in audit

---

## 3. Scope

### In Scope

**Filesystem Operations**
- `fs/read-file` - read file to string
- `fs/write-file` - write string to file
- `fs/exists?` - check file existence
- `fs/ls` - list directory contents
- `fs/mkdir` - create directory
- `fs/stat` - get file metadata

**Environment Operations**
- `env/get` - read environment variable
- `env/set` - set environment variable

**Sandbox Configuration**
- Mode: strict, relaxed, none
- Allow lists for read/write paths
- Deny patterns (regex)

### Out of Scope

- Network I/O → Change 6b (net-plugin)
- Process execution → Change 6c (exec-plugin)
- File watching → Change 3 (runtime-api Watch)
- Advanced filesystem operations (symlinks, hardlinks, permissions)

---

## 4. Functional Requirements

### Filesystem

| ID | Requirement | Priority |
|----|-------------|----------|
| IO6a.1 | read-file returns string content | P0 |
| IO6a.2 | write-file creates parent directories if needed | P0 |
| IO6a.3 | exists? returns boolean | P0 |
| IO6a.4 | ls returns list of filenames | P0 |
| IO6a.5 | mkdir creates directory recursively | P0 |
| IO6a.6 | stat returns map with size, mtime, isdir | P0 |
| IO6a.7 | All operations respect context cancellation | P0 |

### Environment

| ID | Requirement | Priority |
|----|-------------|----------|
| IO6a.8 | env/get returns string or nil if unset | P0 |
| IO6a.9 | env/set sets variable for process | P0 |
| IO6a.10 | Changes visible to child processes | P1 |

### Security

| ID | Requirement | Priority |
|----|-------------|----------|
| IO6a.11 | Strict mode: all paths relative to sandbox dir | P0 |
| IO6a.12 | Path traversal blocked (../ escapes) | P0 |
| IO6a.13 | Relaxed mode: validate against allow list | P0 |
| IO6a.14 | None mode: no restrictions (host responsibility) | P0 |
| IO6a.15 | Deny patterns applied in all modes | P0 |

---

## 5. Design Philosophy

### Defense in Depth

Multiple layers of protection:
1. Capability gating (plugin loading)
2. Sandbox mode (configuration)
3. Path validation (runtime)
4. Deny patterns (runtime)

### Unix Philosophy

Mechanism, not policy:
- Host application defines security posture
- Flexible configuration per deployment
- Production: strict + allow lists
- Development: relaxed or none

### Fail Secure

Security violations return errors, not warnings:
- Path traversal → `CapabilityError: path escapes sandbox`
- Capability denied → `CapabilityError: io:write not granted`
- Deny pattern match → `CapabilityError: path matches deny pattern`

---

## 6. Sandbox Modes

### Strict Mode

All paths resolved relative to sandbox directory:

```go
type Sandbox struct {
    RootDir string // e.g., "/app/data"
}

func (s *Sandbox) Resolve(path string) (string, error) {
    full := filepath.Join(s.RootDir, filepath.Clean(path))
    rel, err := filepath.Rel(s.RootDir, full)
    if err != nil || strings.HasPrefix(rel, "..") {
        return "", fmt.Errorf("path escapes sandbox: %s", path)
    }
    return full, nil
}
```

**Security note**: `filepath.Rel` is used instead of `strings.HasPrefix` to prevent the `/app/data2` prefix collision attack where a path like `/app/data2/secret` incorrectly passes a `HasPrefix(/app/data)` check.

### Relaxed Mode

Validate against allow/deny lists:

```go
type Sandbox struct {
    AllowRead   []string // allowed read paths/prefixes
    AllowWrite  []string // allowed write paths/prefixes
    DenyPattern []string // regex patterns to deny
}
```

### None Mode

No restrictions. Host application takes full responsibility.

---

## 7. Configuration

```go
type Config struct {
    Mode        SandboxMode    // Strict, Relaxed, None
    RootDir     string         // for Strict mode
    AllowRead   []string       // for Relaxed mode
    AllowWrite  []string       // for Relaxed mode
    DenyPattern []string       // regex patterns
}

type SandboxMode int

const (
    ModeStrict SandboxMode = iota
    ModeRelaxed
    ModeNone
)
```

---

## 8. Lisp API Reference

### fs/read-file

```lisp
(fs/read-file path) → string

; Strict mode (/app/data is sandbox root)
(fs/read-file "config.json")
; Reads /app/data/config.json

(fs/read-file "../etc/passwd")
; Error: path escapes sandbox
```

### fs/write-file

```lisp
(fs/write-file path content) → nil

(fs/write-file "output.txt" "Hello, World!")
; Creates /app/data/output.txt
; Creates parent directories if needed
```

### fs/exists?

```lisp
(fs/exists? path) → bool

(fs/exists? "config.json")
; => true or false
```

### fs/ls

```lisp
(fs/ls path) → [string]

(fs/ls ".")
; => ["file1.txt" "dir1" "file2.lisp"]

(fs/ls "dir1")
; => ["nested.txt"]
```

### fs/mkdir

```lisp
(fs/mkdir path) → nil

(fs/mkdir "newdir")
(fs/mkdir "parent/child")
; Creates all parent directories
```

### fs/stat

```lisp
(fs/stat path) → map

(fs/stat "file.txt")
; => {:size 1024
;     :mtime 1708704000
;     :isdir false}
```

### env/get

```lisp
(env/get name) → string or nil

(env/get "HOME")
; => "/home/user"

(env/get "NONEXISTENT")
; => nil
```

### env/set

```lisp
(env/set name value) → nil

(env/set "MY_VAR" "my_value")
; Sets environment variable
```

**Security**: `env/set` modifies the OS process environment for all goroutines. Use with caution in concurrent settings. This operation requires the `io:env-write` capability (future enforcement).

---

## 9. Error Handling

### Error Types

```
CapabilityError: io:read not granted
  → Plugin not loaded with capability

CapabilityError: path escapes sandbox: ../etc/passwd
  → Path traversal attempt in strict mode

CapabilityError: path not in allow list: /tmp/test
  → Path not allowed in relaxed mode

CapabilityError: path matches deny pattern: .*secret.*
  → Deny pattern matched

IOError: no such file: config.json
  → File doesn't exist (for read-file)

IOError: permission denied: /root/file
  → OS permission error
```

---

## 10. Implementation Notes

### Path Cleaning

Always use `filepath.Clean()` before validation:

```go
// Bad
full := root + "/" + path

// Good
cleaned := filepath.Clean(path)
full := filepath.Join(root, cleaned)
```

### Context Cancellation

File operations should check context:

```go
func readFile(ctx context.Context, path string) (string, error) {
    if err := ctx.Err(); err != nil {
        return "", fmt.Errorf("read file %s: %w", path, err)
    }
    data, err := os.ReadFile(path)
    if err != nil {
        return "", fmt.Errorf("read file %s: %w", path, err)
    }
    return string(data), nil
}
```

**Limitation**: Go's `os.ReadFile` does not support context cancellation at the syscall level. We check context before the call (cancels cleanly for pending operations) but cannot interrupt an in-progress read of a slow filesystem. For network filesystems, use a timeout-wrapped context from the caller.

### Security Audit Checklist

- [ ] Path traversal using `../` blocked
- [ ] Path traversal using `..\` blocked (Windows)
- [ ] Symlinks validated (target under sandbox)
- [ ] Deny patterns tested with regex injection
- [ ] Capability gating enforced
- [ ] Error messages don't leak sensitive paths

---

## 11. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| read-file (1KB) | < 1ms | Excluding disk I/O |
| write-file (1KB) | < 1ms | Excluding disk I/O |
| exists? | < 100µs | stat syscall |
| ls (100 files) | < 5ms | Directory listing |
| Path validation | < 10µs | Sandbox checks |

---

## 12. Dependencies

### External Dependencies

- `os` - File operations (stdlib)
- `path/filepath` - Path manipulation (stdlib)
- `regexp` - Pattern matching (stdlib)
- `context` - Cancellation (stdlib)

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required (str, list)
- **Change 3** (runtime-api): Required (context)

### Dependent Changes

- **Change 6c** (data-plugin): May use for JSON file I/O
- **Change 7** (fsm-plugin): May use for state persistence

---

## 13. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Path traversal vulnerability | Low | Critical | Strict validation, security audit |
| Symlink attacks | Medium | High | Resolve and validate target |
| TOCTOU race conditions | Medium | Medium | Document, accept for v1 |
| Performance with large files | Low | Low | Stream large files, document limits |

---

## 14. Acceptance Criteria

- [ ] All filesystem operations working
- [ ] Environment operations working
- [ ] Strict mode sandbox enforced
- [ ] Relaxed mode allow/deny lists working
- [ ] Path traversal blocked
- [ ] Context cancellation respected
- [ ] Security audit passed
- [ ] Documentation with security guidelines
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with Sandbox implementation, path validation logic, and security considerations.
