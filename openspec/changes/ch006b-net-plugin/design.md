# Design Document: Net Plugin

**Change ID:** 006b-net-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package net

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"
    "time"
    
    "github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
    client *http.Client
}

func New() *Plugin {
    return &Plugin{
        client: &http.Client{
            Timeout: 30 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
            },
        },
    }
}

func (p *Plugin) Name() string {
    return "net"
}

func (p *Plugin) Metadata() core.PluginMeta {
    return core.PluginMeta{
        Version:     "1.0.0",
        Description: "HTTP client plugin",
        Author:      "go-lispico team",
    }
}

func (p *Plugin) Init(env *core.Env) error {
    env.Set("http/get", core.GoFunc{
        Name: "http/get",
        Fn:   p.get,
    })
    
    env.Set("http/post", core.GoFunc{
        Name: "http/post",
        Fn:   p.post,
    })
    
    env.Set("http/fetch", core.GoFunc{
        Name: "http/fetch",
        Fn:   p.fetch,
    })
    
    return nil
}
```

---

## 2. Function Implementations

### http/get

```go
func (p *Plugin) get(args []core.Value) (core.Value, error) {
    if len(args) < 1 || len(args) > 2 {
        return nil, fmt.Errorf("http/get: requires 1-2 arguments")
    }
    
    urlStr, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("http/get: first argument must be string")
    }
    
    // Parse options
    var opts *core.HashMap
    if len(args) == 2 {
        var ok bool
        opts, ok = args[1].(*core.HashMap)
        if !ok {
            return nil, fmt.Errorf("http/get: second argument must be map")
        }
    }
    
    // Build request
    req, err := p.buildRequest("GET", urlStr.V, opts)
    if err != nil {
        return nil, err
    }
    
    // Execute
    return p.doRequest(req, opts)
}
```

### http/post

```go
func (p *Plugin) post(args []core.Value) (core.Value, error) {
    if len(args) < 1 || len(args) > 2 {
        return nil, fmt.Errorf("http/post: requires 1-2 arguments")
    }
    
    urlStr, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("http/post: first argument must be string")
    }
    
    var opts *core.HashMap
    if len(args) == 2 {
        var ok bool
        opts, ok = args[1].(*core.HashMap)
        if !ok {
            return nil, fmt.Errorf("http/post: second argument must be map")
        }
    }
    
    req, err := p.buildRequest("POST", urlStr.V, opts)
    if err != nil {
        return nil, err
    }
    
    return p.doRequest(req, opts)
}
```

### http/fetch

```go
func (p *Plugin) fetch(args []core.Value) (core.Value, error) {
    if len(args) < 1 || len(args) > 2 {
        return nil, fmt.Errorf("http/fetch: requires 1-2 arguments")
    }
    
    urlStr, ok := args[0].(core.String)
    if !ok {
        return nil, fmt.Errorf("http/fetch: first argument must be string")
    }
    
    var opts *core.HashMap
    if len(args) == 2 {
        var ok bool
        opts, ok = args[1].(*core.HashMap)
        if !ok {
            return nil, fmt.Errorf("http/fetch: second argument must be map")
        }
    }
    
    // Extract method from opts, default to GET
    method := "GET"
    if opts != nil {
        if m, ok := opts.Get(core.Keyword{V: "method"}); ok {
            if ms, ok := m.(core.String); ok {
                method = ms.V
            } else if mk, ok := m.(core.Keyword); ok {
                method = strings.ToUpper(mk.V)
            }
        }
    }
    
    req, err := p.buildRequest(method, urlStr.V, opts)
    if err != nil {
        return nil, err
    }
    
    return p.doRequest(req, opts)
}
```

---

## 3. Request Building

```go
func (p *Plugin) buildRequest(method, urlStr string, opts *core.HashMap) (*http.Request, error) {
    var body io.Reader
    var contentType string
    
    if opts != nil {
        // Check for body
        if b, ok := opts.Get(core.Keyword{V: "body"}); ok {
            switch v := b.(type) {
            case core.String:
                body = strings.NewReader(v.V)
            case *core.HashMap:
                // JSON encode
                jsonData, err := json.Marshal(hashMapToGo(v))
                if err != nil {
                    return nil, err
                }
                body = bytes.NewReader(jsonData)
                contentType = "application/json"
            }
        }
    }
    
    req, err := http.NewRequest(method, urlStr, body)
    if err != nil {
        return nil, err
    }
    
    // Add query params
    if opts != nil {
        if q, ok := opts.Get(core.Keyword{V: "query"}); ok {
            if qm, ok := q.(*core.HashMap); ok {
                query := req.URL.Query()
                for k, v := range qm.M {
                    query.Set(k.String(), v.String())
                }
                req.URL.RawQuery = query.Encode()
            }
        }
    }
    
    // Add headers
    if contentType != "" {
        req.Header.Set("Content-Type", contentType)
    }
    
    if opts != nil {
        if h, ok := opts.Get(core.Keyword{V: "headers"}); ok {
            if hm, ok := h.(*core.HashMap); ok {
                for k, v := range hm.M {
                    req.Header.Set(k.String(), v.String())
                }
            }
        }
    }
    
    return req, nil
}

