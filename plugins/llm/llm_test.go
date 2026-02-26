package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func TestTypes(t *testing.T) {
	t.Run("LLMRequest", func(t *testing.T) {
		req := LLMRequest{
			Model:       "gpt-4",
			System:      "You are helpful",
			User:        "Hello",
			MaxTokens:   100,
			Temperature: 0.7,
		}
		assert.Equal(t, "gpt-4", req.Model)
		assert.Equal(t, 100, req.MaxTokens)
	})

	t.Run("LLMResponse", func(t *testing.T) {
		resp := LLMResponse{
			Content:    "Hi there!",
			StopReason: "stop",
			Usage: TokenUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}
		assert.Equal(t, "Hi there!", resp.Content)
		assert.Equal(t, 15, resp.Usage.TotalTokens)
	})

	t.Run("ToolSpec", func(t *testing.T) {
		params := map[string]any{"type": "object"}
		paramsJSON, _ := json.Marshal(params)
		tool := ToolSpec{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  paramsJSON,
		}
		assert.Equal(t, "get_weather", tool.Name)
	})

	t.Run("LLMChunk", func(t *testing.T) {
		chunk := LLMChunk{Content: "Hello", Done: false}
		assert.Equal(t, "Hello", chunk.Content)
		assert.False(t, chunk.Done)
	})
}

func TestHTTPClient_Complete(t *testing.T) {
	t.Run("successful completion", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/chat/completions", r.URL.Path)
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

			resp := `{
				"choices": [{
					"message": {"content": "Hello!"},
					"finish_reason": "stop"
				}],
				"usage": {"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7}
			}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(resp))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "test-key")
		resp, err := client.Complete(context.Background(), LLMRequest{
			Model:  "gpt-4",
			System: "Be helpful",
			User:   "Hi",
		})

		require.NoError(t, err)
		assert.Equal(t, "Hello!", resp.Content)
		assert.Equal(t, "stop", resp.StopReason)
	})

	t.Run("with tools", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			assert.Contains(t, body, "tools")
			tools := body["tools"].([]any)
			assert.Len(t, tools, 1)

			resp := `{
				"choices": [{
					"message": {
						"content": "",
						"tool_calls": [{
							"id": "call-123",
							"function": {"name": "get_weather", "arguments": "{\"city\": \"Paris\"}"}
						}]
					},
					"finish_reason": "tool_calls"
				}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
			}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(resp))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "test-key")
		params, _ := json.Marshal(map[string]any{"type": "object"})
		resp, err := client.Complete(context.Background(), LLMRequest{
			Model: "gpt-4",
			User:  "What's the weather?",
			Tools: []ToolSpec{
				{Name: "get_weather", Description: "Get weather", Parameters: params},
			},
		})

		require.NoError(t, err)
		assert.Len(t, resp.ToolCalls, 1)
		assert.Equal(t, "get_weather", resp.ToolCalls[0].Name)
		assert.Equal(t, "Paris", resp.ToolCalls[0].Args["city"])
	})

	t.Run("http error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "invalid api key"}`))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "bad-key")
		_, err := client.Complete(context.Background(), LLMRequest{Model: "gpt-4"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})
}

