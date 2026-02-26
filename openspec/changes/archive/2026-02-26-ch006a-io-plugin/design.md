# Design Document: IO Plugin

**Change ID:** 006a-io-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package lio

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
    "regexp"
    "strings"

    "github.com/victorzhuk/go-lispico/core"
)

type SandboxMode int

const (
    ModeStrict SandboxMode = iota
    ModeRelaxed
    ModeNone
)

type Config struct {
    Mode        SandboxMode
    RootDir     string         // For strict mode
    AllowRead   []string       // For relaxed mode
    AllowWrite  []string       // For relaxed mode
    DenyPattern []*regexp.Regexp
}

type Plugin struct {
    config Config
}

type Sandbox struct {
    config Config
}

func New(config Config) *Plugin {
    return &Plugin{config: config}
}

func (p *Plugin) Name() string {
    return "io"
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "File I/O and environment plugin",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    // Filesystem functions
    env.Set("fs/read-file", core.GoFunc{
        Name: "fs/read-file",
        Fn:   p.readFile,
    })
    
    env.Set("fs/write-file", core.GoFunc{
        Name: "fs/write-file",
        Fn:   p.writeFile,
    })
    
    env.Set("fs/exists?", core.GoFunc{
        Name: "fs/exists?",
        Fn:   p.exists,
    })
    
    env.Set("fs/ls", core.GoFunc{
        Name: "fs/ls",
        Fn:   p.ls,
    })
    
    env.Set("fs/mkdir", core.GoFunc{
        Name: "fs/mkdir",
        Fn:   p.mkdir,
    })
    
    env.Set("fs/stat", core.GoFunc{
        Name: "fs/stat",
        Fn:   p.stat,
    })
    
    // Environment functions
    env.Set("env/get", core.GoFunc{
        Name: "env/get",
        Fn:   p.envGet,
    })
    
    env.Set("env/set", core.GoFunc{
        Name: "env/set",
        Fn:   p.envSet,
    })
    
    return nil
}
```

---

## 2. Sandbox Implementation

```go
func NewSandbox(config Config) *Sandbox {
    return &Sandbox{config: config}
}

func (s *Sandbox) Validate(path string, forWrite bool) (string, error) {
    // Clean path
    cleaned := filepath.Clean(path)
    
    // Check deny patterns first
    for _, pattern := range s.config.DenyPattern {
        if pattern.MatchString(cleaned) {
            return "", fmt.Errorf("path matches deny pattern: %s", cleaned)
        }
    }
    
    switch s.config.Mode {
    case ModeStrict:
        return s.validateStrict(cleaned, forWrite)
    case ModeRelaxed:
        return s.validateRelaxed(cleaned, forWrite)
    case ModeNone:
        return cleaned, nil
    default:
        return "", fmt.Errorf("unknown sandbox mode")
    }
}

func (s *Sandbox) validateStrict(path string, forWrite bool) (string, error) {
    // Join with root
    full := filepath.Join(s.config.RootDir, path)
    
    // Resolve any symlinks
    resolved, err := filepath.EvalSymlinks(full)
    if err != nil {
        // Path might not exist yet (for writes)
        // Check parent
        parent := filepath.Dir(full)
        resolvedParent, err := filepath.EvalSymlinks(parent)
        if err != nil {
            return "", fmt.Errorf("invalid path: %w", err)
        }
        resolved = filepath.Join(resolvedParent, filepath.Base(full))
    }
    
    // Verify it's under root
    if !strings.HasPrefix(resolved, s.config.RootDir) {
        return "", fmt.Errorf("path escapes sandbox: %s", path)
    }
    
    return resolved, nil
}

