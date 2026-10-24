package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (c *HTTPClient) Stream(ctx context.Context, req LLMRequest) (<-chan LLMChunk, error) {
	ch := make(chan LLMChunk, 10)

	body := map[string]any{
		"model":    req.Model,
		"messages": buildMessages(req),
		"stream":   true,
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/v1/chat/completions",
		bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	go func() {
		defer close(ch)
		defer func() { _ = resp.Body.Close() }()

		sendTerminal := func(err error) {
			select {
			case ch <- LLMChunk{Err: err, Done: true}:
			default:
				select {
				case <-ch:
				default:
				}
				ch <- LLMChunk{Err: err, Done: true}
			}
		}

		send := func(chunk LLMChunk) bool {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				sendTerminal(ctx.Err())
				return false
			}
		}

		scanRawLines := func(data []byte, atEOF bool) (int, []byte, error) {
			if atEOF && len(data) == 0 {
				return 0, nil, nil
			}
			if i := bytes.IndexByte(data, '\n'); i >= 0 {
				return i + 1, data[:i+1], nil
			}
			if atEOF {
				return len(data), data, nil
			}
			return 0, nil, nil
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBodyBytes)
		scanner.Split(scanRawLines)
		var total int
		for scanner.Scan() {
			if ctx.Err() != nil {
				sendTerminal(ctx.Err())
				return
			}

			raw := scanner.Bytes()
			total += len(raw)
			if total > maxResponseBodyBytes {
				send(LLMChunk{Err: fmt.Errorf("stream exceeds %d bytes", maxResponseBodyBytes), Done: true})
				return
			}

			line := string(bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r")))
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				send(LLMChunk{Done: true})
				return
			}

			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason string `json:"finish_reason"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			if len(event.Choices) > 0 {
				content := event.Choices[0].Delta.Content
				done := event.Choices[0].FinishReason != ""
				if content != "" || done {
					if !send(LLMChunk{Content: content, Done: done}) {
						return
					}
					if done {
						return
					}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				send(LLMChunk{Err: fmt.Errorf("stream read: %w", err), Done: true})
			} else {
				sendTerminal(ctx.Err())
			}
			return
		}

		if ctx.Err() != nil {
			sendTerminal(ctx.Err())
			return
		}
		sendTerminal(fmt.Errorf("stream ended without completion"))
	}()

	return ch, nil
}