func TestHTTPClient_Embed(t *testing.T) {
	t.Run("successful embedding", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/embeddings", r.URL.Path)

			resp := `{
				"data": [{"embedding": [0.1, 0.2, 0.3]}]
			}`
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(resp))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "test-key")
		embedding, err := client.Embed(context.Background(), "Hello world", "text-embedding-3-small")

		require.NoError(t, err)
		assert.Equal(t, []float64{0.1, 0.2, 0.3}, embedding)
	})

	t.Run("default model", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			assert.Equal(t, "text-embedding-3-small", body["model"])

			resp := `{"data": [{"embedding": [0.5]}]}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(resp))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "test-key")
		_, err := client.Embed(context.Background(), "test", "")

		require.NoError(t, err)
	})
}

func TestHTTPClient_Stream(t *testing.T) {
	t.Run("streaming chunks", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/v1/chat/completions", r.URL.Path)

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)

			fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}")
			fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}")
			fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"!\"},\"finish_reason\":\"stop\"}]}")
			fmt.Fprintln(w, "data: [DONE]")
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "test-key")
		chunks, err := client.Stream(context.Background(), LLMRequest{Model: "gpt-4"})

		require.NoError(t, err)

		var contents []string
		for chunk := range chunks {
			if chunk.Err != nil {
				t.Fatalf("stream error: %v", chunk.Err)
			}
			if chunk.Content != "" {
				contents = append(contents, chunk.Content)
			}
		}

		assert.Equal(t, []string{"Hello", " world", "!"}, contents)
	})

	t.Run("context cancellation", func(t *testing.T) {
		done := make(chan struct{})
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "data: {\"choices\":[{\"delta\":{\"content\":\"start\"}}]}")
			w.(http.Flusher).Flush()
			<-done
		}))
		defer ts.Close()
		defer close(done)

		ctx, cancel := context.WithCancel(context.Background())
		client := NewHTTPClient(ts.URL, "test-key")
		chunks, err := client.Stream(ctx, LLMRequest{Model: "gpt-4"})

		require.NoError(t, err)

		chunk := <-chunks
		assert.Equal(t, "start", chunk.Content)
		cancel()

		for chunk := range chunks {
			if chunk.Err != nil {
				assert.ErrorIs(t, chunk.Err, context.Canceled)
				return
			}
		}
	})
}

type mockClient struct {
	completeResp LLMResponse
	completeErr  error
	streamCh     chan LLMChunk
	streamErr    error
	embedResp    []float64
	embedErr     error
}

func (m *mockClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	return m.completeResp, m.completeErr
}

func (m *mockClient) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan LLMChunk, 10)
	go func() {
		defer close(ch)
		if m.streamCh != nil {
			for chunk := range m.streamCh {
				ch <- chunk
			}
		}
	}()
	return ch, nil
}

func (m *mockClient) Embed(ctx context.Context, text string, model string) ([]float64, error) {
	return m.embedResp, m.embedErr
}

func setupPluginTest(t *testing.T, mock LLMClient) (*Plugin, *core.Env) {
	p := New(mock)
	env := core.NewEnv(nil)
	env.SetEvaluator(core.NewEvaluator())

	stdlibP := stdlib.New()
	require.NoError(t, stdlibP.Init(env))

	require.NoError(t, p.Init(env))
	return p, env
}

func TestPlugin_Complete(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{Content: "Test response"},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/complete "gpt-4" "system" "user")`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, core.String{V: "Test response"}, result)
}

func TestPlugin_CompleteStar(t *testing.T) {
	t.Run("with all options", func(t *testing.T) {
		mock := &mockClient{
			completeResp: LLMResponse{Content: "Response"},
		}
		_, env := setupPluginTest(t, mock)

		forms, err := core.Read(`(llm/complete* {:model "gpt-4" :system "sys" :user "hi" :max-tokens 100 :temperature 0.7})`)
		require.NoError(t, err)

		eval := core.NewEvaluator()
		result, err := eval.Eval(context.Background(), forms[0], env)

		require.NoError(t, err)
		assert.Equal(t, core.String{V: "Response"}, result)
	})

	t.Run("with scoped model", func(t *testing.T) {
		mock := &mockClient{
			completeResp: LLMResponse{Content: "Scoped response"},
		}
		_, env := setupPluginTest(t, mock)

		forms, err := core.Read(`
			(with-model "gpt-4o"
				(llm/complete* {:system "sys" :user "hi"}))
		`)
		require.NoError(t, err)

		eval := core.NewEvaluator()
		result, err := eval.Eval(context.Background(), forms[0], env)

		require.NoError(t, err)
		assert.Equal(t, core.String{V: "Scoped response"}, result)
	})
}

func TestPlugin_Chat(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{Content: "Chat response"},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(llm/chat "gpt-4"
			[{:role "system" :content "Be helpful"}
			 {:role "user" :content "Hello"}])
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, core.String{V: "Chat response"}, result)
}