func (s *Sandbox) validateRelaxed(path string, forWrite bool) (string, error) {
    // Resolve path
    resolved, err := filepath.Abs(path)
    if err != nil {
        return "", err
    }
    
    // Check allow lists
    var allowList []string
    if forWrite {
        allowList = s.config.AllowWrite
    } else {
        allowList = s.config.AllowRead
    }
    
    allowed := false
    for _, prefix := range allowList {
        if strings.HasPrefix(resolved, prefix) {
            allowed = true
            break
        }
    }
    
    if !allowed {
        return "", fmt.Errorf("path not in allow list: %s", path)
    }
    
    return resolved, nil
}
```

---

## 3. Function Implementations

### fs/read-file

```go
func (p *Plugin) readFile(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fs/read-file: requires 1 argument")
    }
    
    path, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("fs/read-file: argument must be string")
    }
    
    // Validate path
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, false)
    if err != nil {
        return nil, fmt.Errorf("fs/read-file: %w", err)
    }
    
    // Read file
    data, err := os.ReadFile(resolved)
    if err != nil {
        return nil, fmt.Errorf("fs/read-file: %w", err)
    }
    
    return core.String{V: string(data)}, nil
}
```

### fs/write-file

```go
func (p *Plugin) writeFile(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("fs/write-file: requires 2 arguments")
    }
    
    path, ok1 := args[0].(core.String)
    content, ok2 := args[1].(core.String)
    
    if !ok1 || !ok2 {
        return nil, fmt.Errorf("fs/write-file: arguments must be strings")
    }
    
    // Validate path
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, true)
    if err != nil {
        return nil, fmt.Errorf("fs/write-file: %w", err)
    }
    
    // Create parent directories
    dir := filepath.Dir(resolved)
    if err := os.MkdirAll(dir, 0755); err != nil {
        return nil, fmt.Errorf("fs/write-file: %w", err)
    }
    
    // Write file
    if err := os.WriteFile(resolved, []byte(content.V), 0644); err != nil {
        return nil, fmt.Errorf("fs/write-file: %w", err)
    }
    
    return core.Nil{}, nil
}
```

### fs/exists?

```go
func (p *Plugin) exists(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fs/exists?: requires 1 argument")
    }
    
    path, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("fs/exists?: argument must be string")
    }
    
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, false)
    if err != nil {
        // If validation fails, file doesn't exist for our purposes
        return core.Bool{V: false}, nil
    }
    
    _, err = os.Stat(resolved)
    return core.Bool{V: err == nil}, nil
}
```

### fs/ls

```go
func (p *Plugin) ls(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fs/ls: requires 1 argument")
    }
    
    path, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("fs/ls: argument must be string")
    }
    
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, false)
    if err != nil {
        return nil, fmt.Errorf("fs/ls: %w", err)
    }
    
    entries, err := os.ReadDir(resolved)
    if err != nil {
        return nil, fmt.Errorf("fs/ls: %w", err)
    }
    
    items := make([]core.Value, len(entries))
    for i, entry := range entries {
        items[i] = core.String{V: entry.Name()}
    }
    
    return core.List{Items: items}, nil
}
```

### fs/mkdir

```go
func (p *Plugin) mkdir(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fs/mkdir: requires 1 argument")
    }
    
    path, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("fs/mkdir: argument must be string")
    }
    
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, true)
    if err != nil {
        return nil, fmt.Errorf("fs/mkdir: %w", err)
    }
    
    if err := os.MkdirAll(resolved, 0755); err != nil {
        return nil, fmt.Errorf("fs/mkdir: %w", err)
    }
    
    return core.Nil{}, nil
}
```

### fs/stat

```go
func (p *Plugin) stat(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fs/stat: requires 1 argument")
    }
    
    path, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("fs/stat: argument must be string")
    }
    
    sandbox := NewSandbox(p.config)
    resolved, err := sandbox.Validate(path.V, false)
    if err != nil {
        return nil, fmt.Errorf("fs/stat: %w", err)
    }
    
    info, err := os.Stat(resolved)
    if err != nil {
        return nil, fmt.Errorf("fs/stat: %w", err)
    }
    
    m := core.NewHashMap()
    m.M[core.Keyword{V: "size"}] = core.Int{V: info.Size()}
    m.M[core.Keyword{V: "mtime"}] = core.Int{V: info.ModTime().Unix()}
    m.M[core.Keyword{V: "isdir"}] = core.Bool{V: info.IsDir()}
    m.M[core.Keyword{V: "mode"}] = core.String{V: info.Mode().String()}
    
    return m, nil
}
```

### env/get

```go
func (p *Plugin) envGet(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("env/get: requires 1 argument")
	}

	name, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("env/get: argument must be string")
	}

	// Use LookupEnv to distinguish unset from empty string
	val, found := os.LookupEnv(name.V)
	if !found {
		return core.Nil{}, nil
	}
	return core.String{V: val}, nil
}
```

### env/set

```go
func (p *Plugin) envSet(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("env/set: requires 2 arguments")
    }
    
    name, ok1 := args[0].(core.String)
    value, ok2 := args[1].(core.String)
    
    if !ok1 || !ok2 {
        return nil, fmt.Errorf("env/set: arguments must be strings")
    }
    
    if err := os.Setenv(name.V, value.V); err != nil {
        return nil, fmt.Errorf("env/set: %w", err)
    }
    
    return core.Nil{}, nil
}
```

---

## 4. File Organization

```
plugins/lio/
├── plugin.go         # Main plugin
├── sandbox.go        # Sandbox validation
├── filesystem.go     # File operations
├── environment.go    # Environment operations
└── lio_test.go       # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).
