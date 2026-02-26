package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(ctx context.Context, model, system, prompt string) (string, error) {
	return m.response, m.err
}

func setupTest(t *testing.T, mock *mockLLM) (*Plugin, *core.Env) {
	p := New(mock)
	env := core.NewEnv(nil)
	env.SetEvaluator(core.NewEvaluator())

	require.NoError(t, p.Init(env))
	return p, env
}

func eval(env *core.Env, code string) (core.Value, error) {
	forms, err := core.Read(code)
	if err != nil {
		return nil, err
	}
	return env.Evaluator().Eval(context.Background(), forms[0], env)
}

func TestRegistry(t *testing.T) {
	t.Run("Register and Get", func(t *testing.T) {
		r := newRegistry()

		a := &Agent{ID: "test", Model: "gpt-4"}
		r.Register(a)

		got, ok := r.Get("test")
		assert.True(t, ok)
		assert.Equal(t, "gpt-4", got.Model)
	})

	t.Run("Get not found", func(t *testing.T) {
		r := newRegistry()

		_, ok := r.Get("unknown")
		assert.False(t, ok)
	})

	t.Run("List empty", func(t *testing.T) {
		r := newRegistry()

		ids := r.List()
		assert.Empty(t, ids)
	})

	t.Run("List multiple", func(t *testing.T) {
		r := newRegistry()

		r.Register(&Agent{ID: "a"})
		r.Register(&Agent{ID: "b"})
		r.Register(&Agent{ID: "c"})

		ids := r.List()
		assert.Len(t, ids, 3)
		assert.Contains(t, ids, "a")
		assert.Contains(t, ids, "b")
		assert.Contains(t, ids, "c")
	})

	t.Run("Register overwrites", func(t *testing.T) {
		r := newRegistry()

		r.Register(&Agent{ID: "test", Model: "gpt-3"})
		r.Register(&Agent{ID: "test", Model: "gpt-4"})

		got, ok := r.Get("test")
		assert.True(t, ok)
		assert.Equal(t, "gpt-4", got.Model)
	})
}

func TestDefagent(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		wantErr  bool
		errMatch string
	}{
		{
			name:    "minimal agent",
			code:    `(defagent :simple)`,
			wantErr: false,
		},
		{
			name:    "with model",
			code:    `(defagent :assistant :model "gpt-4")`,
			wantErr: false,
		},
		{
			name:    "with system prompt",
			code:    `(defagent :helper :model "gpt-4" :system "You are helpful")`,
			wantErr: false,
		},
		{
			name:    "with temperature as float",
			code:    `(defagent :creative :model "gpt-4" :temperature 0.7)`,
			wantErr: false,
		},
		{
			name:    "with temperature as int",
			code:    `(defagent :creative :model "gpt-4" :temperature 1)`,
			wantErr: false,
		},
		{
			name:    "with max-tokens",
			code:    `(defagent :limited :model "gpt-4" :max-tokens 100)`,
			wantErr: false,
		},
		{
			name:    "with tools as list",
			code:    `(defagent :tooled :model "gpt-4" :tools ["search" "calc"])`,
			wantErr: false,
		},
		{
			name:    "with tools as vector",
			code:    `(defagent :tooled :model "gpt-4" :tools ["search" "calc"])`,
			wantErr: false,
		},
		{
			name:    "with can-delegate as list",
			code:    `(defagent :router :model "gpt-4" :can-delegate [:worker :analyst])`,
			wantErr: false,
		},
		{
			name:    "with can-delegate as vector",
			code:    `(defagent :router :model "gpt-4" :can-delegate [:worker :analyst])`,
			wantErr: false,
		},
		{
			name:    "full config",
			code:    `(defagent :full :model "gpt-4" :system "Be helpful" :temperature 0.5 :max-tokens 500 :tools ["search"] :can-delegate [:helper])`,
			wantErr: false,
		},
		{
			name:     "no arguments",
			code:     `(defagent)`,
			wantErr:  true,
			errMatch: "requires at least an id keyword",
		},
		{
			name:     "id not keyword",
			code:     `(defagent "not-a-keyword")`,
			wantErr:  true,
			errMatch: "must be keyword id",
		},
		{
			name:     "option key not keyword",
			code:     `(defagent :test "not-keyword" "value")`,
			wantErr:  true,
			errMatch: "expected keyword",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLLM{}
			_, env := setupTest(t, mock)

			result, err := eval(env, tt.code)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
				return
			}

			require.NoError(t, err)
			assert.IsType(t, core.Keyword{}, result)
		})
	}
}

