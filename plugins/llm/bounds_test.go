package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/victorzhuk/go-lispico/core"
)

const secretErrorBody = "SECRET-PROVIDER-INTERNAL-DETAILS"

func waitContextDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server request context not released within 5s")
	}
}

func TestCompleteBodyCap(t *testing.T) {
	valid := `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`
	exact := strings.Repeat(" ", maxResponseBodyBytes-len(valid)) + valid

	t.Run("exact limit succeeds", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(exact))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		resp, err := client.Complete(context.Background(), LLMRequest{Model: "m"})
		require.NoError(t, err)
		assert.Equal(t, "ok", resp.Content)
	})

	t.Run("over limit fails closed", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(exact + "x"))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		_, err := client.Complete(context.Background(), LLMRequest{Model: "m"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("exceeds %d", maxResponseBodyBytes))
	})

	t.Run("status error body not reflected", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, secretErrorBody, http.StatusInternalServerError)
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		_, err := client.Complete(context.Background(), LLMRequest{Model: "m"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http 500")
		assert.NotContains(t, err.Error(), secretErrorBody)
	})
}

func TestEmbedBodyCap(t *testing.T) {
	embedding := `{"data":[{"embedding":[0.1,0.2]}]}`

	t.Run("exact limit succeeds", func(t *testing.T) {
		body := strings.Repeat(" ", maxResponseBodyBytes-len(embedding)) + embedding
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		vec, err := client.Embed(context.Background(), "hi", "")
		require.NoError(t, err)
		assert.Equal(t, []float64{0.1, 0.2}, vec)
	})

	t.Run("over limit fails closed", func(t *testing.T) {
		body := strings.Repeat(" ", maxResponseBodyBytes-len(embedding)+1) + embedding
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		_, err := client.Embed(context.Background(), "hi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), fmt.Sprintf("exceeds %d", maxResponseBodyBytes))
	})

	t.Run("status error body not reflected", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, secretErrorBody, http.StatusTooManyRequests)
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		_, err := client.Embed(context.Background(), "hi", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "http 429")
		assert.NotContains(t, err.Error(), secretErrorBody)
	})
}

func TestStreamBodyCap(t *testing.T) {
	chunk := `data: {"choices":[{"delta":{"content":"a"}}]}`

	t.Run("exact limit succeeds", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			pad := "x" + strings.Repeat("y", 98)
			for range (maxResponseBodyBytes - 10*(len(chunk)+1) - len("data: [DONE]") - 1) / (len(pad) + 1) {
				fmt.Fprintln(w, pad)
			}
			for range 10 {
				fmt.Fprintln(w, chunk)
			}
			fmt.Fprintln(w, "data: [DONE]")
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
		require.NoError(t, err)

		var contents []string
		for c := range chunks {
			require.NoError(t, c.Err)
			if c.Content != "" {
				contents = append(contents, c.Content)
			}
		}
		assert.Len(t, contents, 10)
	})

	t.Run("CRLF lines counted exactly", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat(chunk+"\r\n", 10) + "data: [DONE]\r\n"))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
		require.NoError(t, err)

		var contents []string
		for c := range chunks {
			require.NoError(t, c.Err)
			if c.Content != "" {
				contents = append(contents, c.Content)
			}
		}
		assert.Len(t, contents, 10)
	})

	t.Run("unterminated final line within cap succeeds", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat(chunk+"\n", 10) + `data: {"choices":[{"delta":{"content":"tail"},"finish_reason":"stop"}]}`))
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
		require.NoError(t, err)

		var last LLMChunk
		for c := range chunks {
			require.NoError(t, c.Err)
			if c.Content != "" {
				last = c
			}
		}
		assert.Equal(t, "tail", last.Content)
		assert.True(t, last.Done)
	})

	t.Run("over limit fails closed without over-limit content", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for range maxResponseBodyBytes/(len(chunk)+1) + 10 {
				fmt.Fprintln(w, chunk)
			}
			w.(http.Flusher).Flush()
		}))
		defer ts.Close()

		client := NewHTTPClient(ts.URL, "k")
		chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
		require.NoError(t, err)

		var chunkErr error
		count := 0
		for c := range chunks {
			if c.Err != nil {
				chunkErr = c.Err
				break
			}
			count++
		}
		require.Error(t, chunkErr)
		assert.Contains(t, chunkErr.Error(), fmt.Sprintf("exceeds %d", maxResponseBodyBytes))
		assert.LessOrEqual(t, count, maxResponseBodyBytes/(len(chunk)+1))
	})
}

func TestStreamLargeSingleDataLine(t *testing.T) {
	payload := strings.Repeat("x", 128*1024)
	line := `data: {"choices":[{"delta":{"content":"` + payload + `"}}]}` + "\n"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(line))
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "k")
	chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
	require.NoError(t, err)

	var got string
	doneCount := 0
	for c := range chunks {
		require.NoError(t, c.Err)
		got += c.Content
		if c.Done {
			doneCount++
		}
	}
	assert.Equal(t, payload, got)
	assert.Equal(t, 1, doneCount)
}

