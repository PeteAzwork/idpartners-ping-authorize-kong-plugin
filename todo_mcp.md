# MCP Support — Implementation Task List

**Source:** `design-interview-8-feb-mcp-support.md`
**Target:** `idpartners-ping-authorize/` (Go plugin, v2.0.0 → v2.1.0)

---

## Phase 1: Foundation — Configuration & Types

### 1.1 Add MCP configuration fields to `Config` struct
**File:** `config.go` (lines 11–53)
**Interview refs:** Q8.1, Q8.2, Q3.3, Q4.3, Q5.2, Q9.1

Add the following fields to `Config`:

```go
// MCP support
EnableMCP            bool     `json:"enable_mcp"`              // Master toggle (Q8.1, Q8.2)
MCPJsonrpcErrors     bool     `json:"mcp_jsonrpc_errors"`      // Return JSON-RPC 2.0 error format (Q3.3, Q7.3)
MaxSidebandBodyBytes int      `json:"max_sideband_body_bytes"` // Max payload size, 0 = unlimited (Q4.3)
ExtractHeaders       []string `json:"extract_headers"`         // Headers to extract as top-level fields (Q5.2)
MCPRetryMethods      []string `json:"mcp_retry_methods"`       // MCP methods safe to retry (Q9.1)
```

**Acceptance criteria:**
- [ ] Fields added with JSON tags and documentation comments
- [ ] `applyDefaults()` sets sensible defaults: `MCPRetryMethods` defaults to `["tools/list", "resources/list", "prompts/list", "initialize"]`
- [ ] `MaxSidebandBodyBytes` defaults to `0` (unlimited)
- [ ] `MCPJsonrpcErrors` defaults to `false`
- [ ] `Validate()` updated: `MaxSidebandBodyBytes >= 0`, `MCPRetryMethods` entries are valid MCP method names

---

### 1.2 Add MCP types and sideband payload extensions
**File:** `types.go` (lines 1–87)
**Interview refs:** Q7.1, Q7.2, Q2.1

Add MCP context struct and extend `SidebandAccessRequest`:

```go
// MCPContext holds extracted MCP fields for the sideband payload.
type MCPContext struct {
    Method        string          `json:"mcp_method"`                    // JSON-RPC method (e.g. "tools/call")
    ToolName      string          `json:"mcp_tool_name,omitempty"`       // tools/call: $.params.name
    ToolArguments json.RawMessage `json:"mcp_tool_arguments,omitempty"`  // tools/call: $.params.arguments
    ResourceURI   string          `json:"mcp_resource_uri,omitempty"`    // resources/read: $.params.uri
    JsonrpcID     json.RawMessage `json:"mcp_jsonrpc_id,omitempty"`      // $.id (string or int)
}

// JsonRPCRequest is the minimal structure for parsing JSON-RPC 2.0 requests.
type JsonRPCRequest struct {
    Jsonrpc string          `json:"jsonrpc"`
    Method  string          `json:"method"`
    ID      json.RawMessage `json:"id,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// JsonRPCError is the JSON-RPC 2.0 error response format.
type JsonRPCError struct {
    Jsonrpc string             `json:"jsonrpc"`
    ID      json.RawMessage    `json:"id"`
    Error   JsonRPCErrorDetail `json:"error"`
}

type JsonRPCErrorDetail struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

Extend existing structs (additive, backward-compatible per Q7.1):

```go
// In SidebandAccessRequest:
TrafficType       string            `json:"traffic_type,omitempty"`
MCP               *MCPContext       `json:"mcp,omitempty"`
ExtractedHeaders  map[string]string `json:"extracted_headers,omitempty"`

// In SidebandResponsePayload:
TrafficType       string            `json:"traffic_type,omitempty"`
MCP               *MCPContext       `json:"mcp,omitempty"`
```

**Acceptance criteria:**
- [ ] Non-MCP requests produce identical payloads to current behavior (no `traffic_type`, `mcp`, or `extracted_headers` fields when `enable_mcp` is false)
- [ ] `traffic_type` is `"mcp"` when MCP detected, omitted otherwise (Q3.2)
- [ ] `mcp_jsonrpc_id` preserves original type (string or integer) using `json.RawMessage`
- [ ] All new types have doc comments

