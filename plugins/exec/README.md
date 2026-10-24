# Exec/Crypto Plugin

Process execution and cryptographic utilities for go-lispico.

## Available Functions

### exec/run

Execute a command and capture output.

```lisp
(exec/run command args ?opts)
```

**Parameters:**
- `command` (string) - The executable name or path
- `args` (vector/list of strings) - Command arguments (optional)
- `opts` (map) - Options map (optional)

**Options:**
- `:timeout` (int) - Timeout in milliseconds (default: 30000; must be between 1 and 300000)
- `:dir` (string) - Working directory for the command
- `:env` (map) - Additional environment variables
- `:inherit-env` (bool) - Pass the full host environment to the child (default: `false`)

**Returns:** A map with:
- `:stdout` (string) - Standard output
- `:stderr` (string) - Standard error
- `:exit` (int) - Exit code (-1 if killed by timeout/cancellation)

**Examples:**
```lisp
; Simple command
(exec/run "echo" ["hello"])
; => {:stdout "hello\n" :stderr "" :exit 0}

; With timeout
(exec/run "sleep" ["10"] {:timeout 100})
; => {:stdout "" :stderr "" :exit -1}

; With working directory
(exec/run "pwd" [] {:dir "/tmp"})
; => {:stdout "/tmp\n" :stderr "" :exit 0}

; With environment variables
(exec/run "sh" ["-c" "echo $MY_VAR"] {:env {"MY_VAR" "test"}})
; => {:stdout "test\n" :stderr "" :exit 0}
```

### exec/pipe

Chain multiple commands with pipes.

```lisp
(exec/pipe commands ?opts)
```

**Parameters:**
- `commands` (vector/list of vectors) - List of commands, each `[name arg1 arg2 ...]`
- `opts` (map) - Options map (optional)

**Options:**
- `:timeout` (int) - Timeout in milliseconds (default: 30000; must be between 1 and 300000)

**Returns:** A map with `:stdout`, `:stderr`, `:exit` (same as `exec/run`)

**Examples:**
```lisp
; Pipe echo to tr
(exec/pipe [["echo" "hello world"] ["tr" "a-z" "A-Z"]])
; => {:stdout "HELLO WORLD\n" :stderr "" :exit 0}

; Single command (no piping)
(exec/pipe [["ls" "-la"]])
; => {:stdout "..." :stderr "" :exit 0}

; Three-command pipeline
(exec/pipe [["cat" "file.txt"] ["grep" "error"] ["wc" "-l"]])
```

### exec/which

Find the full path to an executable.

```lisp
(exec/which command)
```

**Parameters:**
- `command` (string) - The executable name to search for

**Returns:** String path if found, `nil` otherwise

**Examples:**
```lisp
(exec/which "ls")
; => "/usr/bin/ls"

(exec/which "nonexistent-command")
; => nil
```

### crypto/sha256

Compute SHA-256 hash of a string.

```lisp
(crypto/sha256 data)
```

**Parameters:**
- `data` (string) - The string to hash

**Returns:** Hex-encoded SHA-256 hash (64 characters)

**Examples:**
```lisp
(crypto/sha256 "hello")
; => "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

(crypto/sha256 "")
; => "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
```

### crypto/uuid

Generate a random UUID v4.

```lisp
(crypto/uuid)
```

**Returns:** UUID string in format `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx`

**Examples:**
```lisp
(crypto/uuid)
; => "550e8400-e29b-41d4-a716-446655440000"
```

## Security Notes

### No Shell Interpretation

Commands are executed directly without shell interpretation. This prevents:
- Shell injection attacks
- Unexpected glob expansion
- Variable substitution by the shell

```lisp
; Safe - no shell interpretation
(exec/run "echo" ["$HOME"])
; => {:stdout "$HOME\n" ...}  ; literal $HOME, not expanded
```

### Timeout Handling

All commands have a default 30-second timeout. `:timeout` must be between
1 and 300000 ms (1 ms to 5 minutes); values outside this window are rejected
with an error before any process is started. Long-running commands should
specify an explicit timeout:

```lisp
(exec/run "long-process" [] {:timeout 60000})  ; 60 seconds
```

When a timeout occurs, the command context requests process termination.
An executed command killed by its timeout reports exit code -1.

### Context Cancellation

Processes respect Go context cancellation. If the parent context is cancelled, running processes are terminated.

### Process Cleanup

The plugin waits for `exec/run` to finish. A successful pipeline waits for each
stage. If a later pipeline stage fails to start, the plugin kills and waits for
the stages already started. A pipeline that returns early on a stage error does
not guarantee that every remaining stage was waited for.

### Environment Isolation

By default a child process sees only `PATH` and `HOME` plus any variables you pass
in `:env`. The embedder's own environment — which may hold API keys, cloud
credentials, or other secrets — is **not** handed to executed commands.

```lisp
; Default: only PATH, HOME, and :env vars reach the child
(exec/run "sh" ["-c" "echo $HOME:$SOME_HOST_SECRET"])
; => {:stdout "/home/user:\n" ...}  ; host secret is empty

; Extra variables for this command only
(exec/run "sh" ["-c" "echo $MY_VAR"] {:env {"MY_VAR" "value"}})
```

To run a command with the full host environment, opt in explicitly with
`:inherit-env`. Only do this for trusted commands — it re-exposes every host
variable, secrets included:

```lisp
(exec/run "env" [] {:inherit-env true})  ; child inherits everything
```

### Output Limits

For both `exec/run` and `exec/pipe`, captured stdout and captured stderr are
each capped independently at 1 MiB (1,048,576 bytes). In a pipeline only the
final stage's output is captured, under the same caps.

If either stream would exceed its cap, the excess bytes are discarded, the
process runs to completion, and the call fails with a `ResourceLimitError`
instead of returning a truncated result. Output of exactly 1 MiB per stream
succeeds in full.

### Trust Model

`exec/run` and `exec/pipe` run arbitrary binaries with the interpreter's own
privileges — this is effectively remote code execution by design. Only grant the
`exec` namespace to scripts you trust. Sandboxing (user, cgroups, container,
seccomp) is the embedder's responsibility; the plugin does not confine what a
command can do once launched.

## Error Handling

Functions return errors for invalid input:
- Missing required arguments
- Wrong argument types
- Invalid command structure
- `:timeout` outside the 1–300000 ms window (rejected before process start)
- Captured stdout or stderr exceeding the 1 MiB per-stream cap (`ResourceLimitError`)

Non-zero exit codes are NOT errors - they're returned in the `:exit` field:

```lisp
(exec/run "false")  ; exits with code 1
; => {:stdout "" :stderr "" :exit 1}  ; No error, just non-zero exit
```
