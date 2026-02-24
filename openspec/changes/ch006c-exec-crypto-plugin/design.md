# Design Document: Exec/Crypto Plugin

**Change ID:** 006c-exec-crypto-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package exec

import (
    "bytes"
    "context"
    "crypto/rand"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
    defaultTimeout time.Duration
}

func New() *Plugin {
    return &Plugin{
        defaultTimeout: 30 * time.Second,
    }
}

func (p *Plugin) Name() string {
    return "exec"
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "Process execution and crypto utilities",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    // Exec functions
    env.Set("exec/run", core.GoFunc{
        Name: "exec/run",
        Fn:   p.run,
    })
    
    env.Set("exec/pipe", core.GoFunc{
        Name: "exec/pipe",
        Fn:   p.pipe,
    })
    
    env.Set("exec/which", core.GoFunc{
        Name: "exec/which",
        Fn:   p.which,
    })
    
    // Crypto functions
    env.Set("crypto/sha256", core.GoFunc{
        Name: "crypto/sha256",
        Fn:   p.sha256,
    })
    
    env.Set("crypto/uuid", core.GoFunc{
        Name: "crypto/uuid",
        Fn:   p.uuid,
    })
    
    return nil
}
```

---

## 2. Exec Functions

### exec/run

```go
func (p *Plugin) run(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 1 {
        return nil, fmt.Errorf("exec/run: requires at least 1 argument")
    }
    
    cmd, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("exec/run: first argument must be string")
    }
    
    var cmdArgs []string
    var opts *core.HashMap
    
    if len(args) >= 2 {
        if list, ok := args[1].(core.List); ok {
            for _, item := range list.Items {
                if s, ok := item.(core.String); ok {
                    cmdArgs = append(cmdArgs, s.V)
                } else {
                    return nil, fmt.Errorf("exec/run: args must be strings")
                }
            }
        } else if vec, ok := args[1].(core.Vector); ok {
            for _, item := range vec.Items {
                if s, ok := item.(core.String); ok {
                    cmdArgs = append(cmdArgs, s.V)
                } else {
                    return nil, fmt.Errorf("exec/run: args must be strings")
                }
            }
        }
    }
    
    if len(args) == 3 {
        var ok bool
        opts, ok = args[2].(*core.HashMap)
        if !ok {
            return nil, fmt.Errorf("exec/run: third argument must be map")
        }
    }
    
    // Use provided context; wrap with timeout if specified
    cmdCtx := ctx
    if opts != nil {
        if t, ok := opts.Get(core.Keyword{V: "timeout"}); ok {
            var timeoutMs int64
            switch v := t.(type) {
            case core.Int:
                timeoutMs = v.V
            case core.Float:
                timeoutMs = int64(v.V)
            }

            var cancel context.CancelFunc
            cmdCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
            defer cancel()
        }
    }

    command := exec.CommandContext(cmdCtx, cmd.V, cmdArgs...)
    
    // Set working directory
    if opts != nil {
        if d, ok := opts.Get(core.Keyword{V: "dir"}); ok {
            if ds, ok := d.(core.String); ok {
                command.Dir = ds.V
            }
        }
    }
    
    // Set environment
    if opts != nil {
        if e, ok := opts.Get(core.Keyword{V: "env"}); ok {
            if em, ok := e.(*core.HashMap); ok {
                env := os.Environ()
                for k, v := range em.M {
                    env = append(env, fmt.Sprintf("%s=%s", k.String(), v.String()))
                }
                command.Env = env
            }
        }
    }
    
    // Execute
    var stdout, stderr bytes.Buffer
    command.Stdout = &stdout
    command.Stderr = &stderr
    
    err := command.Run()
    
    // Build result
    result := core.NewHashMap()
    result.M[core.Keyword{V: "stdout"}] = core.String{V: stdout.String()}
    result.M[core.Keyword{V: "stderr"}] = core.String{V: stderr.String()}
    
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            result.M[core.Keyword{V: "exit"}] = core.Int{V: int64(exitErr.ExitCode())}
        } else {
            return nil, fmt.Errorf("exec/run: %w", err)
        }
    } else {
        result.M[core.Keyword{V: "exit"}] = core.Int{V: 0}
    }
    
    return result, nil
}
```

### exec/pipe

```go
func (p *Plugin) pipe(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 1 {
        return nil, fmt.Errorf("exec/pipe: requires at least 1 argument")
    }
    
    var cmds [][]string
    
    // Parse command list
    switch v := args[0].(type) {
    case core.List, core.Vector:
        var items []core.Value
        if l, ok := v.(core.List); ok {
            items = l.Items
        } else {
            items = v.(core.Vector).Items
        }
        
        for _, item := range items {
            switch cmdSpec := item.(type) {
            case core.List:
                var cmd []string
                for _, part := range cmdSpec.Items {
                    if s, ok := part.(core.String); ok {
                        cmd = append(cmd, s.V)
                    } else {
                        return nil, fmt.Errorf("exec/pipe: command parts must be strings")
                    }
                }
                cmds = append(cmds, cmd)
            case core.Vector:
                var cmd []string
                for _, part := range cmdSpec.Items {
                    if s, ok := part.(core.String); ok {
                        cmd = append(cmd, s.V)
                    } else {
                        return nil, fmt.Errorf("exec/pipe: command parts must be strings")
                    }
                }
                cmds = append(cmds, cmd)
            default:
                return nil, fmt.Errorf("exec/pipe: commands must be lists")
            }
        }
    default:
        return nil, fmt.Errorf("exec/pipe: first argument must be list of commands")
    }
    
    if len(cmds) == 0 {
        return nil, fmt.Errorf("exec/pipe: no commands provided")
    }
    
    // Create commands
    var commands []*exec.Cmd
    for _, cmd := range cmds {
        commands = append(commands, exec.Command(cmd[0], cmd[1:]...))
    }

    // Connect pipes between commands
    for i := 0; i < len(commands)-1; i++ {
        pipe, err := commands[i].StdoutPipe()
        if err != nil {
            return nil, fmt.Errorf("exec/pipe: create pipe: %w", err)
        }
        commands[i+1].Stdin = pipe
    }

    // Capture stdout from last command via pipe (must be set before Start)
    lastPipe, err := commands[len(commands)-1].StdoutPipe()
    if err != nil {
        return nil, fmt.Errorf("exec/pipe: create output pipe: %w", err)
    }

    // Start all commands
    for _, cmd := range commands {
        if err := cmd.Start(); err != nil {
            return nil, fmt.Errorf("exec/pipe: failed to start command: %w", err)
        }
    }

    // Read output from last command's pipe
    outBytes, readErr := io.ReadAll(lastPipe)

    // Wait for all commands
    var lastErr error
    for _, cmd := range commands {
        if err := cmd.Wait(); err != nil {
            lastErr = err
        }
    }

    if readErr != nil {
        return nil, fmt.Errorf("exec/pipe: read output: %w", readErr)
    }

    var stdout bytes.Buffer
    stdout.Write(outBytes)
    
    // Build result
    result := core.NewHashMap()
    result.M[core.Keyword{V: "stdout"}] = core.String{V: stdout.String()}
    result.M[core.Keyword{V: "stderr"}] = core.String{V: ""}
    
    if lastErr != nil {
        if exitErr, ok := lastErr.(*exec.ExitError); ok {
            result.M[core.Keyword{V: "exit"}] = core.Int{V: int64(exitErr.ExitCode())}
        } else {
            return nil, fmt.Errorf("exec/pipe: %w", lastErr)
        }
    } else {
        result.M[core.Keyword{V: "exit"}] = core.Int{V: 0}
    }
    
    return result, nil
}
```

### exec/which

```go
func (p *Plugin) which(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("exec/which: requires 1 argument")
    }
    
    name, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("exec/which: argument must be string")
    }
    
    path, err := exec.LookPath(name.V)
    if err != nil {
        return core.Nil{}, nil
    }
    
    return core.String{V: path}, nil
}
```

---

## 3. Crypto Functions

### crypto/sha256

```go
func (p *Plugin) sha256(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("crypto/sha256: requires 1 argument")
    }
    
    data, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("crypto/sha256: argument must be string")
    }
    
    h := sha256.New()
    h.Write([]byte(data.V))
    hash := hex.EncodeToString(h.Sum(nil))
    
    return core.String{V: hash}, nil
}
```

### crypto/uuid

```go
func (p *Plugin) uuid(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("crypto/uuid: takes no arguments")
	}

	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return nil, fmt.Errorf("crypto/uuid: %w", err)
	}
	// Set version 4 and variant bits (RFC 4122)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return core.String{V: id}, nil
}
```

---

## 4. File Organization

```
plugins/exec/
├── plugin.go         # Main plugin
├── exec.go           # Process execution
├── crypto.go         # Crypto functions
└── exec_test.go      # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).
