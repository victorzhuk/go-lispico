# Change Proposal: Exec/Crypto Plugin

**Change ID:** 006c-exec-crypto-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the Exec/Crypto plugin combining subprocess execution with cryptographic utilities. Provides sandboxed command execution and common crypto operations.

**Key Characteristics:**
- Subprocess execution with timeouts
- Command piping support
- SHA256 hashing
- UUID generation
- Capability-gated: `sys:exec`, `sys:signal` (capability enforcement deferred to future change)

---

## 2. Motivation

### Problem
Services need to:
- Execute external tools (git, compilers, etc.)
- Generate unique identifiers (UUIDs)
- Hash data for integrity (SHA256)

### Solution
A combined plugin for:
- Safe subprocess execution with timeouts
- Cryptographic utilities without external deps
- Pipeline support for command chaining

### Success Metrics
- Commands timeout correctly (no zombies)
- Pipelines work (stdout → stdin)
- UUIDs are RFC 4122 compliant
- SHA256 produces correct hashes

---

## 3. Scope

### In Scope

**Subprocess Execution**
- `exec/run` - run command with args
- `exec/pipe` - pipe multiple commands
- `exec/which` - find executable in PATH

**Execution Control**
- Timeout configuration
- Working directory
- Environment variables
- Capture stdout/stderr
- Exit code access

**Cryptographic Functions**
- `crypto/sha256` - compute SHA256 hash
- `crypto/uuid` - generate UUID v4

### Out of Scope

- Encryption/decryption → Future (complex, needs key management)
- HMAC → Future (needs key management)
- Random numbers → Core or future plugin
- Process management (kill, signal) → Future

---

## 4. Functional Requirements

### Subprocess Execution

| ID | Requirement | Priority |
|----|-------------|----------|
| EC6c.1 | run executes command with args | P0 |
| EC6c.2 | Returns map with :stdout, :stderr, :exit | P0 |
| EC6c.3 | Timeout kills process if exceeded | P0 |
| EC6c.4 | Working directory configurable | P0 |
| EC6c.5 | Environment variables configurable | P0 |
| EC6c.6 | Context cancellation kills process | P0 |

### Piping

| ID | Requirement | Priority |
|----|-------------|----------|
| EC6c.7 | pipe chains multiple commands | P0 |
| EC6c.8 | Stdout of prev → stdin of next | P0 |
| EC6c.9 | Returns result of final command | P0 |
| EC6c.10 | Any command failure stops pipeline | P0 |

### Utilities

| ID | Requirement | Priority |
|----|-------------|----------|
| EC6c.11 | which finds executable in PATH | P0 |
| EC6c.12 | Returns full path or nil | P0 |

### Cryptography

| ID | Requirement | Priority |
|----|-------------|----------|
| EC6c.13 | sha256 computes hex digest | P0 |
| EC6c.14 | uuid generates RFC 4122 v4 | P0 |
| EC6c.15 | UUID returns string format | P0 |

---

## 5. Design Philosophy

### Safety First

Subprocess execution is inherently risky:
- Timeout prevents runaway processes
- Context cancellation propagates to child
- No shell interpretation (no `sh -c`)
- Argument list, not command string

### No Shell

Commands are executed directly, not via shell:

```lisp
; Good: direct execution
(exec/run "git" ["clone" "https://github.com/user/repo.git"])

; Bad: would require shell, not supported
(exec/run "sh" ["-c" "git clone repo && cd repo && make"])
```

**Rationale**: Shell injection protection, explicit control.

### Timeout by Default

All commands have timeout (default 30s):

```lisp
(exec/run "long-command" [] {:timeout 60000})  ; 60 seconds
```

No timeout = potential resource leak.

---

## 6. Lisp API Reference

### exec/run

```lisp
(exec/run cmd args opts?) → map

; Simple
(exec/run "echo" ["Hello"])
; => {:stdout "Hello\n" :stderr "" :exit 0}

; With options
(exec/run "make" ["build"]
  {:timeout 60000
   :dir "/project"
   :env {"GOOS" "linux" "GOARCH" "amd64"}})
; => {:stdout "Building...\nDone\n" :stderr "" :exit 0}

; Error case
(exec/run "false" [])
; => {:stdout "" :stderr "" :exit 1}

; Timeout
(exec/run "sleep" ["10"] {:timeout 1000})
; => throws TimeoutError (process killed)
```

### exec/pipe

```lisp
(exec/pipe commands opts?) → map

; commands is vector of [cmd args] pairs
(exec/pipe
  [["cat" ["file.txt"]]
   ["grep" ["pattern"]]
   ["wc" ["-l"]]])
; => {:stdout "5\n" :stderr "" :exit 0}

; Equivalent to: cat file.txt | grep pattern | wc -l
```

### exec/which

```lisp
(exec/which cmd) → string or nil

(exec/which "git")
; => "/usr/bin/git"

(exec/which "nonexistent")
; => nil
```

### crypto/sha256

```lisp
(crypto/sha256 data) → string

(crypto/sha256 "Hello, World!")
; => "dffd6021bb2bd5b0af676290809ec3a53191dd81c7f70a4b28688a362182986f"
```

### crypto/uuid

```lisp
(crypto/uuid) → string

(crypto/uuid)
; => "550e8400-e29b-41d4-a716-446655440000"
```

---

## 7. Implementation Notes

### Command Execution