---

### 1.3 Create `mcp.go` — MCP detection and JSON-RPC parsing module
**New file:** `mcp.go`
**Interview refs:** Q2.1, Q2.2, Q2.3, Q2.4

Create a new module with the following functions:

```go
// ParseMCPRequest parses a JSON-RPC 2.0 request body and extracts MCP context.
// Returns nil if the body is not a valid JSON-RPC 2.0 request.
func ParseMCPRequest(body []byte) *MCPContext

// IsMCPMethod returns true if the method is a recognized MCP method.
func IsMCPMethod(method string) bool
```

**Supported MCP methods (Q2.2):**
- `tools/call` → extract `mcp_tool_name` (from `$.params.name`) and `mcp_tool_arguments` (from `$.params.arguments`)
- `tools/list` → method only
- `resources/read` → extract `mcp_resource_uri` (from `$.params.uri`)
- `resources/list` → method only
- `prompts/get` → extract prompt name from `$.params.name`
- `prompts/list` → method only
- `initialize` → method only

**Parsing rules:**
- Body must be valid JSON with `"jsonrpc": "2.0"` field
- `$.method` must be a recognized MCP method string
- `$.id` is preserved as raw JSON (can be string, int, or null)
- If `$.params` is present, extract method-specific fields
- Return `nil` (not error) for non-MCP bodies — fail silently for regular API traffic

**Acceptance criteria:**
- [ ] Handles all 7 MCP method types
- [ ] Returns `nil` for non-JSON, non-JSON-RPC, and unrecognized methods
- [ ] Does not allocate or parse if `enable_mcp` is false (caller responsibility)
- [ ] Tool arguments preserved as `json.RawMessage` (not re-serialized)

---

### 1.4 Create `mcp_test.go` — Unit tests for MCP parsing
**New file:** `mcp_test.go`
**Interview ref:** Q10.3

**Test cases:**
- [ ] `tools/call` with name and arguments → all fields extracted
- [ ] `tools/call` with no arguments → `mcp_tool_arguments` is nil
- [ ] `tools/list` → method only, no tool-specific fields
- [ ] `resources/read` with URI → `mcp_resource_uri` extracted
- [ ] `resources/list` → method only
- [ ] `prompts/get` with name → prompt name extracted
- [ ] `prompts/list` → method only
- [ ] `initialize` → method and id extracted
- [ ] Non-JSON body → returns nil
- [ ] JSON but not JSON-RPC (missing `jsonrpc` field) → returns nil
- [ ] JSON-RPC but unrecognized method (e.g. `"foo/bar"`) → returns nil
- [ ] JSON-RPC notification (no `id`) → `mcp_jsonrpc_id` is nil
- [ ] `id` as integer → preserved
- [ ] `id` as string → preserved
- [ ] Empty body → returns nil
- [ ] Malformed `params` → method extracted, params fields empty

---

## Phase 2: Access Phase — MCP-Aware Request Processing

### 2.1 Integrate MCP parsing into access phase payload composition
**File:** `access.go` — `composeAccessPayload()` (line 78)
**Interview refs:** Q2.1, Q3.2, Q7.1

After building the base `SidebandAccessRequest`, add MCP enrichment:

```go
// After line 128 (req construction), before return:
if conf.EnableMCP {
    mcpCtx := ParseMCPRequest(rawBody)
    if mcpCtx != nil {
        req.TrafficType = "mcp"
        req.MCP = mcpCtx
    }
}
```

**Also add configurable header extraction (Q5.2):**

```go
if conf.EnableMCP && len(conf.ExtractHeaders) > 0 {
    extracted := make(map[string]string)
    for _, headerName := range conf.ExtractHeaders {
        lowerName := strings.ToLower(headerName)
        if vals, ok := headers[lowerName]; ok && len(vals) > 0 {
            extracted[lowerName] = vals[0]
        }
    }
    if len(extracted) > 0 {
        req.ExtractedHeaders = extracted
    }
}
```

