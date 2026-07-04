package net

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

// maxResponseBytes caps how much of a response body doRequest reads, so a
// malicious or misbehaving server cannot exhaust memory via an unbounded
// response.
const maxResponseBytes = 32 << 20

func (p *Plugin) doRequest(ctx context.Context, req *http.Request, opts *core.HashMap) (core.Value, error) {
	client := p.client

	if opts != nil {
		if timeoutMs, ok := extractInt(opts, "timeout"); ok && timeoutMs > 0 {
			client = &http.Client{
				Timeout: time.Duration(timeoutMs) * time.Millisecond,
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			}
		}
	}

	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("http: read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("http: response body exceeds %d byte limit", maxResponseBytes)
	}

	result := core.NewHashMap()
	result, _ = result.Assoc(core.Keyword{V: "status"}, core.Int{V: int64(resp.StatusCode)})

	headers := core.NewHashMap()
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers, _ = headers.Assoc(core.String{V: k}, core.String{V: v[0]})
		}
	}
	result, _ = result.Assoc(core.Keyword{V: "headers"}, headers)

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") && len(body) > 0 {
		var jsonData any
		if err := json.Unmarshal(body, &jsonData); err == nil {
			bodyVal, err := goToLisp(jsonData)
			if err != nil {
				result, _ = result.Assoc(core.Keyword{V: "body"}, core.String{V: string(body)})
			} else {
				result, _ = result.Assoc(core.Keyword{V: "body"}, bodyVal)
			}
		} else {
			result, _ = result.Assoc(core.Keyword{V: "body"}, core.String{V: string(body)})
		}
	} else {
		result, _ = result.Assoc(core.Keyword{V: "body"}, core.String{V: string(body)})
	}

	return result, nil
}
