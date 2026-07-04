package net

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestPlugin_New(t *testing.T) {
	p := New()
	assert.NotNil(t, p)
	assert.NotNil(t, p.client)
	assert.Equal(t, "net", p.Name())
}

func TestPlugin_Metadata(t *testing.T) {
	p := New()
	meta := p.Metadata()
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Contains(t, meta.Description, "HTTP")
}

func TestPlugin_Init(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	err := p.Init(env)
	require.NoError(t, err)

	_, ok := env.Get("http/get")
	assert.True(t, ok, "http/get should be registered")

	_, ok = env.Get("http/post")
	assert.True(t, ok, "http/post should be registered")

	_, ok = env.Get("http/fetch")
	assert.True(t, ok, "http/fetch should be registered")
}

func TestPlugin_Get(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		url        string
		opts       *core.HashMap
		wantStatus int64
		wantBody   string
		wantErr    bool
	}{
		{
			name: "simple get",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("hello"))
			},
			wantStatus: 200,
			wantBody:   "hello",
		},
		{
			name: "get with query params",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test", r.URL.Query().Get("key"))
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				qm := core.NewHashMap()
				qm, _ = qm.Assoc(core.Keyword{V: "key"}, core.String{V: "test"})
				m, _ = m.Assoc(core.Keyword{V: "query"}, qm)
				return m
			}(),
			wantStatus: 200,
			wantBody:   "ok",
		},
		{
			name: "get with headers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "test-value", r.Header.Get("X-Custom"))
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				hm := core.NewHashMap()
				hm, _ = hm.Assoc(core.Keyword{V: "X-Custom"}, core.String{V: "test-value"})
				m, _ = m.Assoc(core.Keyword{V: "headers"}, hm)
				return m
			}(),
			wantStatus: 200,
			wantBody:   "ok",
		},
		{
			name: "get with json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{"message": "hello"})
			},
			wantStatus: 200,
		},
		{
			name: "get 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("not found"))
			},
			wantStatus: 404,
			wantBody:   "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			p := New()
			args := []core.Value{core.String{V: srv.URL}}
			if tt.opts != nil {
				args = append(args, tt.opts)
			}

			result, err := p.get(context.Background(), nil, args, nil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			resultMap, ok := result.(*core.HashMap)
			require.True(t, ok, "result should be HashMap")

			status, ok := resultMap.Get(core.Keyword{V: "status"})
			require.True(t, ok)
			assert.Equal(t, tt.wantStatus, status.(core.Int).V)

			if tt.wantBody != "" {
				body, ok := resultMap.Get(core.Keyword{V: "body"})
				require.True(t, ok)
				assert.Equal(t, tt.wantBody, body.(core.String).V)
			}
		})
	}
}

func TestPlugin_Post(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		opts       *core.HashMap
		wantStatus int64
		wantErr    bool
	}{
		{
			name: "post with string body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("created"))
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				m, _ = m.Assoc(core.Keyword{V: "body"}, core.String{V: "test data"})
				return m
			}(),
			wantStatus: 201,
		},
		{
			name: "post with json body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.Header.Get("Content-Type"), "application/json")
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("created"))
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				bodyMap := core.NewHashMap()
				bodyMap, _ = bodyMap.Assoc(core.Keyword{V: "name"}, core.String{V: "test"})
				m, _ = m.Assoc(core.Keyword{V: "body"}, bodyMap)
				return m
			}(),
			wantStatus: 201,
		},
		{
			name: "post with json response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{"id": 123})
			},
			wantStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			p := New()
			args := []core.Value{core.String{V: srv.URL}}
			if tt.opts != nil {
				args = append(args, tt.opts)
			}

			result, err := p.post(context.Background(), nil, args, nil)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			resultMap, ok := result.(*core.HashMap)
			require.True(t, ok)

			status, ok := resultMap.Get(core.Keyword{V: "status"})
			require.True(t, ok)
			assert.Equal(t, tt.wantStatus, status.(core.Int).V)
		})
	}
}

func TestPlugin_Fetch(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		handler    http.HandlerFunc
		opts       *core.HashMap
		wantStatus int64
	}{
		{
			name:   "default get",
			method: "",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method)
				w.WriteHeader(http.StatusOK)
			},
			wantStatus: 200,
		},
		{
			name:   "put method",
			method: "PUT",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "PUT", r.Method)
				w.WriteHeader(http.StatusOK)
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				m, _ = m.Assoc(core.Keyword{V: "method"}, core.String{V: "PUT"})
				return m
			}(),
			wantStatus: 200,
		},
		{
			name:   "delete method",
			method: "DELETE",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "DELETE", r.Method)
				w.WriteHeader(http.StatusOK)
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				m, _ = m.Assoc(core.Keyword{V: "method"}, core.Keyword{V: "delete"})
				return m
			}(),
			wantStatus: 200,
		},
		{
			name:   "patch method",
			method: "PATCH",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "PATCH", r.Method)
				w.WriteHeader(http.StatusOK)
			},
			opts: func() *core.HashMap {
				m := core.NewHashMap()
				m, _ = m.Assoc(core.Keyword{V: "method"}, core.String{V: "patch"})
				return m
			}(),
			wantStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			p := New()
			args := []core.Value{core.String{V: srv.URL}}
			if tt.opts != nil {
				args = append(args, tt.opts)
			}

			result, err := p.fetch(context.Background(), nil, args, nil)
			require.NoError(t, err)

			resultMap, ok := result.(*core.HashMap)
			require.True(t, ok)

			status, ok := resultMap.Get(core.Keyword{V: "status"})
			require.True(t, ok)
			assert.Equal(t, tt.wantStatus, status.(core.Int).V)
		})
	}
}

