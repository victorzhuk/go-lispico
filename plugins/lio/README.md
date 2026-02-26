# IO Plugin

Filesystem and environment operations with sandbox security for go-lispico.

## Installation

```go
import "github.com/victorzhuk/go-lispico/plugins/lio"
```

## Usage

### Safe Mode (Recommended)

```go
plugin, err := lio.New(lio.Config{
    Mode:    lio.ModeStrict,
    RootDir: "/path/to/sandbox",
})
if err != nil {
    log.Fatal(err)
}

env := core.NewEnv(nil)
plugin.Init(env)
```

### Unsafe Mode (Development Only)

```go
plugin := lio.NewUnsafe()
```

## Sandbox Modes

### ModeStrict (Recommended for Production)

Constrains all file operations to a single root directory.

**Security Guarantees:**
- All paths are resolved to absolute form
- Path traversal (`../`) is blocked
- Symlinks are resolved and validated
- Operations outside root directory are rejected

```go
lio.Config{
    Mode:    lio.ModeStrict,
    RootDir: "/app/data",  // All ops confined to this directory
}
```

### ModeRelaxed

Allows fine-grained control with separate read/write allow lists.

**Security Guarantees:**
- Read and write paths controlled independently
- Path traversal blocked
- Symlinks resolved and validated

```go
lio.Config{
    Mode:       lio.ModeRelaxed,
    AllowRead:  []string{"/app/config", "/app/data"},
    AllowWrite: []string{"/app/data", "/app/logs"},
}
```

### ModeNone (Development Only)

No restrictions. Use only in trusted environments.

```go
lio.Config{
    Mode: lio.ModeNone,
}
```

## Security Guidelines

### 1. Always Use ModeStrict in Production

```go
// Good
plugin, _ := lio.New(lio.Config{
    Mode:    lio.ModeStrict,
    RootDir: "/app/sandbox",
})

// Bad - exposes entire filesystem
plugin := lio.NewUnsafe()
```

### 2. Use Deny Patterns for Sensitive Files

Block access to configuration files, credentials, etc.

```go
lio.Config{
    Mode:        lio.ModeStrict,
    RootDir:     "/app/data",
    DenyPattern: `\.(env|key|pem|secret)$`,
}
```

### 3. Principle of Least Privilege

Only enable the directories needed:

```go
lio.Config{
    Mode:       lio.ModeRelaxed,
    AllowRead:  []string{"/app/config"},
    AllowWrite: []string{"/app/logs"},
}
```

### 4. Context Cancellation

All operations check context before execution. Use context with timeout for untrusted code:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
```

**Limitation:** Go's `os.ReadFile` does not support context cancellation at the syscall level. Context is checked before the call (pending operations cancel cleanly) but cannot interrupt an in-progress read.

### 5. Environment Variables

`env-get` and `env-set` are not sandboxed. In production:
- Use `ModeStrict` or `ModeRelaxed` to restrict filesystem
- Be aware that environment access is unrestricted
- Consider using deny patterns for sensitive env keys if needed

## Functions

### File Operations

| Function | Args | Returns | Description |
|----------|------|---------|-------------|
| `io/read-file` | `path` | `string` | Read file contents |
| `io/write-file` | `path`, `content` | `nil` | Write string to file (creates parent dirs) |
| `io/exists?` | `path` | `bool` | Check if path exists |
| `io/ls` | `path` | `list` | List directory contents |
| `io/mkdir` | `path` | `nil` | Create directory (and parents) |
| `io/stat` | `path` | `hashmap` | File metadata (size, mtime, isdir, mode) |

### Environment Operations

| Function | Args | Returns | Description |
|----------|------|---------|-------------|
| `io/env-get` | `key` | `string` or `nil` | Get environment variable |
| `io/env-set` | `key`, `value` | `nil` | Set environment variable |

## Lisp Examples

```lisp
;; Read a file
(io/read-file "config.txt")

;; Write a file
(io/write-file "output.txt" "Hello, World!")

;; Check if file exists
(if (io/exists? "data.json")
  (process-data)
  (create-default-data))

;; List directory
(io/ls "/app/data")
;; => ("file1.txt" "file2.txt" "subdir")

;; Create nested directory
(io/mkdir "/app/data/logs/archive")

;; Get file info
(def info (io/stat "document.pdf"))
(get info :size)   ;; => 1024
(get info :isdir)  ;; => false
(get info :mtime)  ;; => 1709000000

;; Environment variables
(io/env-get "HOME")           ;; => "/home/user"
(io/env-set "DEBUG" "true")   ;; => nil
```

## Error Handling

All functions return errors with context:

```lisp
(io/read-file "/nonexistent")
;; Error: io/read-file: open /nonexistent: no such file or directory

(io/read-file "/etc/passwd")  ;; In strict mode with different root
;; Error: io/read-file: path outside sandbox root: /etc/passwd
```
