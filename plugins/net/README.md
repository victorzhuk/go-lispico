# Net Plugin

HTTP client for go-lispico. Registers functions under the `http/` namespace.

## Available Functions

### http/get

```lisp
(http/get url ?opts)
```

Perform a GET request. `url` is a string; `opts` is an optional map.

### http/post

```lisp
(http/post url ?opts)
```

Perform a POST request. Same arguments as `http/get`.

### http/fetch

```lisp
(http/fetch url ?opts)
```

Perform a request with an explicit method taken from `:method` in `opts`
(default `GET`).

## Options

- `:method` (string/keyword) — HTTP method, `http/fetch` only (default `GET`)
- `:body` (string or map) — request body; a map is JSON-encoded and sets
  `Content-Type: application/json`
- `:query` (map) — query-string parameters
- `:headers` (map) — request headers
- `:timeout` (int) — per-request timeout in milliseconds

## Return value

A map with:

- `:status` (int) — HTTP status code
- `:headers` (map) — response headers (first value per key)
- `:body` — parsed JSON value when the response is `application/json`,
  otherwise the raw body string

```lisp
(http/get "https://example.com/api" {:query {"q" "term"} :timeout 5000})
; => {:status 200 :headers {...} :body {...}}
```

## Trust Model

`http/get`, `http/post`, and `http/fetch` accept any URL with no allow/deny list.
They will happily reach `localhost`, RFC 1918 ranges, and cloud metadata
endpoints such as `169.254.169.254` — this is server-side request forgery (SSRF)
surface. This is by design for an interpreter meant to run trusted automation
scripts.

Grant the `net` namespace only to scripts you trust. If untrusted Lisp can reach
these functions, the embedder is responsible for restricting outbound network
access (egress firewall, URL allowlisting at a proxy, or a custom
`*http.Client`/transport passed when constructing the plugin).