```go
func (p *Plugin) Run(ctx context.Context, cmd string, args []string, opts map[string]Value) (map[string]Value, error) {
    if timeoutMs, ok := opts["timeout"]; ok {
        timeout := time.Duration(toInt(timeoutMs)) * time.Millisecond
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, timeout)
        defer cancel()
    }

    command := exec.CommandContext(ctx, cmd, args...)

    if dir, ok := opts["dir"]; ok {
        command.Dir = toString(dir)
    }
    if env, ok := opts["env"]; ok {
        command.Env = buildEnv(toMap(env))
    }

    var stdout, stderr bytes.Buffer
    command.Stdout = &stdout
    command.Stderr = &stderr

    if err := command.Run(); err != nil {
        var exitErr *exec.ExitError
        if !errors.As(err, &exitErr) {
            return nil, fmt.Errorf("exec run %s: %w", cmd, err)
        }
    }

    return map[string]Value{
        "stdout": String{V: stdout.String()},
        "stderr": String{V: stderr.String()},
        "exit":   Int{V: int64(command.ProcessState.ExitCode())},
    }, nil
}
```

Note: "The timeout context is created BEFORE building the command so that both Dir/Env and timeout apply to the same command instance."

### Pipeline

```go
func (p *Plugin) Pipe(ctx context.Context, commands [][]Value, opts map[string]Value) (map[string]Value, error) {
    if len(commands) == 0 {
        return nil, fmt.Errorf("pipe: empty command list")
    }

    cmds := make([]*exec.Cmd, len(commands))
    for i, cmdSpec := range commands {
        cmds[i] = exec.CommandContext(ctx, toString(cmdSpec[0]), toStringSlice(cmdSpec[1])...)
    }

    for i := 0; i < len(cmds)-1; i++ {
        pipe, err := cmds[i].StdoutPipe()
        if err != nil {
            return nil, fmt.Errorf("pipe setup stage %d: %w", i, err)
        }
        cmds[i+1].Stdin = pipe
    }

    var stdout, stderr bytes.Buffer
    cmds[len(cmds)-1].Stdout = &stdout
    cmds[len(cmds)-1].Stderr = &stderr

    for i, cmd := range cmds {
        if err := cmd.Start(); err != nil {
            return nil, fmt.Errorf("pipe start stage %d: %w", i, err)
        }
    }

    for i, cmd := range cmds {
        if err := cmd.Wait(); err != nil {
            var exitErr *exec.ExitError
            if !errors.As(err, &exitErr) {
                return nil, fmt.Errorf("pipe wait stage %d: %w", i, err)
            }
        }
    }

    return map[string]Value{
        "stdout": String{V: stdout.String()},
        "stderr": String{V: stderr.String()},
        "exit":   Int{V: int64(cmds[len(cmds)-1].ProcessState.ExitCode())},
    }, nil
}
```

### SHA256

```go
func (p *Plugin) SHA256(data string) string {
    h := sha256.New()
    h.Write([]byte(data))
    return hex.EncodeToString(h.Sum(nil))
}
```

### UUID v4

```go
func (p *Plugin) UUID() string {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
    }
    b[6] = (b[6] & 0x0f) | 0x40 // version 4
    b[8] = (b[8] & 0x3f) | 0x80 // variant bits
    return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
        b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
```

---

## 8. Error Handling

### Execution Errors

```
ExecError: command not found: git
  → Executable not in PATH

TimeoutError: command timed out after 30s
  → Timeout exceeded

ExecError: permission denied
  → Cannot execute file
```

### Pipeline Errors

```
PipelineError: command 2 failed with exit 1
  → Pipeline stopped at failing command
```

---

## 9. Security Considerations

### Command Injection Prevention

- No shell interpretation
- Arguments as list, not string
- No `;`, `&&`, `||` injection possible

### Resource Limits

- Timeout prevents infinite loops
- Context cancellation kills processes
- Memory limits via OS (cgroup) not implemented in v1

### Path Security

- Commands resolved via PATH
- Absolute paths bypass PATH lookup
- No automatic path sanitization (use io plugin's sandbox)

---

## 10. Performance Requirements

| Operation | Target | Notes |
|-----------|--------|-------|
| exec/run startup | < 10ms | Process spawn |
| exec/pipe overhead | < 5ms per command | Pipe setup |
| crypto/sha256 (1KB) | < 100µs | Native Go |
| crypto/uuid | < 10µs | Fast generation |
| Context cancellation | < 100ms | Kill process |

---

## 11. Dependencies

### External Dependencies

- `os/exec` - Command execution (stdlib)
- `crypto/sha256` - Hashing (stdlib)
- `crypto/rand` - UUID generation (stdlib)
- `encoding/hex` - Hex encoding (stdlib)
- `context` - Cancellation (stdlib)

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required
- **Change 3** (runtime-api): Required (context)

### Dependent Changes

- May be used by **Change 7** (fsm-plugin) for external triggers

---

## 12. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Zombie processes | Low | High | Proper context usage, Wait() calls |
| Resource exhaustion | Medium | Medium | Timeouts, limits on concurrent |
| Command injection | Low | Critical | No shell, args as list |
| Pipeline complexity | Medium | Low | Clear error messages |

---

## 13. Acceptance Criteria

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

**Next Step:** Create detailed design document (02-design.md) with process lifecycle management, pipeline implementation, and crypto functions.
