package exec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) run(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("exec/run: requires at least 1 argument")
	}

	cmdName, err := expectString(args[0], "command")
	if err != nil {
		return nil, err
	}

	var cmdArgs []string
	if len(args) >= 2 {
		cmdArgs, err = toStringSlice(args[1])
		if err != nil {
			return nil, fmt.Errorf("args must be list/vector of strings: %w", err)
		}
	}

	timeout := p.defaultTimeout
	dir := ""
	extraEnv := make(map[string]string)
	inheritEnv := false

	if len(args) >= 3 {
		opts, ok := args[2].(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("options must be a map")
		}

		if v, ok := opts.Get(core.Keyword{V: "inherit-env"}); ok {
			b, ok := v.(core.Bool)
			if !ok {
				return nil, fmt.Errorf("exec/run: inherit-env must be a boolean")
			}
			inheritEnv = b.V
		}

		if v, ok := opts.Get(core.Keyword{V: "timeout"}); ok {
			t, err := expectInt(v, "timeout")
			if err != nil {
				return nil, err
			}
			timeout = t
		}

		if v, ok := opts.Get(core.Keyword{V: "dir"}); ok {
			dir, err = expectString(v, "dir")
			if err != nil {
				return nil, err
			}
		}

		if v, ok := opts.Get(core.Keyword{V: "env"}); ok {
			envMap, ok := v.(*core.HashMap)
			if !ok {
				return nil, fmt.Errorf("env option must be a map")
			}
			envMap.Each(func(k, v core.Value) {
				if key, ok := k.(core.String); ok {
					if val, ok := v.(core.String); ok {
						extraEnv[key.V] = val.V
					}
				}
			})
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, cmdName, cmdArgs...)

	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Env = buildEnv(inheritEnv, extraEnv)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	result := core.NewHashMap()
	result, _ = result.Assoc(core.Keyword{V: "stdout"}, core.String{V: stdout.String()})
	result, _ = result.Assoc(core.Keyword{V: "stderr"}, core.String{V: stderr.String()})

	exitCode := int64(0)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = int64(exitErr.ExitCode())
		} else if cmdCtx.Err() == context.DeadlineExceeded {
			exitCode = -1
		} else {
			return nil, fmt.Errorf("run command %q: %w", cmdName, err)
		}
	}

	result, _ = result.Assoc(core.Keyword{V: "exit"}, core.Int{V: exitCode})

	return result, nil
}

func (p *Plugin) pipe(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("exec/pipe: requires at least 1 argument")
	}

	commands, err := toCommandList(args[0])
	if err != nil {
		return nil, err
	}

	if len(commands) == 0 {
		return nil, fmt.Errorf("exec/pipe: requires at least 1 command")
	}

	timeout := p.defaultTimeout
	if len(args) >= 2 {
		opts, ok := args[1].(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("options must be a map")
		}
		if v, ok := opts.Get(core.Keyword{V: "timeout"}); ok {
			t, err := expectInt(v, "timeout")
			if err != nil {
				return nil, err
			}
			timeout = t
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	cmds := make([]*exec.Cmd, len(commands))
	for i, c := range commands {
		cmds[i] = exec.CommandContext(cmdCtx, c.name, c.args...)
		cmds[i].Env = buildEnv(false, nil)
	}

	for i := 0; i < len(cmds)-1; i++ {
		stdout, err := cmds[i].StdoutPipe()
		if err != nil {
			return nil, fmt.Errorf("create stdout pipe: %w", err)
		}
		cmds[i+1].Stdin = stdout
	}

	var finalStdout, finalStderr bytes.Buffer
	cmds[len(cmds)-1].Stdout = &finalStdout
	cmds[len(cmds)-1].Stderr = &finalStderr

	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start command: %w", err)
		}
	}

	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result := core.NewHashMap()
				result, _ = result.Assoc(core.Keyword{V: "stdout"}, core.String{V: finalStdout.String()})
				result, _ = result.Assoc(core.Keyword{V: "stderr"}, core.String{V: finalStderr.String()})
				result, _ = result.Assoc(core.Keyword{V: "exit"}, core.Int{V: int64(exitErr.ExitCode())})
				return result, nil
			}
			return nil, fmt.Errorf("wait for command: %w", err)
		}
	}

	result := core.NewHashMap()
	result, _ = result.Assoc(core.Keyword{V: "stdout"}, core.String{V: finalStdout.String()})
	result, _ = result.Assoc(core.Keyword{V: "stderr"}, core.String{V: finalStderr.String()})
	result, _ = result.Assoc(core.Keyword{V: "exit"}, core.Int{V: 0})

	return result, nil
}

func (p *Plugin) which(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("exec/which: requires 1 argument")
	}

	cmdName, err := expectString(args[0], "command")
	if err != nil {
		return nil, err
	}

	path, err := exec.LookPath(cmdName)
	if err != nil {
		return core.Nil{}, nil
	}

	return core.String{V: path}, nil
}

// buildEnv returns the child process environment. By default the child sees
// only PATH and HOME, so host secrets in the embedder's environment are not
// leaked to executed commands; inherit-env opts back into the full host env.
func buildEnv(inherit bool, extra map[string]string) []string {
	var env []string
	if inherit {
		env = os.Environ()
	} else {
		for _, key := range []string{"PATH", "HOME"} {
			if v, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+v)
			}
		}
	}
	for k, v := range extra {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

type command struct {
	name string
	args []string
}

func toCommandList(v core.Value) ([]command, error) {
	var items []core.Value
	switch val := v.(type) {
	case core.List:
		items = val.Items
	case core.Vector:
		items = val.Items
	default:
		return nil, fmt.Errorf("expected list or vector, got %T", v)
	}

	commands := make([]command, len(items))
	for i, item := range items {
		var pairItems []core.Value
		switch p := item.(type) {
		case core.Vector:
			pairItems = p.Items
		case core.List:
			pairItems = p.Items
		default:
			return nil, fmt.Errorf("command %d must be a vector or list", i)
		}

		if len(pairItems) < 1 {
			return nil, fmt.Errorf("command %d must have at least 1 element", i)
		}

		name, err := expectString(pairItems[0], "command name")
		if err != nil {
			return nil, fmt.Errorf("command %d: %w", i, err)
		}

		var args []string
		if len(pairItems) > 1 {
			rest := core.List{Items: pairItems[1:]}
			args, err = toStringSlice(rest)
			if err != nil {
				return nil, fmt.Errorf("command %d args: %w", i, err)
			}
		}

		commands[i] = command{name: name, args: args}
	}

	return commands, nil
}

func toStringSlice(v core.Value) ([]string, error) {
	var items []core.Value
	switch val := v.(type) {
	case core.List:
		items = val.Items
	case core.Vector:
		items = val.Items
	default:
		return nil, fmt.Errorf("expected list or vector, got %T", v)
	}

	result := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(core.String)
		if !ok {
			return nil, fmt.Errorf("element %d is not a string", i)
		}
		result[i] = s.V
	}

	return result, nil
}

func expectString(v core.Value, name string) (string, error) {
	s, ok := v.(core.String)
	if !ok {
		return "", fmt.Errorf("%s must be a string, got %T", name, v)
	}
	return s.V, nil
}

func expectInt(v core.Value, name string) (int64, error) {
	i, ok := v.(core.Int)
	if !ok {
		return 0, fmt.Errorf("%s must be an integer, got %T", name, v)
	}
	return i.V, nil
}