func (p *Plugin) doRequest(req *http.Request, opts *core.HashMap) (core.Value, error) {
    // Apply timeout if specified
    var client *http.Client
    if opts != nil {
        if t, ok := opts.Get(core.Keyword{V: "timeout"}); ok {
            var timeoutMs int64
            switch v := t.(type) {
            case core.Int:
                timeoutMs = v.V
            case core.Float:
                timeoutMs = int64(v.V)
            }
            
            timeout := time.Duration(timeoutMs) * time.Millisecond
            client = &http.Client{Timeout: timeout}
        }
    }
    
    if client == nil {
        client = p.client
    }
    
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("http request failed: %w", err)
    }
    defer resp.Body.Close()
    
    // Read body
    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read response body: %w", err)
    }
    
    // Build response map
    result := core.NewHashMap()
    result.M[core.Keyword{V: "status"}] = core.Int{V: int64(resp.StatusCode)}
    
    // Headers
    headers := core.NewHashMap()
    for k, v := range resp.Header {
        if len(v) > 0 {
            headers.M[core.String{V: k}] = core.String{V: v[0]}
        }
    }
    result.M[core.Keyword{V: "headers"}] = headers
    
    // Body - try JSON parse if content-type is JSON
    contentType := resp.Header.Get("Content-Type")
    if strings.Contains(contentType, "application/json") {
        var jsonData any
        if err := json.Unmarshal(body, &jsonData); err == nil {
            result.M[core.Keyword{V: "body"}] = goToValue(jsonData)
        } else {
            result.M[core.Keyword{V: "body"}] = core.String{V: string(body)}
        }
    } else {
        result.M[core.Keyword{V: "body"}] = core.String{V: string(body)}
    }
    
    return result, nil
}
```

---

## 4. Helpers

```go
func hashMapToGo(m *core.HashMap) map[string]any {
    result := make(map[string]any)
    for k, v := range m.M {
        key := k.String()
        if kw, ok := k.(core.Keyword); ok {
            key = kw.V
        }
        result[key] = valueToGo(v)
    }
    return result
}

func valueToGo(v core.Value) any {
    switch val := v.(type) {
    case core.Nil:
        return nil
    case core.Bool:
        return val.V
    case core.Int:
        return val.V
    case core.Float:
        return val.V
    case core.String:
        return val.V
    case core.Keyword:
        return val.V
    case core.List:
        items := make([]any, len(val.Items))
        for i, item := range val.Items {
            items[i] = valueToGo(item)
        }
        return items
    case core.Vector:
        items := make([]any, len(val.Items))
        for i, item := range val.Items {
            items[i] = valueToGo(item)
        }
        return items
    case *core.HashMap:
        return hashMapToGo(val)
    default:
        return v.String()
    }
}

func goToValue(v any) core.Value {
    switch val := v.(type) {
    case nil:
        return core.Nil{}
    case bool:
        return core.Bool{V: val}
    case float64:
        return core.Float{V: val}
    case string:
        return core.String{V: val}
    case []any:
        items := make([]core.Value, len(val))
        for i, item := range val {
            items[i] = goToValue(item)
        }
        return core.Vector{Items: items}
    case map[string]any:
        m := core.NewHashMap()
        for k, v := range val {
            m.M[core.Keyword{V: k}] = goToValue(v)
        }
        return m
    default:
        return core.String{V: fmt.Sprintf("%v", v)}
    }
}
```

---

## 5. File Organization

```
plugins/net/
├── plugin.go         # Main plugin
├── request.go        # Request building
├── response.go       # Response parsing
├── helpers.go        # Type conversions
└── net_test.go       # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).