func TestPlugin_ArgumentErrors(t *testing.T) {
	p := New()

	t.Run("get wrong arg count", func(t *testing.T) {
		_, err := p.get(context.Background(), nil, []core.Value{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1-2")
	})

	t.Run("get wrong url type", func(t *testing.T) {
		_, err := p.get(context.Background(), nil, []core.Value{core.Int{V: 123}}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "url must be string")
	})

	t.Run("get wrong opts type", func(t *testing.T) {
		_, err := p.get(context.Background(), nil, []core.Value{core.String{V: "http://test"}, core.Int{V: 123}}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "opts must be map")
	})

	t.Run("post wrong arg count", func(t *testing.T) {
		_, err := p.post(context.Background(), nil, []core.Value{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1-2")
	})

	t.Run("fetch wrong arg count", func(t *testing.T) {
		_, err := p.fetch(context.Background(), nil, []core.Value{}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1-2")
	})
}

func TestPlugin_Timeout(t *testing.T) {
	p := New()

	t.Run("network error", func(t *testing.T) {
		_, err := p.get(context.Background(), nil, []core.Value{core.String{V: "http://127.0.0.1:1"}}, nil)
		require.Error(t, err)
	})
}

func TestPlugin_HeadersInResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "test-value")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	headers, ok := resultMap.Get(core.Keyword{V: "headers"})
	require.True(t, ok)

	headersMap := headers.(*core.HashMap)
	customHeader, ok := headersMap.Get(core.String{V: "X-Custom"})
	require.True(t, ok)
	assert.Equal(t, "test-value", customHeader.(core.String).V)
}

func TestPlugin_JSONResponseParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":     123,
			"name":   "test",
			"active": true,
			"items":  []any{1, 2, 3},
			"nested": map[string]any{"key": "value"},
		})
	}))
	defer srv.Close()

	p := New()
	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	body, ok := resultMap.Get(core.Keyword{V: "body"})
	require.True(t, ok)

	bodyMap, ok := body.(*core.HashMap)
	require.True(t, ok, "body should be parsed as HashMap")

	id, ok := bodyMap.Get(core.Keyword{V: "id"})
	require.True(t, ok)
	assert.Equal(t, float64(123), id.(core.Float).V)

	name, ok := bodyMap.Get(core.Keyword{V: "name"})
	require.True(t, ok)
	assert.Equal(t, "test", name.(core.String).V)

	active, ok := bodyMap.Get(core.Keyword{V: "active"})
	require.True(t, ok)
	assert.True(t, active.(core.Bool).V)
}

func TestPlugin_CustomTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	opts := core.NewHashMap()
	opts, _ = opts.Assoc(core.Keyword{V: "timeout"}, core.Int{V: 5000})

	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}, opts}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}

func TestPlugin_PostWithoutOpts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	result, err := p.post(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}

func TestPlugin_FetchWithoutOpts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	result, err := p.fetch(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}

func TestPlugin_InvalidURL(t *testing.T) {
	p := New()
	_, err := p.get(context.Background(), nil, []core.Value{core.String{V: "://invalid"}}, nil)
	require.Error(t, err)
}

func TestPlugin_HeadersWithStringKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-value", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	opts := core.NewHashMap()
	hm := core.NewHashMap()
	hm, _ = hm.Assoc(core.String{V: "X-Custom"}, core.String{V: "test-value"})
	opts, _ = opts.Assoc(core.Keyword{V: "headers"}, hm)

	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}, opts}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}

func TestPlugin_QueryWithStringKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test", r.URL.Query().Get("key"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	opts := core.NewHashMap()
	qm := core.NewHashMap()
	qm, _ = qm.Assoc(core.String{V: "key"}, core.String{V: "test"})
	opts, _ = opts.Assoc(core.Keyword{V: "query"}, qm)

	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}, opts}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}

func TestPlugin_InvalidJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json {"))
	}))
	defer srv.Close()

	p := New()
	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	body, ok := resultMap.Get(core.Keyword{V: "body"})
	require.True(t, ok)
	assert.Equal(t, "not valid json {", body.(core.String).V)
}

func TestPlugin_EmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := New()
	result, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	body, ok := resultMap.Get(core.Keyword{V: "body"})
	require.True(t, ok)
	assert.Equal(t, "", body.(core.String).V)
}

func TestPlugin_ResponseBodyTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		chunk := make([]byte, 1<<20)
		for written := 0; written <= maxResponseBytes; written += len(chunk) {
			w.Write(chunk)
		}
	}))
	defer srv.Close()

	p := New()
	_, err := p.get(context.Background(), nil, []core.Value{core.String{V: srv.URL}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
}

func TestPlugin_ExistingContentTypeNotOverridden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "text/plain", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	p := New()
	opts := core.NewHashMap()
	bodyMap := core.NewHashMap()
	bodyMap, _ = bodyMap.Assoc(core.Keyword{V: "name"}, core.String{V: "test"})
	opts, _ = opts.Assoc(core.Keyword{V: "body"}, bodyMap)
	hm := core.NewHashMap()
	hm, _ = hm.Assoc(core.Keyword{V: "Content-Type"}, core.String{V: "text/plain"})
	opts, _ = opts.Assoc(core.Keyword{V: "headers"}, hm)

	result, err := p.post(context.Background(), nil, []core.Value{core.String{V: srv.URL}, opts}, nil)
	require.NoError(t, err)

	resultMap := result.(*core.HashMap)
	status, ok := resultMap.Get(core.Keyword{V: "status"})
	require.True(t, ok)
	assert.Equal(t, int64(200), status.(core.Int).V)
}
