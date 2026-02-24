# Change Proposal: Net Plugin

**Change ID:** 006b-net-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the Net plugin for HTTP client operations. Provides simple, context-aware HTTP requests with response parsing.

**Key Characteristics:**
- HTTP client: GET, POST, and generic fetch
- JSON request/response handling
- Context propagation for timeouts
- Capability-gated: `net:http` (capability enforcement deferred to future change)
- No external dependencies (uses net/http)

---

## 2. Motivation

### Problem
Services need to make HTTP calls from Lisp scripts:
- Call REST APIs
- Webhook notifications
- Fetch configuration from remote sources

### Solution
A simple HTTP client plugin:
- Simple API for common operations
- Context-aware (timeouts, cancellation)
- Response parsing helpers
- Independent of IO plugin (can work with in-memory data)

### Success Metrics
- HTTP calls respect context cancellation
- JSON responses parsed automatically
- Connection reuse (HTTP keep-alive)
- Timeout handling without leaks

---

## 3. Scope

### In Scope

**HTTP Operations**
- `http/get` - GET request
- `http/post` - POST request with body
- `http/fetch` - Generic request with full control

**Request Options**
- Headers as map
- Query parameters
- Request body (string or map for JSON)
- Timeout configuration

**Response Handling**
- Status code
- Headers
- Body as string
- JSON automatic parsing (when Content-Type is JSON)

### Out of Scope

- HTTP server → Future plugin
- WebSocket → Future plugin
- File upload/download with progress → Use io plugin
- Authentication helpers → Host application responsibility
- Retry logic → Host application responsibility

---

## 4. Functional Requirements

### HTTP Operations

| ID | Requirement | Priority |
|----|-------------|----------|
| N6b.1 | get makes GET request, returns response map | P0 |
| N6b.2 | post makes POST request with body | P0 |
| N6b.3 | fetch provides full control over request | P0 |
| N6b.4 | All operations respect context | P0 |
| N6b.5 | Timeout configurable per request | P0 |
| N6b.6 | Connection pooling/reuse | P1 |

### Request Building

| ID | Requirement | Priority |
|----|-------------|----------|
| N6b.7 | Headers specified as map | P0 |
| N6b.8 | Query parameters as map | P0 |
| N6b.9 | Body as string or map (auto-JSON) | P0 |
| N6b.10 | Content-Type auto-set for JSON body | P0 |

### Response Handling

| ID | Requirement | Priority |
|----|-------------|----------|
| N6b.11 | Response contains :status, :headers, :body | P0 |
| N6b.12 | JSON responses parsed to Lisp data | P0 |
| N6b.13 | Non-JSON responses returned as string | P0 |
| N6b.14 | Error status codes (4xx, 5xx) don't throw | P0 |
| N6b.15 | Network errors throw LispicoError | P0 |

---

## 5. Design Philosophy

### Simplicity

Minimal API surface. Common cases are easy:

```lisp
; Simple GET
(http/get "https://api.example.com/data")

; GET with query params
(http/get "https://api.example.com/search" 
          {:query {:q "lisp" :limit 10}})

; POST JSON
(http/post "https://api.example.com/users"
           {:body {:name "Alice" :email "alice@example.com"}})
```

### Context-First

All operations accept context for cancellation:

```go
func (p *Plugin) Get(ctx context.Context, url string, opts map[string]Value) (Value, error)
```

### Error Handling

Network errors throw, HTTP errors don't:

```lisp
; Network error (DNS failure, connection refused)
(http/get "https://invalid-domain-12345.com")
; => throws NetworkError

; HTTP 404
(http/get "https://api.example.com/notfound")
; => {:status 404 :body "Not Found"} (no throw)
```

---

## 6. Lisp API Reference

### http/get

```lisp
(http/get url opts?) → map

; Simple
(http/get "https://api.example.com/users")
; => {:status 200
;     :headers {"Content-Type" "application/json"}
;     :body [{:id 1 :name "Alice"} ...]}

; With options
(http/get "https://api.example.com/search"
  {:query {:q "lisp" :limit 10}
   :headers {"Authorization" "Bearer token123"}
   :timeout 5000})  ; milliseconds
```

### http/post

```lisp
(http/post url opts?) → map

; JSON body (auto-encoded)
(http/post "https://api.example.com/users"
  {:body {:name "Alice" :email "alice@example.com"}})

; String body
(http/post "https://api.example.com/webhook"
  {:body "raw payload"
   :headers {"Content-Type" "text/plain"}})
```

### http/fetch

```lisp
(http/fetch url opts?) → map

; Full control
(http/fetch "https://api.example.com/resource"
  {:method "PUT"
   :body {:field "value"}
   :headers {"X-Custom-Header" "value"}
   :query {:format "json"}
   :timeout 10000})
```