**Acceptance criteria:**
- [ ] When `enable_mcp` is false, no MCP parsing occurs (zero overhead for non-MCP routes)
- [ ] When `enable_mcp` is true but body is not MCP, payload is unchanged (no `traffic_type` or `mcp` fields)
- [ ] When MCP detected, `traffic_type: "mcp"` and `mcp` object are present
- [ ] Header extraction only runs when `extract_headers` is non-empty
- [ ] Existing tests continue to pass unmodified

---

### 2.2 Add payload size enforcement
**File:** `access.go` — `composeAccessPayload()` (after payload construction)
**Interview ref:** Q4.3

After composing the payload, check serialized size:

```go
if conf.MaxSidebandBodyBytes > 0 {
    payloadBytes, _ := json.Marshal(req)
    if len(payloadBytes) > conf.MaxSidebandBodyBytes {
        // Truncate body field to fit within limit
        // OR reject with 413 — configurable behavior TBD
    }
}
```

**Acceptance criteria:**
- [ ] When `max_sideband_body_bytes` is 0, no size check
- [ ] When payload exceeds limit, request body is truncated and a warning is logged
- [ ] Truncation removes the `body` field content first (preserving MCP context and headers)
- [ ] Unit test for payload size enforcement

---

### 2.3 MCP-aware JSON-RPC error responses for denied requests
**File:** `access.go` — `handleAccessResponse()` (line 231) and `handleCircuitBreakerError()` (line 366)
**Interview refs:** Q3.3, Q7.3, Q9.2

When `conf.MCPJsonrpcErrors` is true and the request is MCP traffic, format deny responses as JSON-RPC 2.0 errors:

```go
func formatMCPDenyResponse(statusCode int, message string, jsonrpcID json.RawMessage) []byte {
    errCode := httpStatusToJsonRPCError(statusCode)
    resp := JsonRPCError{
        Jsonrpc: "2.0",
        ID:      jsonrpcID,
        Error: JsonRPCErrorDetail{
            Code:    errCode,
            Message: message,
        },
    }
    body, _ := json.Marshal(resp)
    return body
}
```

**HTTP → JSON-RPC error code mapping:**
| HTTP Status | JSON-RPC Code | Meaning |
|-------------|---------------|---------|
| 400 | -32600 | Invalid Request |
| 401/403 | -32600 | Invalid Request (unauthorized) |
| 404 | -32601 | Method not found |
| 429 | -32000 | Server error (rate limited) |
| 500 | -32603 | Internal error |
| 502/503 | -32000 | Server error (unavailable) |

**Must update:**
- `handleAccessResponse()` — wrap PingAuthorize denial in JSON-RPC error format
- `handleCircuitBreakerError()` — wrap circuit breaker 429/502 in JSON-RPC error format
- `handleCircuitBreakerErrorResponse()` — same for response phase

**Acceptance criteria:**
- [ ] JSON-RPC error preserves original `$.id` from the request
- [ ] `Content-Type` header set to `application/json` for JSON-RPC errors
- [ ] When `mcp_jsonrpc_errors` is false, behavior is unchanged
- [ ] Need access to `mcp_jsonrpc_id` — store in Kong per-request context during access phase
- [ ] Unit tests for each HTTP→JSON-RPC error mapping

---

### 2.4 MCP-aware request body rewriting (tool argument modification)
**File:** `access.go` — `updateRequest()` (line 254)
**Interview ref:** Q6.2

When PingAuthorize modifies the request body in the `SidebandAccessResponse`, and the request is MCP traffic, the plugin must:

1. Parse the modified body as JSON-RPC
2. Rebuild the original JSON-RPC envelope with the modified params
3. Set the rewritten body on the upstream request

This is needed because PingAuthorize may return a modified `body` field where tool arguments have been redacted, constrained, or augmented.