func TestAgentRun(t *testing.T) {
	tests := []struct {
		name     string
		setup    string
		code     string
		response string
		wantErr  bool
		errMatch string
	}{
		{
			name:     "successful run",
			setup:    `(defagent :assistant :model "gpt-4")`,
			code:     `(agent/run :assistant "Hello")`,
			response: "Hi there!",
			wantErr:  false,
		},
		{
			name:     "unknown agent",
			setup:    ``,
			code:     `(agent/run :unknown "Hello")`,
			response: "",
			wantErr:  true,
			errMatch: "unknown agent",
		},
		{
			name:     "wrong id type",
			setup:    `(defagent :assistant :model "gpt-4")`,
			code:     `(agent/run "assistant" "Hello")`,
			response: "",
			wantErr:  true,
			errMatch: "must be keyword",
		},
		{
			name:     "wrong prompt type",
			setup:    `(defagent :assistant :model "gpt-4")`,
			code:     `(agent/run :assistant 123)`,
			response: "",
			wantErr:  true,
			errMatch: "must be string",
		},
		{
			name:     "llm error",
			setup:    `(defagent :assistant :model "gpt-4")`,
			code:     `(agent/run :assistant "Hello")`,
			response: "",
			wantErr:  true,
			errMatch: "llm failed",
		},
		{
			name:     "wrong arity",
			setup:    `(defagent :assistant :model "gpt-4")`,
			code:     `(agent/run :assistant)`,
			response: "",
			wantErr:  true,
			errMatch: "requires 2 arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLLM{response: tt.response}
			if tt.name == "llm error" {
				mock.err = fmt.Errorf("llm failed")
			}
			_, env := setupTest(t, mock)

			if tt.setup != "" {
				_, err := eval(env, tt.setup)
				require.NoError(t, err)
			}

			result, err := eval(env, tt.code)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMatch)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, core.String{V: tt.response}, result)
		})
	}
}

func TestAgentRunParallel(t *testing.T) {
	t.Run("successful parallel run with vector", func(t *testing.T) {
		mock := &mockLLM{response: "response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :a :model "gpt-4")`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :b :model "gpt-4")`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/run-parallel [:a :b] "test")`)

		require.NoError(t, err)
		vec, ok := result.(core.Vector)
		require.True(t, ok)
		assert.Len(t, vec.Items, 2)
		assert.Equal(t, core.String{V: "response"}, vec.Items[0])
		assert.Equal(t, core.String{V: "response"}, vec.Items[1])
	})

	t.Run("successful parallel run with quoted list", func(t *testing.T) {
		mock := &mockLLM{response: "response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :a :model "gpt-4")`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :b :model "gpt-4")`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/run-parallel '(:a :b) "test")`)

		require.NoError(t, err)
		vec, ok := result.(core.Vector)
		require.True(t, ok)
		assert.Len(t, vec.Items, 2)
	})

	t.Run("unknown agent in list", func(t *testing.T) {
		mock := &mockLLM{response: "response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :a :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/run-parallel [:a :unknown] "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent")
	})

	t.Run("wrong ids type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-parallel "not-a-list" "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be list or vector")
	})

	t.Run("id not keyword in list", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-parallel ["not-keyword"] "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be keywords")
	})

	t.Run("wrong prompt type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-parallel [:a] 123)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-parallel [:a])`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("llm error", func(t *testing.T) {
		mock := &mockLLM{err: fmt.Errorf("llm failed")}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :a :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/run-parallel [:a] "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "llm failed")
	})
}