func TestPlugin_Embed(t *testing.T) {
	mock := &mockClient{
		embedResp: []float64{0.1, 0.2, 0.3},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/embed "Hello world")`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	vec, ok := result.(core.Vector)
	require.True(t, ok)
	assert.Len(t, vec.Items, 3)
	assert.Equal(t, core.Float{V: 0.1}, vec.Items[0])
}

func TestPlugin_Stream(t *testing.T) {
	ch := make(chan LLMChunk, 3)
	ch <- LLMChunk{Content: "Hel"}
	ch <- LLMChunk{Content: "lo"}
	ch <- LLMChunk{Content: "!", Done: true}
	close(ch)

	mock := &mockClient{streamCh: ch}
	_, env := setupPluginTest(t, mock)

	var collected strings.Builder
	env.Set("collect", core.GoFunc{
		Name: "collect",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			m := args[0].(*core.HashMap)
			v, _ := m.Get(core.Keyword{V: "content"})
			if s, ok := v.(core.String); ok {
				collected.WriteString(s.V)
			}
			return core.Nil{}, nil
		},
	})

	forms, err := core.Read(`(llm/stream {:model "gpt-4" :user "hi"} collect)`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, "Hello!", collected.String())
}

func TestPlugin_ToolCall(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{
			ToolCalls: []ToolCall{
				{ID: "call-1", Name: "get_weather", Args: map[string]any{"city": "Paris"}},
			},
		},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(llm/tool-call {:model "gpt-4" :user "Weather in Paris?"}
			[{:name "get_weather" :description "Get weather" :parameters {:type "object"}}])
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	list, ok := result.(core.List)
	require.True(t, ok)
	require.Len(t, list.Items, 1)

	toolMap := list.Items[0].(*core.HashMap)
	name, _ := toolMap.Get(core.Keyword{V: "name"})
	assert.Equal(t, core.String{V: "get_weather"}, name)
}

func TestBootstrapMacros(t *testing.T) {
	_, env := setupPluginTest(t, &mockClient{})

	tests := []struct {
		name     string
		code     string
		expected string
	}{
		{
			name:     "prompt macro",
			code:     `(prompt "Hello " "world" "!")`,
			expected: "\"Hello world!\"",
		},
		{
			name:     "defprompt macro",
			code:     `(do (defprompt greet [name] "Hello, " name "!") (greet "Alice"))`,
			expected: "\"Hello, Alice!\"",
		},
		{
			name:     "with-model returns body",
			code:     `(with-model "gpt-4" "result")`,
			expected: "\"result\"",
		},
		{
			name:     "with-temp returns body",
			code:     `(with-temp 0.7 "done")`,
			expected: "\"done\"",
		},
	}

	eval := core.NewEvaluator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forms, err := core.Read(tt.code)
			require.NoError(t, err)

			result, err := eval.Eval(context.Background(), forms[0], env)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestPlugin_Metadata(t *testing.T) {
	p := New(&mockClient{})

	assert.Equal(t, "llm", p.Name())
	assert.Equal(t, "1.0.0", p.Metadata().Version)
	assert.Contains(t, p.Metadata().Description, "LLM")
}

func TestBuildMessages(t *testing.T) {
	tests := []struct {
		name     string
		req      LLMRequest
		expected []map[string]string
	}{
		{
			name: "system and user",
			req:  LLMRequest{System: "sys", User: "hi"},
			expected: []map[string]string{
				{"role": "system", "content": "sys"},
				{"role": "user", "content": "hi"},
			},
		},
		{
			name: "with messages",
			req: LLMRequest{
				System: "sys",
				Messages: []Message{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "hi"},
				},
			},
			expected: []map[string]string{
				{"role": "system", "content": "sys"},
				{"role": "user", "content": "hello"},
				{"role": "assistant", "content": "hi"},
			},
		},
		{
			name:     "empty request",
			req:      LLMRequest{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMessages(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPlugin_ErrorCases(t *testing.T) {
	_, env := setupPluginTest(t, &mockClient{})
	eval := core.NewEvaluator()

	t.Run("complete wrong arity", func(t *testing.T) {
		forms, _ := core.Read(`(llm/complete "gpt-4")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 3 arguments")
	})

	t.Run("complete wrong types", func(t *testing.T) {
		forms, _ := core.Read(`(llm/complete 1 2 3)`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model must be string")
	})

	t.Run("complete* wrong type", func(t *testing.T) {
		forms, _ := core.Read(`(llm/complete* "not-a-map")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a map")
	})

	t.Run("stream wrong arity", func(t *testing.T) {
		forms, _ := core.Read(`(llm/stream {})`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("stream wrong handler type", func(t *testing.T) {
		forms, _ := core.Read(`(llm/stream {} "not-a-fn")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a function")
	})

	t.Run("chat wrong arity", func(t *testing.T) {
		forms, _ := core.Read(`(llm/chat "gpt-4")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("chat wrong messages type", func(t *testing.T) {
		forms, _ := core.Read(`(llm/chat "gpt-4" "not-a-list")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a list or vector")
	})

	t.Run("embed wrong type", func(t *testing.T) {
		forms, _ := core.Read(`(llm/embed 123)`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be string")
	})

	t.Run("tool-call wrong arity", func(t *testing.T) {
		forms, _ := core.Read(`(llm/tool-call {})`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 2 arguments")
	})

	t.Run("tool-call wrong tools type", func(t *testing.T) {
		forms, _ := core.Read(`(llm/tool-call {} "not-a-list")`)
		_, err := eval.Eval(context.Background(), forms[0], env)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a list or vector")
	})
}

func TestPlugin_CompleteStarWithOptions(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{Content: "Response with messages"},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(llm/complete* {:model "gpt-4"
					   :system "You are helpful"
					   :messages [{:role "user" :content "Hello"}]})
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, core.String{V: "Response with messages"}, result)
}

func TestPlugin_ChatWithVector(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{Content: "Chat response"},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(llm/chat "gpt-4"
			[{:role "user" :content "Hello"}])
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, core.String{V: "Chat response"}, result)
}

func TestPlugin_EmbedWithModel(t *testing.T) {
	mock := &mockClient{
		embedResp: []float64{0.5},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/embed "Hello" "custom-model")`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	vec, ok := result.(core.Vector)
	require.True(t, ok)
	assert.Len(t, vec.Items, 1)
}

func TestPlugin_ToolCallWithVector(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{
			ToolCalls: []ToolCall{
				{Name: "test_fn", Args: map[string]any{"x": 1}},
			},
		},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(llm/tool-call {:model "gpt-4" :user "test"}
			[{:name "test_fn" :description "Test" :parameters {:type "object"}}])
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	list, ok := result.(core.List)
	require.True(t, ok)
	assert.Len(t, list.Items, 1)
}

func TestPlugin_WithTemp(t *testing.T) {
	mock := &mockClient{
		completeResp: LLMResponse{Content: "Scoped temp response"},
	}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`
		(with-temp 0.5
			(llm/complete* {:model "gpt-4" :user "hi"}))
	`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	result, err := eval.Eval(context.Background(), forms[0], env)

	require.NoError(t, err)
	assert.Equal(t, core.String{V: "Scoped temp response"}, result)
}

func TestHTTPClient_EmbedError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal error"}`))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "test-key")
	_, err := client.Embed(context.Background(), "test", "model")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestHTTPClient_CompleteNoChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices": [], "usage": {}}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "test-key")
	_, err := client.Complete(context.Background(), LLMRequest{Model: "gpt-4"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no choices")
}

func TestHTTPClient_EmbedNoData(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"data": []}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "test-key")
	_, err := client.Embed(context.Background(), "test", "model")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no embedding")
}