**Acceptance criteria:**
- [ ] If PingAuthorize returns a modified body for an MCP request, the body is still valid JSON-RPC 2.0
- [ ] If body modification fails to parse, log warning and use the raw modified body as-is
- [ ] Non-MCP requests continue to use the current raw body replacement logic
- [ ] Unit test: PingAuthorize modifies tool arguments → upstream receives valid JSON-RPC with modified args

---

## Phase 3: Response Phase — SSE Streams and MCP Response Processing

### 3.1 SSE stream parsing for response phase
**New file:** `sse.go`
**Interview refs:** Q4.1, Q4.2

Create an SSE parser that extracts the final JSON-RPC message from an SSE stream:

```go
// ParseSSEFinalMessage extracts the last JSON-RPC response from an SSE stream body.
// SSE format: lines prefixed with "data: ", separated by blank lines.
// Returns the final JSON-RPC message body, or the original body if not SSE.
func ParseSSEFinalMessage(body []byte, contentType string) []byte
```

**Parsing rules:**
- Only parse if `Content-Type` is `text/event-stream`
- SSE events are `data: <json>\n\n` separated
- Extract the last `data:` payload that contains a valid JSON-RPC response
- If the body is `application/json`, return it as-is (single response, no SSE)

**Acceptance criteria:**
- [ ] Handles `text/event-stream` content type
- [ ] Extracts last JSON-RPC response from multi-event stream
- [ ] Returns original body unchanged for `application/json`
- [ ] Handles edge cases: empty stream, single event, no valid JSON-RPC events
- [ ] Unit tests in `sse_test.go`

---

### 3.2 Create `sse_test.go` — Unit tests for SSE parsing
**New file:** `sse_test.go`
**Interview ref:** Q10.3

**Test cases:**
- [ ] Single `data:` event with JSON-RPC response → extracted
- [ ] Multiple events, last is JSON-RPC response → last extracted
- [ ] Multiple events with notifications followed by final response → response extracted
- [ ] `application/json` body → returned as-is
- [ ] Empty body → returned as-is
- [ ] Malformed SSE (no `data:` prefix) → returned as-is
- [ ] `data:` with non-JSON content → skipped, prior valid event used
- [ ] Large SSE stream with many events → only final extracted (memory efficient)

---

### 3.3 Integrate SSE parsing into response phase
**File:** `response.go` — `composeResponsePayload()` (line 104)
**Interview refs:** Q4.1, Q4.2

After reading the upstream response body, check if it's an SSE stream and extract the final JSON-RPC message:

```go
responseBody := string(responseBodyBytes)

if conf.EnableMCP {
    // Check Content-Type from upstream response headers
    contentType := getContentType(responseHeaders)
    if contentType == "text/event-stream" {
        finalMsg := ParseSSEFinalMessage(responseBodyBytes, contentType)
        responseBody = string(finalMsg)
    }
}
```

**Acceptance criteria:**
- [ ] SSE parsing only occurs when `enable_mcp` is true
- [ ] Non-SSE responses are unaffected
- [ ] The MCP context from the original request is available in the response payload
- [ ] Unit test with mock SSE upstream response

---

### 3.4 Tools/list response filtering
**File:** `response.go` — `handleResponseResult()` (line 164)
**Interview ref:** Q6.3

When PingAuthorize returns a modified `tools/list` response body, the plugin should apply it. PingAuthorize can remove tools from the response to hide unauthorized tools.

This works via the existing body modification mechanism — PingAuthorize returns a `body` field in the `SidebandResponseResult` with filtered tools. No new plugin logic is strictly required, but:

**Acceptance criteria:**
- [ ] Verify that the existing `handleResponseResult()` correctly passes through modified bodies from PingAuthorize
- [ ] Add integration test: PingAuthorize removes a tool from `tools/list` response → client does not see it
- [ ] Add integration test: PingAuthorize removes a resource from `resources/list` response → client does not see it
- [ ] Confirm the body modification works for all list-type MCP responses

---

### 3.5 Add MCP context to response phase payload
**File:** `response.go` — `composeResponsePayload()` (line 104)
**Interview ref:** Q7.1

Add MCP context to the response payload so PingAuthorize knows this is MCP traffic:

