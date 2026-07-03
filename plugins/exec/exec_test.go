package exec

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestPlugin_Name(t *testing.T) {
	p := New()
	assert.Equal(t, "exec", p.Name())
}

func TestPlugin_Metadata(t *testing.T) {
	p := New()
	meta := p.Metadata()
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Contains(t, meta.Description, "Process execution")
}

func TestPlugin_Init(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	err := p.Init(env)
	require.NoError(t, err)

	tests := []string{
		"exec/run",
		"exec/pipe",
		"exec/which",
		"crypto/sha256",
		"crypto/uuid",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			v, ok := env.Get(name)
			assert.True(t, ok, "function %s should be registered", name)
			_, ok = v.(core.GoFunc)
			assert.True(t, ok, "function %s should be GoFunc", name)
		})
	}
}

func TestExec_Run(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	t.Run("simple command", func(t *testing.T) {
		args := []core.Value{
			core.String{V: "echo"},
			core.Vector{Items: []core.Value{core.String{V: "hello"}}},
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm, ok := result.(*core.HashMap)
		require.True(t, ok)

		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "hello\n", stdout.(core.String).V)

		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(0), exit.(core.Int).V)
	})

	t.Run("command with options", func(t *testing.T) {
		args := []core.Value{
			core.String{V: "echo"},
			core.Vector{Items: []core.Value{core.String{V: "test"}}},
			core.NewHashMap(),
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "test\n", stdout.(core.String).V)
	})

	t.Run("non-zero exit code", func(t *testing.T) {
		args := []core.Value{
			core.String{V: "sh"},
			core.Vector{Items: []core.Value{
				core.String{V: "-c"},
				core.String{V: "exit 42"},
			}},
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(42), exit.(core.Int).V)
	})

	t.Run("timeout kills process", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "timeout"}, core.Int{V: 100})

		args := []core.Value{
			core.String{V: "sleep"},
			core.Vector{Items: []core.Value{core.String{V: "10"}}},
			opts,
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(-1), exit.(core.Int).V)
	})

	t.Run("context cancellation kills process", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		args := []core.Value{
			core.String{V: "sleep"},
			core.Vector{Items: []core.Value{core.String{V: "10"}}},
		}
		result, err := p.run(ctx, nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(-1), exit.(core.Int).V)
	})

	t.Run("working directory option", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "dir"}, core.String{V: "/tmp"})

		args := []core.Value{
			core.String{V: "pwd"},
			core.Vector{Items: []core.Value{}},
			opts,
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "/tmp\n", stdout.(core.String).V)
	})

	t.Run("environment variables option", func(t *testing.T) {
		envMap := core.NewHashMap()
		envMap, _ = envMap.Assoc(core.String{V: "MY_TEST_VAR"}, core.String{V: "test_value_123"})
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "env"}, envMap)

		args := []core.Value{
			core.String{V: "sh"},
			core.Vector{Items: []core.Value{core.String{V: "-c"}, core.String{V: "echo $MY_TEST_VAR"}}},
			opts,
		}
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "test_value_123\n", stdout.(core.String).V)
	})

	t.Run("missing command", func(t *testing.T) {
		args := []core.Value{}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid command type", func(t *testing.T) {
		args := []core.Value{core.Int{V: 123}}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid args type", func(t *testing.T) {
		args := []core.Value{core.String{V: "echo"}, core.Int{V: 123}}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid options type", func(t *testing.T) {
		args := []core.Value{core.String{V: "echo"}, core.Vector{}, core.Int{V: 123}}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid timeout type", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "timeout"}, core.String{V: "not-an-int"})
		args := []core.Value{core.String{V: "echo"}, core.Vector{}, opts}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid dir type", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "dir"}, core.Int{V: 123})
		args := []core.Value{core.String{V: "echo"}, core.Vector{}, opts}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("invalid env type", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "env"}, core.String{V: "not-a-map"})
		args := []core.Value{core.String{V: "echo"}, core.Vector{}, opts}
		_, err := p.run(context.Background(), nil, args, env)
		assert.Error(t, err)
	})
}