func TestAgentRunWithCtx(t *testing.T) {
	t.Run("successful run with context", func(t *testing.T) {
		mock := &mockLLM{response: "contextual response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :assistant :model "gpt-4")`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/run-with-ctx :assistant "Do something" {:user "alice" :task "analysis"})`)

		require.NoError(t, err)
		assert.Equal(t, core.String{V: "contextual response"}, result)
	})

	t.Run("unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx :unknown "test" {} )`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent")
	})

	t.Run("wrong id type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx "assistant" "test" {} )`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be keyword")
	})

	t.Run("wrong prompt type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx :assistant 123 {} )`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("wrong context type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx :assistant "test" "not-a-map")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be hashmap")
	})

	t.Run("wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx :assistant "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 3 arguments")
	})

	t.Run("llm error", func(t *testing.T) {
		mock := &mockLLM{err: fmt.Errorf("llm failed")}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :assistant :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/run-with-ctx :assistant "test" {} )`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "llm failed")
	})
}

func TestAgentList(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		result, err := eval(env, `(agent/list)`)

		require.NoError(t, err)
		list, ok := result.(core.List)
		require.True(t, ok)
		assert.Empty(t, list.Items)
	})

	t.Run("multiple agents", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :a :model "gpt-4")`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :b :model "gpt-4")`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :c :model "gpt-4")`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/list)`)

		require.NoError(t, err)
		list, ok := result.(core.List)
		require.True(t, ok)
		assert.Len(t, list.Items, 3)

		ids := make(map[string]bool)
		for _, item := range list.Items {
			if kw, ok := item.(core.Keyword); ok {
				ids[kw.V] = true
			}
		}
		assert.True(t, ids["a"])
		assert.True(t, ids["b"])
		assert.True(t, ids["c"])
	})

	t.Run("wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/list :unexpected)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "takes no arguments")
	})
}

func TestAgentInfo(t *testing.T) {
	t.Run("returns hashmap for known agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :assistant :model "gpt-4" :system "Be helpful" :temperature 0.7 :max-tokens 100 :tools ["search"] :can-delegate [:worker])`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/info :assistant)`)

		require.NoError(t, err)
		_, ok := result.(*core.HashMap)
		assert.True(t, ok, "result should be a HashMap")
	})

	t.Run("unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/info :unknown)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent")
	})

	t.Run("wrong argument type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/info "not-keyword")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be keyword")
	})

	t.Run("wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/info)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})
}

func TestAgentDelegate(t *testing.T) {
	t.Run("successful delegation", func(t *testing.T) {
		mock := &mockLLM{response: "delegated response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :router :model "gpt-4" :can-delegate [:worker])`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :worker :model "gpt-4")`)
		require.NoError(t, err)

		result, err := eval(env, `(agent/delegate :router :worker "Do work")`)

		require.NoError(t, err)
		assert.Equal(t, core.String{V: "delegated response"}, result)
	})

	t.Run("delegation not allowed", func(t *testing.T) {
		mock := &mockLLM{response: "response"}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :router :model "gpt-4")`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :worker :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/delegate :router :worker "Do work")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot delegate to")
	})

	t.Run("unknown source agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :worker :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/delegate :unknown :worker "Do work")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown source agent")
	})

	t.Run("unknown target agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :router :model "gpt-4" :can-delegate [:unknown])`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/delegate :router :unknown "Do work")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown target agent")
	})

	t.Run("wrong from type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/delegate "not-keyword" :worker "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be keyword")
	})

	t.Run("wrong to type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/delegate :router "not-keyword" "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be keyword")
	})

	t.Run("wrong prompt type", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/delegate :router :worker 123)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/delegate :router :worker)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 3 arguments")
	})

	t.Run("llm error", func(t *testing.T) {
		mock := &mockLLM{err: fmt.Errorf("llm failed")}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :router :model "gpt-4" :can-delegate [:worker])`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :worker :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/delegate :router :worker "Do work")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "llm failed")
	})
}

func TestAgentDelegateDepthLimit(t *testing.T) {
	t.Run("depth limit exceeded", func(t *testing.T) {
		mock := &mockLLM{}
		p := New(mock)
		env := core.NewEnv(nil)
		env.SetEvaluator(core.NewEvaluator())
		require.NoError(t, p.Init(env))

		_, err := eval(env, `(defagent :a :model "gpt-4" :can-delegate [:b])`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :b :model "gpt-4")`)
		require.NoError(t, err)

		ctx := withDelegationDepth(context.Background(), maxDelegationDepth)

		forms, err := core.Read(`(agent/delegate :a :b "test")`)
		require.NoError(t, err)

		_, err = env.Evaluator().Eval(ctx, forms[0], env)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "max delegation depth")
	})

	t.Run("depth just under limit", func(t *testing.T) {
		mock := &mockLLM{response: "ok"}
		p := New(mock)
		env := core.NewEnv(nil)
		env.SetEvaluator(core.NewEvaluator())
		require.NoError(t, p.Init(env))

		_, err := eval(env, `(defagent :a :model "gpt-4" :can-delegate [:b])`)
		require.NoError(t, err)
		_, err = eval(env, `(defagent :b :model "gpt-4")`)
		require.NoError(t, err)

		ctx := withDelegationDepth(context.Background(), maxDelegationDepth-1)

		forms, err := core.Read(`(agent/delegate :a :b "test")`)
		require.NoError(t, err)

		result, err := env.Evaluator().Eval(ctx, forms[0], env)

		require.NoError(t, err)
		assert.Equal(t, core.String{V: "ok"}, result)
	})
}