```go
if conf.EnableMCP {
    // Re-parse the response body for MCP context (response may be JSON-RPC)
    mcpCtx := ParseMCPRequest([]byte(responseBody))
    if mcpCtx != nil {
        payload.TrafficType = "mcp"
        payload.MCP = mcpCtx
    } else if /* original request was MCP (check stored context) */ {
        payload.TrafficType = "mcp"
        // Carry forward the original MCP method context
    }
}
```

**Acceptance criteria:**
- [ ] Response payload includes `traffic_type: "mcp"` when the original request was MCP
- [ ] MCP context from the response body (if JSON-RPC) is included
- [ ] If response is not JSON-RPC (e.g., after SSE extraction), still mark as MCP traffic
- [ ] Store MCP context in per-request Kong context during access phase for response phase use

---

## Phase 4: Retry & Circuit Breaker MCP Awareness

### 4.1 MCP-aware retry configuration
**File:** `network.go` — `Execute()` (line 52)
**Interview ref:** Q9.1

Add MCP method awareness to the retry loop. When the request is for an MCP method not in the `mcp_retry_methods` list, do not retry:

**Approach:** Pass MCP method (or nil) into `Execute()` or add a `retryable` flag.

```go
// Option A: Add mcpMethod parameter
func (c *SidebandHTTPClient) Execute(ctx context.Context, requestURL string, body []byte,
    parsedURL *ParsedURL, mcpMethod string) (int, http.Header, []byte, error)

// In retry loop:
if mcpMethod != "" && !isMCPMethodRetryable(mcpMethod, c.config.MCPRetryMethods) {
    maxAttempts = 1 // no retry for non-idempotent MCP methods
}
```

**Default retryable methods:** `tools/list`, `resources/list`, `prompts/list`, `initialize`
**Default non-retryable:** `tools/call`, `resources/read`, `prompts/get`

**Acceptance criteria:**
- [ ] `tools/call` requests are never retried (unless operator overrides via config)
- [ ] `tools/list` requests follow normal retry config
- [ ] Non-MCP requests are unaffected (existing behavior)
- [ ] `SidebandProvider.EvaluateRequest()` passes MCP method through to HTTP client
- [ ] Update `PolicyProvider` interface if needed (or pass method via context)
- [ ] Unit test: `tools/call` with 500 → no retry; `tools/list` with 500 → retries

---

### 4.2 Circuit breaker JSON-RPC error format
**File:** `access.go` — `handleCircuitBreakerError()` (line 366)
**File:** `response.go` — `handleCircuitBreakerErrorResponse()` (line 218)
**Interview ref:** Q9.2