func TestStreamStatusErrorBodyNotReflected(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, secretErrorBody, http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "k")
	_, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http 500")
	assert.NotContains(t, err.Error(), secretErrorBody)
}

func TestStreamCleanEOFWithoutDone(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"partial"}}]}`)
		w.(http.Flusher).Flush()
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "k")
	chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
	require.NoError(t, err)

	var last LLMChunk
	sawClose := false
	for {
		c, ok := <-chunks
		if !ok {
			sawClose = true
			break
		}
		last = c
	}
	require.True(t, sawClose)
	require.Error(t, last.Err)
	assert.Contains(t, last.Err.Error(), "stream ended without completion")
	assert.True(t, last.Done)

	p := New(client)
	env := core.NewEnv(nil)
	env.SetEvaluator(core.NewEvaluator())
	require.NoError(t, p.Init(env))
	env.Set("noop", core.GoFunc{Name: "noop", Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
		return core.Nil{}, nil
	}})
	forms, err := core.Read(`(llm/stream {:model "m"} noop)`)
	require.NoError(t, err)
	_, err = core.NewEvaluator().Eval(context.Background(), forms[0], env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stream ended without completion")
}

func TestStreamFinishReasonThenDoneSingleTerminal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"a"},"finish_reason":"stop"}]}`)
		fmt.Fprintln(w, `data: [DONE]`)
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "k")
	chunks, err := client.Stream(context.Background(), LLMRequest{Model: "m"})
	require.NoError(t, err)

	doneCount := 0
	errCount := 0
	for c := range chunks {
		if c.Done {
			doneCount++
		}
		if c.Err != nil {
			errCount++
		}
	}
	assert.Equal(t, 1, doneCount)
	assert.Equal(t, 0, errCount)
}

func TestStreamHandlerAbandonment(t *testing.T) {
	hold := make(chan struct{})
	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"first"}}]}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()
	defer close(hold)

	client := NewHTTPClient(ts.URL, "k")
	p := New(client)
	env := core.NewEnv(nil)
	env.SetEvaluator(core.NewEvaluator())
	require.NoError(t, p.Init(env))

	failing := core.GoFunc{
		Name: "fail",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			return nil, fmt.Errorf("handler boom")
		},
	}
	env.Set("fail", failing)

	forms, err := core.Read(`(llm/stream {:model "m"} fail)`)
	require.NoError(t, err)

	_, err = core.NewEvaluator().Eval(context.Background(), forms[0], env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler boom")

	waitContextDone(t, serverDone)
}

func TestStreamCancellationTerminalChunkWithFullBuffer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for range 30 {
			fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"x"}}]}`)
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := NewHTTPClient(ts.URL, "k")
	chunks, err := client.Stream(ctx, LLMRequest{Model: "m"})
	require.NoError(t, err)

	readChunk := func(t *testing.T) (LLMChunk, bool) {
		t.Helper()
		select {
		case c, ok := <-chunks:
			return c, ok
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for stream chunk")
			return LLMChunk{}, false
		}
	}

	for range 20 {
		c, ok := readChunk(t)
		require.True(t, ok)
		require.NoError(t, c.Err)
	}

	cancel()

	var last LLMChunk
	sawClose := false
	for {
		c, ok := readChunk(t)
		if !ok {
			sawClose = true
			break
		}
		last = c
	}
	require.True(t, sawClose)
	require.Error(t, last.Err)
	assert.Contains(t, last.Err.Error(), "context canceled")
	assert.True(t, last.Done)
}

func TestStreamCancellationWhileBlocked(t *testing.T) {
	firstChunk := make(chan struct{}, 1)
	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(serverDone)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"first"}}]}`)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer ts.Close()

	client := NewHTTPClient(ts.URL, "k")
	p := New(client)
	env := core.NewEnv(nil)
	env.SetEvaluator(core.NewEvaluator())
	require.NoError(t, p.Init(env))

	counter := core.GoFunc{
		Name: "count",
		Fn: func(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
			select {
			case firstChunk <- struct{}{}:
			default:
			}
			return core.Nil{}, nil
		},
	}
	env.Set("count", counter)

	forms, err := core.Read(`(llm/stream {:model "m"} count)`)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	evalDone := make(chan error, 1)
	go func() {
		_, err := core.NewEvaluator().Eval(ctx, forms[0], env)
		evalDone <- err
	}()

	select {
	case <-firstChunk:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("handler never received the first flushed chunk")
	}

	cancel()

	select {
	case err := <-evalDone:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(5 * time.Second):
		t.Fatal("llm/stream did not return after cancellation")
	}

	waitContextDone(t, serverDone)
}