func TestExec_Pipe(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	t.Run("pipe two commands", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "hello world"}}},
			core.Vector{Items: []core.Value{core.String{V: "tr"}, core.String{V: "a-z"}, core.String{V: "A-Z"}}},
		}}
		args := []core.Value{commands}

		result, err := p.pipe(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "HELLO WORLD\n", stdout.(core.String).V)
	})

	t.Run("pipe with list", func(t *testing.T) {
		commands := core.List{Items: []core.Value{
			core.List{Items: []core.Value{core.String{V: "echo"}, core.String{V: "test"}}},
		}}
		args := []core.Value{commands}

		result, err := p.pipe(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(0), exit.(core.Int).V)
	})

	t.Run("single command", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "single"}}},
		}}
		args := []core.Value{commands}

		result, err := p.pipe(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		stdout, _ := hm.Get(core.Keyword{V: "stdout"})
		assert.Equal(t, "single\n", stdout.(core.String).V)
	})

	t.Run("pipe with timeout option", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "test"}}},
		}}
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "timeout"}, core.Int{V: 5000})
		args := []core.Value{commands, opts}

		result, err := p.pipe(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(0), exit.(core.Int).V)
	})

	t.Run("pipe with invalid timeout type", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "test"}}},
		}}
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "timeout"}, core.String{V: "not-an-int"})
		args := []core.Value{commands, opts}

		_, err := p.pipe(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("pipe with invalid options type", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "test"}}},
		}}
		args := []core.Value{commands, core.Int{V: 123}}

		_, err := p.pipe(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("pipe with command failure", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "sh"}, core.String{V: "-c"}, core.String{V: "exit 5"}}},
		}}
		args := []core.Value{commands}

		result, err := p.pipe(context.Background(), nil, args, env)
		require.NoError(t, err)

		hm := result.(*core.HashMap)
		exit, _ := hm.Get(core.Keyword{V: "exit"})
		assert.Equal(t, int64(5), exit.(core.Int).V)
	})

	t.Run("empty commands", func(t *testing.T) {
		commands := core.Vector{Items: []core.Value{}}
		args := []core.Value{commands}
		_, err := p.pipe(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("missing commands argument", func(t *testing.T) {
		args := []core.Value{}
		_, err := p.pipe(context.Background(), nil, args, env)
		assert.Error(t, err)
	})
}

func TestExec_Which(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	t.Run("find common executable", func(t *testing.T) {
		args := []core.Value{core.String{V: "ls"}}
		result, err := p.which(context.Background(), nil, args, env)
		require.NoError(t, err)

		path, ok := result.(core.String)
		assert.True(t, ok)
		assert.NotEmpty(t, path.V)
		assert.Contains(t, path.V, "ls")
	})

	t.Run("non-existent command returns nil", func(t *testing.T) {
		args := []core.Value{core.String{V: "nonexistent-command-xyz-123"}}
		result, err := p.which(context.Background(), nil, args, env)
		require.NoError(t, err)

		_, ok := result.(core.Nil)
		assert.True(t, ok)
	})

	t.Run("missing argument", func(t *testing.T) {
		args := []core.Value{}
		_, err := p.which(context.Background(), nil, args, env)
		assert.Error(t, err)
	})
}

func TestCrypto_Sha256(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	t.Run("hash string", func(t *testing.T) {
		args := []core.Value{core.String{V: "hello"}}
		result, err := p.sha256(context.Background(), nil, args, env)
		require.NoError(t, err)

		hash, ok := result.(core.String)
		require.True(t, ok)
		assert.Len(t, hash.V, 64)
		assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", hash.V)
	})

	t.Run("empty string", func(t *testing.T) {
		args := []core.Value{core.String{V: ""}}
		result, err := p.sha256(context.Background(), nil, args, env)
		require.NoError(t, err)

		hash, ok := result.(core.String)
		require.True(t, ok)
		assert.Len(t, hash.V, 64)
	})

	t.Run("missing argument", func(t *testing.T) {
		args := []core.Value{}
		_, err := p.sha256(context.Background(), nil, args, env)
		assert.Error(t, err)
	})

	t.Run("wrong type", func(t *testing.T) {
		args := []core.Value{core.Int{V: 123}}
		_, err := p.sha256(context.Background(), nil, args, env)
		assert.Error(t, err)
	})
}

func TestCrypto_Uuid(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	t.Run("generate valid uuid", func(t *testing.T) {
		args := []core.Value{}
		result, err := p.uuid(context.Background(), nil, args, env)
		require.NoError(t, err)

		uuid, ok := result.(core.String)
		require.True(t, ok)
		assert.Regexp(t, uuidPattern, uuid.V)
	})

	t.Run("generate unique uuids", func(t *testing.T) {
		args := []core.Value{}
		uuids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			result, err := p.uuid(context.Background(), nil, args, env)
			require.NoError(t, err)
			uuid := result.(core.String).V
			assert.False(t, uuids[uuid], "generated duplicate uuid")
			uuids[uuid] = true
		}
	})
}

func TestToStringSlice(t *testing.T) {
	t.Run("from vector", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.String{V: "a"},
			core.String{V: "b"},
			core.String{V: "c"},
		}}
		result, err := toStringSlice(v)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("from list", func(t *testing.T) {
		v := core.List{Items: []core.Value{
			core.String{V: "x"},
			core.String{V: "y"},
		}}
		result, err := toStringSlice(v)
		require.NoError(t, err)
		assert.Equal(t, []string{"x", "y"}, result)
	})

	t.Run("invalid type", func(t *testing.T) {
		v := core.String{V: "not a list"}
		_, err := toStringSlice(v)
		assert.Error(t, err)
	})

	t.Run("non-string element", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{core.Int{V: 123}}}
		_, err := toStringSlice(v)
		assert.Error(t, err)
	})
}