When `mcp_jsonrpc_errors` is true and the request is MCP traffic, circuit breaker errors should be JSON-RPC 2.0:

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Service temporarily unavailable. Retry after 30 seconds."}}
```

**Acceptance criteria:**
- [ ] Circuit breaker 429 → JSON-RPC error with code `-32000` and retry message
- [ ] Circuit breaker 502 → JSON-RPC error with code `-32000` and unavailable message
- [ ] `id` field matches the original request's `$.id` (requires stored context)
- [ ] When `mcp_jsonrpc_errors` is false, existing HTTP error behavior unchanged
- [ ] Unit tests for both 429 and 502 circuit breaker JSON-RPC errors

---

## Phase 5: Observability

### 5.1 Add MCP-specific OTel metrics
**File:** `observability.go`
**Interview ref:** Implicit from Q2, Q6

Add counters/histograms for MCP traffic:

```go
// New OTel instruments:
MCPRequestsTotal    Int64Counter    // by mcp_method
MCPDeniedTotal      Int64Counter    // by mcp_method, reason
MCPToolCallsTotal   Int64Counter    // by tool_name
```

**Acceptance criteria:**
- [ ] `mcp_requests_total{method="tools/call"}` incremented for each MCP request
- [ ] `mcp_denied_total{method="tools/call", reason="policy"}` incremented on denial
- [ ] `mcp_tool_calls_total{tool="delete_user"}` incremented per tool name
- [ ] Metrics only emitted when `enable_mcp` and `enable_otel` are both true
- [ ] Existing OTel metrics unchanged

---

### 5.2 MCP payload debug logging
**File:** `observability.go` — `DebugLogPayload()`
**Interview ref:** Q8.3

Enhance debug logging to include MCP context when present:

- Log `mcp_method`, `mcp_tool_name`, `traffic_type` at INFO level for MCP requests
- Existing `redact_headers` config applies — operators can add MCP headers as needed (Q8.3)
- `mcp_tool_arguments` should respect `debug_body_max_bytes` truncation

**Acceptance criteria:**
- [ ] MCP fields visible in debug logs when `enable_debug_logging` is true
- [ ] Tool arguments truncated per `debug_body_max_bytes`
- [ ] No new default redaction entries needed (per Q8.3)

---

## Phase 6: Integration Tests

### 6.1 MCP access phase integration tests
**File:** `integration_test.go`
**Interview ref:** Q10.3

Add integration tests with mock PingAuthorize server:

- [ ] **MCP `tools/call` — allowed:** Client sends `tools/call`, PingAuthorize allows → upstream receives request with MCP context in sideband payload
- [ ] **MCP `tools/call` — denied:** PingAuthorize denies → client receives 403 (HTTP) or JSON-RPC error (when `mcp_jsonrpc_errors` enabled)
- [ ] **MCP `tools/call` — argument modification:** PingAuthorize modifies tool arguments → upstream receives modified body
- [ ] **MCP `tools/list` — allowed:** Standard flow with method extraction
- [ ] **MCP `resources/read` — allowed:** Resource URI extracted correctly
- [ ] **MCP `initialize` — allowed:** Method and protocol version in payload
- [ ] **Non-MCP request with `enable_mcp: true`:** Regular HTTP POST → no MCP fields in payload
- [ ] **MCP request with `enable_mcp: false`:** JSON-RPC body not parsed, raw HTTP forwarded

---

### 6.2 MCP response phase integration tests
**File:** `integration_test.go`
**Interview ref:** Q10.3

- [ ] **SSE stream response:** Upstream returns `text/event-stream` → plugin extracts final JSON-RPC message for sideband evaluation
- [ ] **`tools/list` filtering:** PingAuthorize removes tools from response → client sees filtered list
- [ ] **JSON-RPC error on denial:** Response phase denial with `mcp_jsonrpc_errors: true` → client receives JSON-RPC error

---

### 6.3 MCP retry and circuit breaker integration tests
**File:** `integration_test.go`
**Interview ref:** Q9.1, Q9.2

- [ ] **`tools/call` no retry:** PingAuthorize returns 500 for `tools/call` → single attempt, no retry
- [ ] **`tools/list` with retry:** PingAuthorize returns 500 then 200 for `tools/list` → retried successfully
- [ ] **Circuit breaker JSON-RPC error:** Circuit breaker open + `mcp_jsonrpc_errors: true` → JSON-RPC error response
- [ ] **Payload size limit:** Large `tools/call` body exceeds `max_sideband_body_bytes` → body truncated or rejected

---

### 6.4 Mock MCP JSON-RPC payloads test fixtures
**New file:** `testdata/mcp_fixtures.go` (or `mcp_testdata_test.go`)
**Interview ref:** Q10.3

Create reusable test fixtures for all MCP method types:

- [ ] `tools/call` — with tool name "get_weather" and arguments `{"city": "London"}`
- [ ] `tools/call` — with no arguments
- [ ] `tools/list` — empty params
- [ ] `resources/read` — with URI "file:///data/config.json"
- [ ] `resources/list` — empty params
- [ ] `prompts/get` — with name "summarize"
- [ ] `prompts/list` — empty params
- [ ] `initialize` — with protocol version and capabilities
- [ ] `tools/list` response — with 3 tools (for filtering tests)
- [ ] SSE stream — with 3 events, last is JSON-RPC response
- [ ] JSON-RPC error response — `-32600 Invalid Request`

---

## Phase 7: Documentation & Version Bump

### 7.1 Update README.md
**File:** `idpartners-ping-authorize/README.md`

- [ ] Add MCP configuration section with all new config fields
- [ ] Document `enable_mcp` toggle behavior
- [ ] Document `mcp_jsonrpc_errors` behavior and when to enable
- [ ] Document `extract_headers` usage with OAuth plugins
- [ ] Document `mcp_retry_methods` configuration
- [ ] Document `max_sideband_body_bytes` configuration
- [ ] Add example Kong configuration for MCP route

---

### 7.2 Update DESIGN.md
**File:** `DESIGN.md`

- [ ] Add MCP sideband payload schema (request and response)
- [ ] Document SSE stream handling behavior
- [ ] Document JSON-RPC error response format
- [ ] Document tool argument modification flow
- [ ] Document tools/list response filtering flow

---

### 7.3 Version bump
**File:** `main.go`

- [ ] Bump `Version` from `"2.0.0"` to `"2.1.0"`
- [ ] Update `User-Agent` string accordingly (automatic, uses `Version` constant)

---

## Dependency Summary

```
Phase 1 (Foundation) ← no dependencies
  ├── 1.1 Config fields
  ├── 1.2 Types
  ├── 1.3 mcp.go (depends on 1.2)
  └── 1.4 mcp_test.go (depends on 1.3)

