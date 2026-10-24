package lio

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/victorzhuk/go-lispico/core"
)

// maxReadFileBytes bounds a single io/read-file result. The plugin config
// has no size limit knob, so this is a package constant: without it a
// script forces unbounded host reads outside the allocation meter.
const maxReadFileBytes int64 = 1 << 20

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

	file, err := p.sandbox.openFileWithin(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("io/read-file: open %s: %w", pathArg.V, err)
	}
	defer func() { _ = file.Close() }()

	body, err := io.ReadAll(io.LimitReader(file, maxReadFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("io/read-file: read %s: %w", pathArg.V, err)
	}
	if int64(len(body)) > maxReadFileBytes {
		return nil, core.NewResourceLimitError(fmt.Sprintf("io/read-file: %s exceeds max read size of %d bytes", pathArg.V, maxReadFileBytes))
	}
	if err := chargeReadBytes(ctx, int64(len(body))); err != nil {
		return nil, err
	}

	return core.String{V: string(body)}, nil
}

// chargeReadBytes is the sole permitted call shape for the result-size
// charge: the ChargeGoFuncResultBytes call itself must sit in return
// position, so it lives in this wrapper rather than at the read site.
func chargeReadBytes(ctx context.Context, n int64) error {
	if ctx == nil {
		return nil
	}
	return core.ChargeGoFuncResultBytes(ctx, n)
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

	if err := p.sandbox.mkdirAllWithin(path, 0755); err != nil {
		return nil, fmt.Errorf("io/write-file: create dir %s: %w", dir(path), err)
	}

	file, err := p.sandbox.openFileWithin(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("io/write-file: write %s: %w", pathArg.V, err)
	}
	if _, err := file.Write([]byte(contentArg.V)); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("io/write-file: write %s: %w", pathArg.V, err)
	}
	if err := file.Close(); err != nil {
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

	_, err = p.sandbox.statWithin(path)
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

	limit := collectionLimit(eval)
	bound := -1
	if limit > 0 {
		bound = math.MaxInt
		if limit < math.MaxInt {
			bound = limit + 1
		}
	}
	entries, err := p.sandbox.readDirWithin(path, bound)
	if err != nil {
		return nil, fmt.Errorf("io/ls: read dir %s: %w", pathArg.V, err)
	}

	if limit > 0 && len(entries) > limit {
		return nil, core.NewResourceLimitError(fmt.Sprintf("io/ls: %s has more than %d entries, exceeds collection limit %d", pathArg.V, limit, limit))
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	items := make([]core.Value, len(entries))
	var resultBytes int64
	for i, entry := range entries {
		items[i] = core.String{V: entry.Name()}
		resultBytes += int64(len(entry.Name()))
	}
	if err := chargeReadBytes(ctx, resultBytes); err != nil {
		return nil, err
	}

	return core.NewList(items), nil
}

// collectionLimit resolves the active collection limit from the evaluator
// using the stdlib convention; an evaluator without a limiter is unlimited.
func collectionLimit(eval core.Evaluator) int {
	if cl, ok := eval.(core.CollectionLimiter); ok {
		return cl.CollectionLimit()
	}
	return 0
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

	if err := p.sandbox.mkdirAllRooted(path, 0755); err != nil {
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

	info, err := p.sandbox.statWithin(path)
	if err != nil {
		return nil, fmt.Errorf("io/stat: stat %s: %w", pathArg.V, err)
	}

	m := core.NewHashMap()
	m, _, _ = m.Assoc(core.Keyword{V: "size"}, core.Int{V: info.Size()})
	m, _, _ = m.Assoc(core.Keyword{V: "mtime"}, core.Int{V: info.ModTime().Unix()})
	m, _, _ = m.Assoc(core.Keyword{V: "isdir"}, core.Bool{V: info.IsDir()})
	m, _, _ = m.Assoc(core.Keyword{V: "mode"}, core.String{V: info.Mode().String()})

	return m, nil
}

func dir(path string) string {
	return path[:len(path)-len(filepath.Base(path))]
}