func TestAgentRunNotFound(t *testing.T) {
	t.Run("run unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run :nonexistent "Hello")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent nonexistent")
	})

	t.Run("run-parallel with unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(defagent :known :model "gpt-4")`)
		require.NoError(t, err)

		_, err = eval(env, `(agent/run-parallel [:known :nonexistent] "test")`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent")
	})

	t.Run("run-with-ctx unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/run-with-ctx :nonexistent "test" {} )`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent nonexistent")
	})

	t.Run("info unknown agent", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/info :nonexistent)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown agent nonexistent")
	})
}

func TestPlugin_Metadata(t *testing.T) {
	mock := &mockLLM{}
	p := New(mock)

	assert.Equal(t, "agent", p.Name())
	assert.Equal(t, "1.0.0", p.Metadata().Version)
	assert.Contains(t, p.Metadata().Description, "agent")
}

func TestRoute(t *testing.T) {
	t.Run("route returns default", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		result, err := eval(env, `(agent/route "some task")`)

		require.NoError(t, err)
		assert.Equal(t, core.Keyword{V: "default"}, result)
	})

	t.Run("route wrong arity", func(t *testing.T) {
		mock := &mockLLM{}
		_, env := setupTest(t, mock)

		_, err := eval(env, `(agent/route)`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})
}

func TestBuildPromptWithContext(t *testing.T) {
	t.Run("builds prompt with context", func(t *testing.T) {
		ctxMap := core.NewHashMap()
		ctxMap.Assoc(core.Keyword{V: "user"}, core.String{V: "alice"})
		ctxMap.Assoc(core.Keyword{V: "role"}, core.String{V: "admin"})

		result := buildPromptWithContext("Do the task", ctxMap)

		assert.Contains(t, result, "Context:")
		assert.Contains(t, result, "Task:")
		assert.Contains(t, result, "Do the task")
	})

	t.Run("empty context", func(t *testing.T) {
		ctxMap := core.NewHashMap()

		result := buildPromptWithContext("Do the task", ctxMap)

		assert.Contains(t, result, "Context:")
		assert.Contains(t, result, "Task:")
		assert.Contains(t, result, "Do the task")
	})
}
