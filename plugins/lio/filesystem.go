package lio

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) readFile(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/read-file: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/read-file: requires 1 argument (path)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/read-file: path must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, false)
	if err != nil {
		return nil, fmt.Errorf("io/read-file: %w", err)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("io/read-file: open %s: %w", pathArg.V, err)
	}
	defer func() { _ = file.Close() }()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("io/read-file: read %s: %w", pathArg.V, err)
	}

	return core.String{V: string(content)}, nil
}

func (p *Plugin) writeFile(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/write-file: %w", ctx.Err())
	}
	if len(args) != 2 {
		return nil, fmt.Errorf("io/write-file: requires 2 arguments (path, content)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/write-file: path must be string")
	}

	contentArg, ok := args[1].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/write-file: content must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, true)
	if err != nil {
		return nil, fmt.Errorf("io/write-file: %w", err)
	}

	dir := dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("io/write-file: create dir %s: %w", dir, err)
	}

	err = os.WriteFile(path, []byte(contentArg.V), 0644)
	if err != nil {
		return nil, fmt.Errorf("io/write-file: write %s: %w", pathArg.V, err)
	}

	return core.Nil{}, nil
}

func (p *Plugin) exists(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/exists?: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/exists?: requires 1 argument (path)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/exists?: path must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, false)
	if err != nil {
		return nil, fmt.Errorf("io/exists?: %w", err)
	}

	_, err = os.Stat(path)
	return core.Bool{V: err == nil}, nil
}

func (p *Plugin) ls(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/ls: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/ls: requires 1 argument (path)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/ls: path must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, false)
	if err != nil {
		return nil, fmt.Errorf("io/ls: %w", err)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("io/ls: read dir %s: %w", pathArg.V, err)
	}

	items := make([]core.Value, len(entries))
	for i, entry := range entries {
		items[i] = core.String{V: entry.Name()}
	}

	return core.List{Items: items}, nil
}

func (p *Plugin) mkdir(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/mkdir: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/mkdir: requires 1 argument (path)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/mkdir: path must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, true)
	if err != nil {
		return nil, fmt.Errorf("io/mkdir: %w", err)
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, fmt.Errorf("io/mkdir: create %s: %w", pathArg.V, err)
	}

	return core.Nil{}, nil
}

func (p *Plugin) stat(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("io/stat: %w", ctx.Err())
	}
	if len(args) != 1 {
		return nil, fmt.Errorf("io/stat: requires 1 argument (path)")
	}

	pathArg, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("io/stat: path must be string")
	}

	path, err := p.sandbox.Validate(pathArg.V, false)
	if err != nil {
		return nil, fmt.Errorf("io/stat: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("io/stat: stat %s: %w", pathArg.V, err)
	}

	m := core.NewHashMap()
	m, _ = m.Assoc(core.Keyword{V: "size"}, core.Int{V: info.Size()})
	m, _ = m.Assoc(core.Keyword{V: "mtime"}, core.Int{V: info.ModTime().Unix()})
	m, _ = m.Assoc(core.Keyword{V: "isdir"}, core.Bool{V: info.IsDir()})
	m, _ = m.Assoc(core.Keyword{V: "mode"}, core.String{V: info.Mode().String()})

	return m, nil
}

func dir(path string) string {
	return path[:len(path)-len(filepath.Base(path))]
}