---

## 7. Response Format

```lisp
{:status 200
 :headers {"Content-Type" "application/json"
           "X-Request-Id" "abc123"}
 :body parsed-or-string}
```

### JSON Parsing

When `Content-Type` is `application/json`, body is parsed:

```lisp
; JSON: {"users": [{"id": 1, "name": "Alice"}]}
(http/get "https://api.example.com/users")
; => {:status 200
;     :headers {...}
;     :body {:users [{:id 1 :name "Alice"}]}}
```

**JSON key conversion**: When parsing JSON responses, string keys from `encoding/json` are converted to Lisp keywords (`:field-name`). Array values become Lisp vectors. Object values become Lisp HashMaps. This conversion is handled by `core.FromGoValue()`.

### String Body

When not JSON, body is string:

```lisp
; Plain text: "Hello, World!"
(http/get "https://example.com/hello")
; => {:status 200
;     :headers {...}
     :body "Hello, World!"}
```

---

## 8. Implementation Notes

### HTTP Client

Use `net/http` with sensible defaults:

```go
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
```

### Request Building

```go
func buildRequest(ctx context.Context, url string, opts map[string]Value) (*http.Request, error) {
    method := "GET"
    if m, ok := opts["method"]; ok {
        method = toString(m)
    }
    
    var body io.Reader
    if b, ok := opts["body"]; ok {
        if isMap(b) {
            // JSON encode
            jsonData, _ := json.Marshal(toGoMap(b))
            body = bytes.NewReader(jsonData)
        } else {
            body = strings.NewReader(toString(b))
        }
    }
    
    req, err := http.NewRequestWithContext(ctx, method, url, body)
    if err != nil {
        return nil, fmt.Errorf("build request %s %s: %w", method, url, err)
    }
    
    // Set headers
    if h, ok := opts["headers"]; ok {
        for k, v := range toMap(h) {
            req.Header.Set(k, toString(v))
        }
    }
    
    // Auto-set Content-Type for JSON body
    if _, ok := opts["body"]; ok && isMap(opts["body"]) {
        req.Header.Set("Content-Type", "application/json")
    }
    
    // Add query params
    if q, ok := opts["query"]; ok {
        query := req.URL.Query()
        for k, v := range toMap(q) {
            query.Set(k, toString(v))
        }
        req.URL.RawQuery = query.Encode()
    }
    
    return req, nil
}
```

### Timeout Handling

Timeout from options overrides default:

```go
if timeoutMs, ok := opts["timeout"]; ok {
    timeout := time.Duration(toInt(timeoutMs)) * time.Millisecond
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
}
```

---

## 9. Error Handling

### Network Errors

```
NetworkError: connection refused
  → Connection to server failed

NetworkError: timeout
  → Request exceeded timeout

NetworkError: DNS resolution failed
  → Cannot resolve hostname
```

### HTTP Status

HTTP errors (4xx, 5xx) are not thrown:

```lisp
(http/get "https://api.example.com/notfound")
; => {:status 404 :body "Not Found" :headers {...}}

; Caller decides how to handle
(let [resp (http/get url)]
  (if (>= (:status resp) 200)
    (process (:body resp))
    (log/error "Request failed:" (:status resp))))
```

---

## 10. Performance Requirements

| Metric | Target | Notes |
|--------|--------|-------|
| Request overhead | < 1ms | Excluding network |
| JSON parse (1KB) | < 1ms | Standard encoding/json |
| Connection reuse | 100% | HTTP keep-alive |
| Concurrent requests | 100+ | Limited by file descriptors |

---

## 11. Dependencies

### External Dependencies

- `net/http` - HTTP client (stdlib)
- `encoding/json` - JSON encoding (stdlib)
- `context` - Cancellation (stdlib)
- `time` - Timeouts (stdlib)

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required
- **Change 3** (runtime-api): Required (context)

### Relationship with IO Plugin

Independent design:
- Net plugin doesn't use IO plugin for file operations
- File downloads: use `http/get` + manual `fs/write-file` if needed
- This maintains ISP and allows minimal deployments

---

## 12. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Timeout leaks | Low | High | Proper context usage, defer cancel |
| Connection exhaustion | Medium | Medium | Connection pooling, limits |
| JSON parsing of large responses | Low | Low | Document limits, streaming for large |
| Redirect loops | Low | Medium | Follow redirects with limit |

---

## 13. Acceptance Criteria

- [ ] http/get working with options
- [ ] http/post with JSON body
- [ ] http/fetch with full control
- [ ] Context cancellation respected
- [ ] JSON responses parsed
- [ ] Connection reuse verified
- [ ] Timeout handling tested
- [ ] Documentation with examples
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with HTTP client configuration, request building, and response parsing.
