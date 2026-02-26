package net

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) buildRequest(method, urlStr string, opts *core.HashMap) (*http.Request, error) {
	var body io.Reader
	var contentType string

	if opts != nil {
		if b, ok := opts.Get(core.Keyword{V: "body"}); ok {
			switch v := b.(type) {
			case core.String:
				body = strings.NewReader(v.V)
			case *core.HashMap:
				goVal, err := lispToGo(v)
				if err != nil {
					return nil, fmt.Errorf("http: convert body: %w", err)
				}
				jsonData, err := json.Marshal(goVal)
				if err != nil {
					return nil, fmt.Errorf("http: encode body: %w", err)
				}
				body = bytes.NewReader(jsonData)
				contentType = "application/json"
			}
		}
	}

	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("http: create request: %w", err)
	}

	if opts != nil {
		if qm, ok := extractMap(opts, "query"); ok {
			query := req.URL.Query()
			qm.Each(func(k, v core.Value) {
				var keyStr string
				switch kk := k.(type) {
				case core.Keyword:
					keyStr = kk.V
				case core.String:
					keyStr = kk.V
				}
				if keyStr != "" {
					if vs, ok := v.(core.String); ok {
						query.Set(keyStr, vs.V)
					}
				}
			})
			req.URL.RawQuery = query.Encode()
		}
	}

	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	if opts != nil {
		if hm, ok := extractMap(opts, "headers"); ok {
			hm.Each(func(k, v core.Value) {
				var keyStr string
				switch kk := k.(type) {
				case core.Keyword:
					keyStr = kk.V
				case core.String:
					keyStr = kk.V
				}
				if keyStr != "" {
					if vs, ok := v.(core.String); ok {
						req.Header.Set(keyStr, vs.V)
					}
				}
			})
		}
	}

	return req, nil
}