Phase 2 (Access) ← depends on Phase 1
  ├── 2.1 Access integration (depends on 1.1, 1.2, 1.3)
  ├── 2.2 Payload size (depends on 1.1)
  ├── 2.3 JSON-RPC errors (depends on 1.2)
  └── 2.4 Body rewriting (depends on 1.3)

Phase 3 (Response) ← depends on Phase 1, partially Phase 2
  ├── 3.1 SSE parser (depends on 1.2)
  ├── 3.2 SSE tests (depends on 3.1)
  ├── 3.3 Response SSE integration (depends on 3.1, 1.1)
  ├── 3.4 tools/list filtering (depends on 1.1)
  └── 3.5 Response MCP context (depends on 1.2, 1.3, 2.1)

Phase 4 (Retry/CB) ← depends on Phase 1, Phase 2
  ├── 4.1 MCP-aware retry (depends on 1.1, 1.3)
  └── 4.2 CB JSON-RPC errors (depends on 2.3)

Phase 5 (Observability) ← depends on Phase 1
  ├── 5.1 OTel metrics (depends on 1.2)
  └── 5.2 Debug logging (depends on 1.2)

Phase 6 (Integration Tests) ← depends on all above
  ├── 6.1–6.3 Integration tests (depends on Phases 1–4)
  └── 6.4 Test fixtures (no deps, can be built early)

Phase 7 (Docs) ← depends on all above
```

---

## Files Changed Summary

| File | Change Type | Tasks |
|------|------------|-------|
| `config.go` | Modified | 1.1 |
| `types.go` | Modified | 1.2 |
| `mcp.go` | **New** | 1.3 |
| `mcp_test.go` | **New** | 1.4 |
| `access.go` | Modified | 2.1, 2.2, 2.3, 2.4 |
| `sse.go` | **New** | 3.1 |
| `sse_test.go` | **New** | 3.2 |
| `response.go` | Modified | 3.3, 3.4, 3.5 |
| `network.go` | Modified | 4.1 |
| `provider.go` | Modified | 4.1 (interface may change) |
| `sideband_provider.go` | Modified | 4.1 |
| `observability.go` | Modified | 5.1, 5.2 |
| `integration_test.go` | Modified | 6.1, 6.2, 6.3 |
| `testdata/` or `*_test.go` | **New** | 6.4 |
| `main.go` | Modified | 7.3 |
| `README.md` | Modified | 7.1 |
| `DESIGN.md` | Modified | 7.2 |

**New files:** 4 (`mcp.go`, `mcp_test.go`, `sse.go`, `sse_test.go`) + test fixtures
**Modified files:** 10
**Estimated new code:** ~1200–1500 lines (including tests)