func TestToCommandList(t *testing.T) {
	t.Run("valid commands", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.String{V: "hello"}}},
			core.List{Items: []core.Value{core.String{V: "grep"}, core.String{V: "h"}}},
		}}
		cmds, err := toCommandList(v)
		require.NoError(t, err)
		require.Len(t, cmds, 2)
		assert.Equal(t, "echo", cmds[0].name)
		assert.Equal(t, []string{"hello"}, cmds[0].args)
		assert.Equal(t, "grep", cmds[1].name)
		assert.Equal(t, []string{"h"}, cmds[1].args)
	})

	t.Run("command without args", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "ls"}}},
		}}
		cmds, err := toCommandList(v)
		require.NoError(t, err)
		require.Len(t, cmds, 1)
		assert.Equal(t, "ls", cmds[0].name)
		assert.Empty(t, cmds[0].args)
	})

	t.Run("invalid container type", func(t *testing.T) {
		v := core.String{V: "not a list"}
		_, err := toCommandList(v)
		assert.Error(t, err)
	})

	t.Run("invalid command type", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.String{V: "not a command"},
		}}
		_, err := toCommandList(v)
		assert.Error(t, err)
	})

	t.Run("empty command vector", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{}},
		}}
		_, err := toCommandList(v)
		assert.Error(t, err)
	})

	t.Run("non-string command name", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.Int{V: 123}}},
		}}
		_, err := toCommandList(v)
		assert.Error(t, err)
	})

	t.Run("non-string arg in command", func(t *testing.T) {
		v := core.Vector{Items: []core.Value{
			core.Vector{Items: []core.Value{core.String{V: "echo"}, core.Int{V: 123}}},
		}}
		_, err := toCommandList(v)
		assert.Error(t, err)
	})
}

func TestExec_EnvIsolation(t *testing.T) {
	t.Setenv("LISPICO_FAKE_SECRET", "leaked")

	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	stdoutOf := func(opts ...core.Value) string {
		args := append([]core.Value{
			core.String{V: "sh"},
			core.Vector{Items: []core.Value{core.String{V: "-c"}, core.String{V: "echo $LISPICO_FAKE_SECRET"}}},
		}, opts...)
		result, err := p.run(context.Background(), nil, args, env)
		require.NoError(t, err)
		stdout, _ := result.(*core.HashMap).Get(core.Keyword{V: "stdout"})
		return stdout.(core.String).V
	}

	t.Run("host secret not leaked by default", func(t *testing.T) {
		assert.Equal(t, "\n", stdoutOf())
	})

	t.Run("inherit-env opts into the host env", func(t *testing.T) {
		opts := core.NewHashMap()
		opts, _ = opts.Assoc(core.Keyword{V: "inherit-env"}, core.Bool{V: true})
		assert.Equal(t, "leaked\n", stdoutOf(opts))
	})
}