func TestHTTPClient_CompleteWithStopSeqs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Contains(t, body, "stop")

		resp := `{"choices": [{"message": {"content": "ok"}, "finish_reason": "stop"}], "usage": {}}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "test-key")
	_, err := client.Complete(context.Background(), LLMRequest{
		Model:    "gpt-4",
		StopSeqs: []string{"END"},
	})

	require.NoError(t, err)
}

func TestHTTPClient_CompleteWithTemperature(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		assert.Contains(t, body, "temperature")

		resp := `{"choices": [{"message": {"content": "ok"}, "finish_reason": "stop"}], "usage": {}}`
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "test-key")
	_, err := client.Complete(context.Background(), LLMRequest{
		Model:       "gpt-4",
		Temperature: 0.5,
	})

	require.NoError(t, err)
}

func TestHTTPClient_StreamError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "bad-key")
	_, err := client.Stream(context.Background(), LLMRequest{Model: "gpt-4"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestPlugin_CompleteClientError(t *testing.T) {
	mock := &mockClient{completeErr: fmt.Errorf("network error")}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/complete "gpt-4" "sys" "user")`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "network error")
}

func TestPlugin_CompleteStarClientError(t *testing.T) {
	mock := &mockClient{completeErr: fmt.Errorf("api error")}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/complete* {:model "gpt-4"})`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "api error")
}

func TestPlugin_EmbedClientError(t *testing.T) {
	mock := &mockClient{embedErr: fmt.Errorf("embed error")}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/embed "test")`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "embed error")
}

func TestPlugin_StreamClientError(t *testing.T) {
	mock := &mockClient{streamErr: fmt.Errorf("stream error")}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/stream {:model "gpt-4"} (fn [c] nil))`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream error")
}

func TestPlugin_ToolCallClientError(t *testing.T) {
	mock := &mockClient{completeErr: fmt.Errorf("tool error")}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/tool-call {:model "gpt-4"} [])`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool error")
}

func TestPlugin_ChatInvalidMessageMap(t *testing.T) {
	mock := &mockClient{completeResp: LLMResponse{Content: "ok"}}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/chat "gpt-4" ["not-a-map"])`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be maps")
}

func TestPlugin_ToolCallInvalidToolMap(t *testing.T) {
	mock := &mockClient{completeResp: LLMResponse{}}
	_, env := setupPluginTest(t, mock)

	forms, err := core.Read(`(llm/tool-call {:model "gpt-4"} ["not-a-map"])`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be maps")
}

func TestPlugin_EmbedWrongArity(t *testing.T) {
	_, env := setupPluginTest(t, &mockClient{})

	forms, err := core.Read(`(llm/embed)`)
	require.NoError(t, err)

	eval := core.NewEvaluator()
	_, err = eval.Eval(context.Background(), forms[0], env)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires 1-2 arguments")
}
