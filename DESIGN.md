# PingAuthorize Sideband Kong Plugin — Go Implementation Design Document

> **Plugin name**: `idpartners-ping-authorize`
> **Replaces**: `ping-auth` (Lua)
> **Design decisions**: See [design-interview-7-feb.md](design-interview-7-feb.md)

## 1. Introduction

### 1.1 Purpose

This document provides a comprehensive specification for implementing the `idpartners-ping-authorize` Kong Gateway plugin in Go using the [Kong Go Plugin Development Kit (PDK)](https://github.com/Kong/go-pdk). The plugin integrates Kong with PingAuthorize via the Sideband API protocol, intercepting requests during the access and response phases to consult the PingAuthorize policy provider.

This is a clean re-implementation, not a port of the existing Lua `ping-auth` plugin. All known bugs in the Lua version are fixed, and behavioral differences from the Lua version are accepted.

### 1.2 Scope

This document covers:
- The PingAuthorize Sideband API protocol
- All request/response data structures and transformations
- Configuration schema and validation
- Lifecycle phases and execution flow
- Circuit breaker behavior
- Retry logic
- Error handling semantics (configurable fail-closed/fail-open)
- Header format conversions
- Client certificate handling (RSA, EC, Ed25519)
- Observability (structured logging, OpenTelemetry, Kong PDK logging)
- Debug log redaction
- Known limitations
- Testing strategy

### 1.3 Design Principles

- **PingAuthorize first, AuthZen later**: The initial implementation targets PingAuthorize Sideband API only. The architecture should use interfaces for the policy provider communication layer to enable a future AuthZen implementation without major refactoring.
- **Clean break**: New plugin name (`idpartners-ping-authorize`), idiomatic Go config field naming, full cutover from the Lua version.
- **Fix all Lua bugs**: Race conditions, nil dereferences, and inconsistent error codes are all resolved.

### 1.4 Reference Implementation

- **Source**: `ping-auth/` directory (Lua)
- **Lua Version**: 1.2.0
- **Go Version**: 2.0.0
- **Priority**: 999 (Kong plugin execution order)
- **License**: Apache 2.0 (Ping Identity Corp.)

### 1.5 Platform Requirements

- **Kong Gateway**: 3.x only (Kong 2.x is not supported)
- **Go PDK**: Standard external process model (`github.com/Kong/go-pdk`), communicating with Kong via MessagePack over sockets
- **Deployment**: Single standalone binary
- **Plugin scope**: Route, Service, or Global

---

## 2. High-Level Architecture

### 2.1 Request Flow

```
Client                 Kong (idpartners-ping-authorize)   PingAuthorize          Upstream API
  │                         │                              │                        │
  │── HTTP Request ────────>│                              │                        │
  │   (HTTP/1.1 or 2)       │                              │                        │
  │                    [ACCESS PHASE]                       │                        │
  │                         │── POST /sideband/request ───>│                        │
  │                         │   (always HTTP/1.1)           │                        │
  │                         │<── Policy Decision ──────────│                        │
  │                         │                              │                        │
  │                    [If DENIED]                          │                        │
  │<── Error Response ──────│                              │                        │
  │                         │                              │                        │
  │                    [If ALLOWED - apply modifications]   │                        │
  │                         │── Forwarded Request ─────────│───────────────────────>│
  │                         │<── Upstream Response ────────│────────────────────────│
  │                         │                              │                        │
  │                    [RESPONSE PHASE - if enabled]        │                        │
  │                         │── POST /sideband/response ──>│                        │
  │                         │   (always HTTP/1.1)           │                        │
  │                         │<── Final Decision ───────────│                        │
  │                         │                              │                        │
  │<── Final Response ──────│                              │                        │
```

### 2.2 Module Decomposition

| Go Module | Responsibility |
|---|---|
| `main.go` | Kong PDK entry point; `Access()` and `Response()` lifecycle hooks |
| `config.go` | Config struct definition, validation, defaults |
| `access.go` | Build sideband request payload; process policy decision; apply request modifications |
| `response.go` | Build sideband response payload; process final policy decision; apply response modifications |
| `network.go` | HTTP client with retry support, circuit breaker |
| `headers.go` | Header format conversion (Sideband array ↔ standard map) |
| `certificate.go` | Client certificate → JWK conversion (RSA, EC, Ed25519) |
| `types.go` | Shared data structures |
| `observability.go` | Structured logging, OpenTelemetry integration, log redaction |
| `provider.go` | Policy provider interface (enables future AuthZen support) |

### 2.3 Plugin Metadata

```go
const (
    PluginName = "idpartners-ping-authorize"
    Version    = "2.0.0"
    Priority   = 999
)
```

### 2.4 Policy Provider Interface

To support future AuthZen integration, the communication layer should be abstracted behind an interface:

```go
// PolicyProvider abstracts the sideband communication protocol.
// The initial implementation is PingAuthorize Sideband API.
// Future implementations may include AuthZen standard APIs.
type PolicyProvider interface {
    // EvaluateRequest sends the client request for policy evaluation (access phase).
    // Returns the policy decision including any modifications, state, or deny response.
    EvaluateRequest(ctx context.Context, req *AccessRequest) (*AccessResponse, error)

    // EvaluateResponse sends the upstream response for final evaluation (response phase).
    // Returns the final response to send to the client.
    EvaluateResponse(ctx context.Context, req *ResponseRequest) (*ResponseResult, error)
}
```

---

## 3. Configuration Schema

### 3.1 Fields

```go
type Config struct {
    // Required fields
    ServiceURL       string `json:"service_url"        kong:"required"`
    SharedSecret     string `json:"shared_secret"      kong:"required,referenceable"`
    SecretHeaderName string `json:"secret_header_name" kong:"required"`

    // Timeouts and connection
    ConnectionTimeoutMs  int  `json:"connection_timeout_ms"   kong:"default=10000"`
    ConnectionKeepaliveMs int `json:"connection_keepalive_ms" kong:"default=60000"`
    VerifyServiceCert    bool `json:"verify_service_cert"     kong:"default=true"`

    // Phase control
    SkipResponsePhase bool `json:"skip_response_phase" kong:"default=false"`

    // Error handling
    FailOpen                bool  `json:"fail_open"                 kong:"default=false"`
    PassthroughStatusCodes  []int `json:"passthrough_status_codes"  kong:"default=[413]"`

    // Retry
    MaxRetries     int `json:"max_retries"      kong:"default=0"`
    RetryBackoffMs int `json:"retry_backoff_ms" kong:"default=500"`

    // Circuit breaker
    CircuitBreakerEnabled bool `json:"circuit_breaker_enabled" kong:"default=true"`

    // Request modification
    StripAcceptEncoding bool `json:"strip_accept_encoding" kong:"default=true"`

    // Client certificate
    IncludeFullCertChain bool `json:"include_full_cert_chain" kong:"default=false"`

    // Debug and observability
    EnableDebugLogging bool     `json:"enable_debug_logging" kong:"default=false"`
    EnableOtel         bool     `json:"enable_otel"          kong:"default=false"`
    RedactHeaders      []string `json:"redact_headers"       kong:"default=[authorization,cookie]"`
    DebugBodyMaxBytes  int      `json:"debug_body_max_bytes" kong:"default=8192"`
}
```

### 3.2 Field Reference

#### Required Fields

| Field | Type | Description |
|---|---|---|
| `service_url` | string | PingAuthorize base URL. Must be valid HTTP/HTTPS. Must NOT include `/sideband/...` suffix. |
| `shared_secret` | string | Auth secret for PingAuthorize. Supports Kong Vault references (e.g., `{vault://env/my-secret}`). |
| `secret_header_name` | string | Header name under which the shared secret is sent to PingAuthorize. |

#### Timeouts and Connection

| Field | Type | Default | Constraints | Description |
|---|---|---|---|---|
| `connection_timeout_ms` | int | 10000 | > 0 | Combined connection/read/write timeout for sideband calls. |
| `connection_keepalive_ms` | int | 60000 | > 0 | Keep-alive duration for connection reuse to PingAuthorize. |
| `verify_service_cert` | bool | true | - | TLS certificate verification for PingAuthorize. Set `false` for test environments only. |

#### Phase Control

| Field | Type | Default | Description |
|---|---|---|---|
| `skip_response_phase` | bool | false | When `true`, the `/sideband/response` call is skipped entirely. The upstream response passes through to the client unmodified after the access phase. |

#### Error Handling

| Field | Type | Default | Description |
|---|---|---|---|
| `fail_open` | bool | false | When `true`, requests are allowed through if PingAuthorize is unreachable, returns an error, or times out. **Default is fail-closed** — errors block the request with 502. Fail-open requires explicit opt-in. |
| `passthrough_status_codes` | []int | [413] | List of HTTP status codes from PingAuthorize that are passed through directly to the client (with body) instead of being converted to a generic 502. |

#### Retry

| Field | Type | Default | Constraints | Description |
|---|---|---|---|---|
| `max_retries` | int | 0 | >= 0 | Number of retry attempts for failed sideband calls. `0` disables retries. |
| `retry_backoff_ms` | int | 500 | > 0 | Backoff duration between retry attempts. Applied as a fixed delay (not exponential). |

#### Circuit Breaker

| Field | Type | Default | Description |
|---|---|---|---|
| `circuit_breaker_enabled` | bool | true | When `false`, the circuit breaker is disabled and every request attempts to reach PingAuthorize. |

#### Request Modification

| Field | Type | Default | Description |
|---|---|---|---|
| `strip_accept_encoding` | bool | true | When `true`, removes the `Accept-Encoding` header from upstream requests to avoid gzip encoding issues with the sideband protocol. Set `false` to allow compressed upstream responses. |

#### Client Certificate

| Field | Type | Default | Description |
|---|---|---|---|
| `include_full_cert_chain` | bool | false | When `true`, includes the full certificate chain in the `x5c` JWK field. When `false`, includes only the leaf certificate. |

#### Debug and Observability

| Field | Type | Default | Description |
|---|---|---|---|
| `enable_debug_logging` | bool | false | Enables detailed DEBUG-level structured logging of sideband request/response payloads. |
| `enable_otel` | bool | false | Enables OpenTelemetry traces and metrics for sideband calls. Independent of Kong PDK logging. |
| `redact_headers` | []string | [authorization, cookie] | Header names whose values are replaced with `[REDACTED]` in debug log output. The `secret_header_name` is always redacted regardless of this list. |
| `debug_body_max_bytes` | int | 8192 | Maximum body size (in bytes) included in debug log output. Bodies exceeding this are truncated with a `[truncated]` marker. `0` disables truncation. |

### 3.3 Validation Rules

1. `service_url`: Parse and validate — scheme must be `http` or `https` (case-insensitive), host must be non-empty.
2. `shared_secret`: Non-empty. Must support Kong Vault `referenceable` resolution.
3. `secret_header_name`: Non-empty.
4. `connection_timeout_ms`: Must be > 0.
5. `connection_keepalive_ms`: Must be > 0.
6. `max_retries`: Must be >= 0.
7. `retry_backoff_ms`: Must be > 0.
8. `passthrough_status_codes`: Each code must be in the 400-599 range.
9. `debug_body_max_bytes`: Must be >= 0.

---

## 4. Sideband API Protocol

### 4.1 Header Format

The PingAuthorize Sideband API uses a specific header serialization format that differs from standard HTTP header maps. This is a critical detail for correct interoperability.

**Sideband format** (array of single-key objects):
```json
[
    {"content-type": "application/json"},
    {"connection": "keep-alive"},
    {"x-custom": "value1"},
    {"x-custom": "value2"}
]
```

**Standard map format** (what Kong and Go use natively):
```json
{
    "content-type": ["application/json"],
    "connection": ["keep-alive"],
    "x-custom": ["value1", "value2"]
}
```

Two conversion functions are required:

#### 4.1.1 `FormatHeaders` (Map → Sideband Array)

Converts a standard header map to the Sideband array-of-objects format.

**Input**: `map[string][]string` (or `map[string]interface{}` where values may be strings or string arrays)

**Output**: `[]map[string]string`

**Rules**:
- All header names are lowercased.
- Multi-value headers produce multiple entries in the array (one per value).
- Nested/multidimensional values are rejected with HTTP 400.

**Example**:
```
Input:  {"X-Custom": ["val1", "val2"], "Content-Type": ["application/json"]}
Output: [{"x-custom": "val1"}, {"x-custom": "val2"}, {"content-type": "application/json"}]
```

#### 4.1.2 `FlattenHeaders` (Sideband Array → Map)

Converts the Sideband array-of-objects format back to a standard header map.

**Input**: `[]map[string]string`

**Output**: `map[string][]string`

**Rules**:
- All header names are lowercased.
- Duplicate header names have their values collected into a single array, preserving order.

**Example**:
```
Input:  [{"Content-Type": "application/json"}, {"X-Custom": "val1"}, {"X-Custom": "val2"}]
Output: {"content-type": ["application/json"], "x-custom": ["val1", "val2"]}
```

### 4.2 Sideband Request Endpoint

**URL**: `POST {service_url}/sideband/request`

Called during the **access phase** to ask PingAuthorize whether to allow, deny, or modify the incoming client request.

#### 4.2.1 Request Payload (Plugin → PingAuthorize)

```json
{
    "source_ip": "192.168.1.100",
    "source_port": "54321",
    "method": "GET",
    "url": "https://api.example.com:443/resource?key=value",
    "body": "<raw request body or empty string>",
    "headers": [
        {"host": "api.example.com"},
        {"content-type": "application/json"}
    ],
    "http_version": "1.1",
    "client_certificate": {
        "kty": "RSA",
        "n": "...",
        "e": "AQAB",
        "x5c": ["<base64-DER-encoded-certificate>"]
    }
}
```

| Field | Type | Always Present | Description |
|---|---|---|---|
| `source_ip` | string | yes | Client's remote IP address |
| `source_port` | string | yes | Client's remote port |
| `method` | string | yes | HTTP method (GET, POST, etc.) |
| `url` | string | yes | Full reconstructed URL: `{scheme}://{host}:{port}{path}[?{query}]` |
| `body` | string | yes | Raw request body (may be empty string) |
| `headers` | array | yes | Headers in Sideband format (see 4.1) |
| `http_version` | string | yes | HTTP version string, e.g. `"1.1"` or `"2"`. Note: sideband calls always use HTTP/1.1 regardless of client protocol. |
| `client_certificate` | object | no | JWK with `x5c` extension; only present when client provides a TLS certificate and Kong Enterprise is available |

**URL Construction Logic**:
```
url = kong.request.get_forwarded_scheme()
    + "://"
    + kong.request.get_forwarded_host()
    + ":"
    + kong.request.get_forwarded_port()
    + kong.request.get_forwarded_path()

query = kong.request.get_raw_query()  // decoded then re-encoded with max 100 args
if query != "" {
    url = url + "?" + query
}
```

#### 4.2.2 HTTP Request to PingAuthorize

```
POST {service_url}/sideband/request HTTP/1.1
Host: {parsed_host}:{parsed_port}
Connection: Keep-Alive
Content-Type: application/json
User-Agent: Kong/{plugin_version}
Content-Length: {body_length}
{secret_header_name}: {shared_secret}

{json_payload}
```

All sideband calls use HTTP/1.1 regardless of the client's protocol version.

#### 4.2.3 Response from PingAuthorize: ALLOWED (request may proceed)

When PingAuthorize **allows** the request, the response body does NOT contain a `response` field. Instead, it may contain modified request fields and an optional `state` field:

```json
{
    "source_ip": "192.168.1.100",
    "source_port": "54321",
    "method": "GET",
    "url": "https://api.example.com:443/resource?key=value",
    "body": "<possibly modified body>",
    "headers": [
        {"host": "api.example.com"},
        {"content-type": "application/json"},
        {"x-injected-header": "added-by-policy"}
    ],
    "state": { "<opaque JSON object, passed to /sideband/response>" }
}
```

The `state` field is always a JSON object from PingAuthorize. It is opaque to the plugin — stored as `json.RawMessage` and forwarded verbatim to the response phase sideband call.

#### 4.2.4 Response from PingAuthorize: DENIED

When PingAuthorize **denies** the request (or returns other non-allow decisions such as indeterminate or challenge), the response body contains a `response` field:

```json
{
    "response": {
        "response_code": "403",
        "response_status": "FORBIDDEN",
        "body": "<optional response body>",
        "headers": [
            {"content-type": "application/json"}
        ]
    }
}
```

The plugin immediately returns this response to the client without forwarding to the upstream. All non-allow decision types follow this same structure with different status codes and bodies.

### 4.3 Sideband Response Endpoint

**URL**: `POST {service_url}/sideband/response`

Called during the **response phase** (unless `skip_response_phase` is `true`) after the upstream API has responded, to allow PingAuthorize to inspect and modify the final response.

#### 4.3.1 Request Payload (Plugin → PingAuthorize)

```json
{
    "method": "GET",
    "url": "https://api.example.com:443/resource?key=value",
    "body": "<upstream response body>",
    "response_code": "200",
    "response_status": "OK",
    "headers": [
        {"content-type": "application/json"},
        {"x-upstream-header": "value"}
    ],
    "http_version": "1.1",
    "state": { "<opaque state from access phase>" }
}
```

| Field | Type | Always Present | Description |
|---|---|---|---|
| `method` | string | yes | HTTP method of the original request |
| `url` | string | yes | Full reconstructed URL (same logic as access phase) |
| `body` | string | yes | Raw upstream response body |
| `response_code` | string | yes | Upstream HTTP status code, as a string |
| `response_status` | string | yes | Human-readable status text (see 4.3.2) |
| `headers` | array | yes | Upstream **response** headers in Sideband format |
| `http_version` | string | yes | HTTP version string |
| `state` | object | conditional | If access phase returned `state`, include it here (as `json.RawMessage`) |
| `request` | object | conditional | If access phase did NOT return `state`, include the original request payload |

**Important**: The `state` and `request` fields are mutually exclusive. If `state` was returned from `/sideband/request`, send `state`. Otherwise, send the original request data under `request`.

#### 4.3.2 Status Code to Status String Mapping

```go
var statusStrings = map[int]string{
    200: "OK",
    400: "BAD REQUEST",
    401: "UNAUTHORIZED",
    404: "NOT FOUND",
    413: "PAYLOAD TOO LARGE",
    429: "TOO MANY REQUESTS",
    500: "INTERNAL SERVER ERROR",
    503: "SERVICE UNAVAILABLE",
}
```

For any status code not in this map, return an empty string `""`.

#### 4.3.3 Response from PingAuthorize

```json
{
    "response_code": "200",
    "body": "<final response body>",
    "headers": [
        {"content-type": "application/json"},
        {"x-custom-header": "value"}
    ]
}
```

The plugin replaces the upstream response entirely with PingAuthorize's response.

---

## 5. Phase Behavior

### 5.1 Access Phase

**Kong lifecycle hook**: `Access(kong *pdk.PDK)`

**Pseudocode**:
```
func Access(kong, config):
    // Start OTel span if enabled
    span = startSpan("ping-authorize.access") if config.EnableOtel

    parsed_url = ParseURL(config.ServiceURL)
    payload, original_request = ComposeAccessPayload(kong, config, parsed_url)

    debugLog(config, "Sending sideband request", payload)

    status, _, body, err = ExecuteHTTP(config, parsed_url, payload)  // includes retry + circuit breaker

    if err != nil:
        if config.FailOpen:
            log.Warn("PingAuthorize unreachable, fail-open enabled, allowing request")
            StorePerRequestContext(kong, original_request, nil)
            return  // allow request through
        return kong.Response.Exit(502)

    state = HandleAccessResponse(kong, config, status, body)

    StorePerRequestContext(kong, original_request, state)
```

#### 5.1.1 Compose Access Payload

Build the JSON body:

1. `source_ip` = client remote address
2. `source_port` = client remote port
3. `method` = request HTTP method
4. `url` = reconstructed forwarded URL (scheme + host + port + path + optional query)
5. `body` = raw request body
6. `headers` = request headers converted to Sideband format
7. Decode and re-encode query string (max 100 arguments) — append to URL if non-empty
8. `http_version` = request HTTP version string (may be `"2"` for HTTP/2 clients — this is accepted and passed through)
9. `client_certificate` = JWK with x5c (see section 6); omit if no client cert or Kong OSS
10. JSON-encode the payload; on failure return 400

Build the HTTP request:

1. Path = `{parsed_url.path}/sideband/request` (ensure single `/` separator)
2. Headers: Host, Connection: Keep-Alive, Content-Type: application/json, User-Agent: Kong/{version}, Content-Length, {secret_header_name}: {shared_secret}
3. Method = POST, HTTP version = 1.1 (always, regardless of client protocol)

#### 5.1.2 Handle Access Response

1. JSON-decode the response body. On failure → **502** (or allow if `fail_open`).
2. Check `IsFailedRequest(status_code, body, config)`. If true → **502** (or allow if `fail_open`).
3. If `body.response` exists (request DENIED):
   - Extract `response_code`, `body`, `headers` from the response object.
   - Return the deny response to the client immediately with the specified status code, body, and flattened headers.
4. If `body.response` does NOT exist (request ALLOWED):
   - Call `UpdateRequest(body)` to apply any modifications.
   - Return `body.state` (as `json.RawMessage`; may be nil).

#### 5.1.3 Update Request (Apply PingAuthorize Modifications)

The policy provider may modify headers, method, URL, and body. The plugin must diff the original vs. returned values and apply changes.

**Headers**:

1. Flatten both original and new headers from Sideband format to maps.
2. For each original header:
   - If it exists in new headers: compare value arrays element-by-element and order-sensitive. If different, call `kong.ServiceRequest.SetHeaders()` with new values.
   - If it does NOT exist in new headers: call `kong.ServiceRequest.ClearHeader()`.
3. For each new header not in original headers: call `kong.ServiceRequest.SetHeaders()` to add it.
4. If `config.StripAcceptEncoding` is `true`: call `kong.ServiceRequest.ClearHeader("Accept-Encoding")`.

**Method**:

- If `original.method != new.method`, call `kong.ServiceRequest.SetMethod(new.method)`.

**URL**:

- If `original.url != new.url`:
  - Parse both URLs.
  - If host or port changed: set the `Host` header to `{new_host}:{new_port}`.
  - If path changed: call `kong.ServiceRequest.SetPath(new.path)`.
  - If query changed: set URI args to `new.query`.
  - If scheme changed: log a warning (scheme changes are not supported).

**Body**:

- If body changed (and it's not just empty-string vs. nil):
  - If new body is nil/null: call `kong.ServiceRequest.SetRawBody("")`.
  - Otherwise: call `kong.ServiceRequest.SetRawBody(new.body)`.
- **Note**: Body modification may affect Content-Length, so perform this after header manipulation.

**Unsupported Modifications** (log warning only):

- `source_ip`
- `source_port`
- `client_certificate`
- URL `scheme`

### 5.2 Response Phase

**Kong lifecycle hook**: `Response(kong *pdk.PDK)`

**Pseudocode**:
```
func Response(kong, config):
    // Skip entirely if configured
    if config.SkipResponsePhase:
        return

    original_request = LoadPerRequestContext(kong, "original_request")
    state = LoadPerRequestContext(kong, "state")

    // Start OTel span if enabled
    span = startSpan("ping-authorize.response") if config.EnableOtel

    parsed_url = ParseURL(config.ServiceURL)
    payload = ComposeResponsePayload(kong, config, original_request, state, parsed_url)

    debugLog(config, "Sending sideband response", payload)

    status, _, body, err = ExecuteHTTP(config, parsed_url, payload)  // includes retry + circuit breaker

    if err != nil:
        if config.FailOpen:
            log.Warn("PingAuthorize unreachable during response phase, fail-open, passing upstream response through")
            return  // pass upstream response through unmodified
        return kong.Response.Exit(502)

    HandleResponsePhaseResult(kong, config, status, body)
```

#### 5.2.1 Compose Response Payload

Build the JSON body:

1. `method` = current request method
2. `url` = reconstructed forwarded URL (same logic as access phase)
3. `body` = **upstream response body** (from `kong.ServiceResponse.GetRawBody()`)
4. `response_code` = upstream response status code **as a string**
5. `response_status` = status string from the mapping table (section 4.3.2)
6. `headers` = upstream **response** headers in Sideband format
7. Decode/re-encode query string, append to URL if non-empty
8. `http_version` = request HTTP version string (pass through as-is, including `"2"`)
9. If `state` is non-nil: set `state` field (as `json.RawMessage`). Otherwise: set `request` field to `original_request`.

Build the HTTP request (same structure as access phase but with `/sideband/response` path).

#### 5.2.2 Handle Response Phase Result

1. JSON-decode the response body. On failure → **502** (or pass through if `fail_open`).
2. Check `IsFailedRequest(status_code, body, config)`. If true → **502** (or pass through if `fail_open`).
3. Flatten the headers from the PingAuthorize response.
4. Remove any upstream response headers that are NOT present in the PingAuthorize response, **except** these allowed headers which are always preserved:
   - `content-length`
   - `date`
   - `connection`
   - `vary`
5. Return the final response to the client: `kong.Response.Exit(response_code, body, flattened_headers)`.

---

## 6. Client Certificate Handling

Client certificate extraction is supported on both Kong Enterprise (via `mtls-auth` plugin) and Kong OSS. The plugin detects the environment and gracefully degrades — if the TLS certificate chain API is unavailable (Kong OSS without mTLS), the `client_certificate` field is simply omitted from the sideband payload.

### 6.1 Supported Key Types

| Key Type | JWK `kty` | Fields |
|---|---|---|
| RSA | `RSA` | `n` (modulus), `e` (exponent) |
| EC (ECDSA) | `EC` | `crv` (curve name), `x`, `y` (coordinates) |
| Ed25519 | `OKP` | `crv` = `Ed25519`, `x` (public key) |

### 6.2 Extraction Flow

```
1. cert_pem = kong.Client.GetClientCertificateChain()
   → If error or nil, skip silently (no client cert or Kong OSS)

2. certs = x509.ParseCertificates(cert_pem)
   → On error, return 400

3. leaf = certs[0]
4. public_key = leaf.PublicKey
   → Detect key type (RSA, EC, Ed25519)

5. jwk = MarshalPublicKeyToJWK(public_key)
   → Handle all three key types
   → On error, return 400

6. if config.IncludeFullCertChain:
       jwk["x5c"] = [base64(cert.DER) for cert in certs]
   else:
       jwk["x5c"] = [base64(leaf.DER)]

7. Return jwk as the client_certificate field
```

### 6.3 Output Format

```json
{
    "kty": "RSA",
    "n": "<modulus-base64url>",
    "e": "<exponent-base64url>",
    "x5c": ["<base64-DER-encoded-certificate>"]
}
```

### 6.4 Go Implementation Notes

- Use `crypto/x509` for certificate parsing.
- Use a JWK library (e.g., `go-jose/v4`) or manual construction for JWK output.
- The `x5c` values use standard Base64 (not Base64URL) per RFC 7517 Section 4.7.
- JWK key parameters (`n`, `e`, `x`, `y`) use Base64URL encoding per RFC 7518.
- Handle `*rsa.PublicKey`, `*ecdsa.PublicKey`, and `ed25519.PublicKey` types.

---

## 7. Circuit Breaker

### 7.1 Overview

The circuit breaker protects against cascading failures when PingAuthorize is overloaded or unavailable. It is scoped **per plugin instance** — each Route/Service configuration gets its own independent circuit breaker. It can be disabled via `circuit_breaker_enabled = false`.

### 7.2 State

```go
type CircuitBreaker struct {
    mu            sync.Mutex
    enabled       bool
    closed        bool      // true = circuit is closed (allowing traffic)
    openedAt      time.Time // when the circuit was opened
    retryAfterSec int       // seconds to wait before re-closing
}
```

**Initial state**: Circuit is closed (traffic flows normally).

### 7.3 Triggers

The circuit breaker opens (trips) on any of the following conditions:

| Trigger | Source | Retry-After |
|---|---|---|
| HTTP 429 from PingAuthorize | Rate limiting | Value from `Retry-After` response header |
| HTTP 5xx from PingAuthorize | Server error | Fixed default: 30 seconds |
| Connection timeout | Unreachable | Fixed default: 30 seconds |
| Read/write timeout | Network issue | Fixed default: 30 seconds |

### 7.4 State Transitions

```
                    ┌──────────────────────┐
                    │    CLOSED            │
                    │  (normal operation)  │
                    └──────┬───────────────┘
                           │
                    HTTP 429 / 5xx /
                    timeout from PingAuthorize
                           │
                           ▼
                    ┌──────────────────────┐
                    │    OPEN              │
                    │  (rejecting traffic) │──── Return 429 to client
                    └──────┬───────────────┘    (or 502 for 5xx/timeout triggers)
                           │
                    Retry-After timer expires
                           │
                           ▼
                    ┌──────────────────────┐
                    │    CLOSED            │
                    │  (normal operation)  │
                    └──────────────────────┘
```

### 7.5 Logic

On every request:
1. If `circuit_breaker_enabled` is `false`: skip circuit breaker, always attempt the request.
2. If circuit is OPEN and `time.Since(openedAt) > retryAfterSec`: transition to CLOSED.
3. If circuit is CLOSED: send the HTTP request to PingAuthorize (with retries per config).
   - If response triggers the breaker: record `openedAt = now`, set `retryAfterSec`, transition to OPEN, return appropriate error to client.
4. If circuit is OPEN (timer not yet expired): return error to client immediately without making a network call.

**Important**: Check for nil/error on the HTTP response BEFORE inspecting status codes. This fixes the nil dereference bug in the Lua version.

### 7.6 Client Response When Circuit Is Open

For **429 triggers**:
```json
{"code": "LIMIT_EXCEEDED", "message": "The request exceeded the allowed rate limit. Please try after 1 second."}
```
Headers: `Content-Type: application/json`, `Retry-After: <remaining_seconds>`

For **5xx/timeout triggers**: Return 502 with empty body (or allow through if `fail_open` is enabled).

### 7.7 Concurrency

The circuit breaker state MUST be protected with `sync.Mutex` since Go handles concurrent requests across goroutines. Keep the critical section minimal — only the state check and transition should be locked.

### 7.8 Persistence

Circuit breaker state is ephemeral. It resets when the Go plugin process restarts.

---

## 8. Retry Logic

### 8.1 Behavior

When `max_retries > 0`, the plugin retries failed sideband calls before giving up.

### 8.2 Retry Conditions

A request is retried on:
- Connection errors (dial timeout, connection refused)
- Read/write timeouts
- HTTP 5xx responses from PingAuthorize

A request is **NOT** retried on:
- HTTP 429 (rate limited — goes to circuit breaker instead)
- HTTP 4xx (client errors from PingAuthorize — these are deterministic)
- Successful responses (2xx)
- JSON encoding errors (local failure, not transient)

### 8.3 Retry Flow

```
attempt = 0
while attempt <= config.MaxRetries:
    response, err = httpClient.Do(request)
    if err == nil and response.StatusCode < 500:
        return response  // success or non-retryable error
    attempt++
    if attempt <= config.MaxRetries:
        sleep(config.RetryBackoffMs)
return last response or error  // all retries exhausted
```

### 8.4 Interaction with Circuit Breaker

- Retries occur BEFORE the circuit breaker trips. If all retries fail, the final failure triggers the circuit breaker.
- If the circuit breaker is already OPEN, retries are skipped entirely (the circuit breaker short-circuits).

---

## 9. Error Handling

### 9.1 Fail-Closed / Fail-Open

The plugin defaults to **fail-closed** mode. When `fail_open` is configured:

| Scenario | Fail-Closed (default) | Fail-Open |
|---|---|---|
| PingAuthorize unreachable | 502 | Allow request through |
| PingAuthorize returns 5xx | 502 | Allow request through |
| Connection/read timeout | 502 | Allow request through |
| PingAuthorize returns invalid JSON | 502 | Allow request through |
| PingAuthorize returns 4xx (non-passthrough) | 502 | 502 (still blocked — this is a config/auth error, not transient) |
| Unexpected panic | 500 | 500 (always fail-closed for panics) |
| JSON encode failure (local) | 500 | 500 (always fail-closed for local errors) |
| Client cert parse failure | 400 | 400 (always fail-closed for client errors) |

### 9.2 Error Status Code Summary

| Error Condition | HTTP Status | Phase |
|---|---|---|
| Unexpected panic | 500 | Both |
| JSON encode failure (local) | 400 (access) / 500 (response) | Both |
| Client certificate parse failure | 400 | Access |
| PingAuthorize unreachable (fail-closed) | 502 | Both |
| PingAuthorize returns invalid JSON (fail-closed) | 502 | Both |
| PingAuthorize returns 4xx (non-passthrough) | 502 | Both |
| PingAuthorize returns passthrough status code | Passthrough (e.g. 413) with body | Both |
| PingAuthorize returns 5xx (fail-closed) | 502 | Both |
| Circuit breaker open (429 trigger) | 429 | Both |
| Circuit breaker open (5xx/timeout trigger) | 502 | Both |
| Multidimensional header values | 400 | Both |

Plugin-generated error responses (500, 502) return empty bodies.

### 9.3 Failed Request Detection

```go
func IsFailedRequest(statusCode int, body *SidebandErrorResponse, config *Config) bool {
    if statusCode >= 400 && statusCode < 500 {
        log.Warn("Sideband request denied", "status", statusCode, "message", body.Message, "id", body.ID)
        // Check if this status code should be passed through directly
        for _, code := range config.PassthroughStatusCodes {
            if statusCode == code {
                kong.Response.Exit(statusCode, body, map[string][]string{"Content-Type": {"application/json"}})
                return false // unreachable
            }
        }
        return true
    }
    if statusCode >= 500 {
        log.Error("Sideband request failed", "status", statusCode, "message", body.Message, "id", body.ID)
        return true
    }
    return false
}
```

---

## 10. Data Structures

### 10.1 Sideband Request Payload (Access Phase)

```go
type SidebandAccessRequest struct {
    SourceIP          string              `json:"source_ip"`
    SourcePort        string              `json:"source_port"`
    Method            string              `json:"method"`
    URL               string              `json:"url"`
    Body              string              `json:"body"`
    Headers           []map[string]string `json:"headers"`
    HTTPVersion       string              `json:"http_version"`
    ClientCertificate *JWK                `json:"client_certificate,omitempty"`
}
```

### 10.2 Sideband Access Response (from PingAuthorize)

```go
type SidebandAccessResponse struct {
    SourceIP          string              `json:"source_ip"`
    SourcePort        string              `json:"source_port"`
    Method            string              `json:"method"`
    URL               string              `json:"url"`
    Body              *string             `json:"body"`     // pointer to distinguish nil vs empty
    Headers           []map[string]string `json:"headers"`
    ClientCertificate *JWK                `json:"client_certificate,omitempty"`
    State             json.RawMessage     `json:"state,omitempty"`     // opaque JSON object from PingAuthorize
    Response          *DenyResponse       `json:"response,omitempty"` // present when request is denied
}

type DenyResponse struct {
    ResponseCode   string              `json:"response_code"`
    ResponseStatus string              `json:"response_status"`
    Body           string              `json:"body,omitempty"`
    Headers        []map[string]string `json:"headers,omitempty"`
}
```

### 10.3 Sideband Response Payload (Response Phase)

```go
type SidebandResponsePayload struct {
    Method         string                 `json:"method"`
    URL            string                 `json:"url"`
    Body           string                 `json:"body"`
    ResponseCode   string                 `json:"response_code"`
    ResponseStatus string                 `json:"response_status"`
    Headers        []map[string]string    `json:"headers"`
    HTTPVersion    string                 `json:"http_version"`
    State          json.RawMessage        `json:"state,omitempty"`   // from access phase
    Request        *SidebandAccessRequest `json:"request,omitempty"` // fallback if no state
}
```

### 10.4 Sideband Response Phase Result (from PingAuthorize)

```go
type SidebandResponseResult struct {
    ResponseCode string              `json:"response_code"`
    Body         string              `json:"body,omitempty"`
    Headers      []map[string]string `json:"headers"`
    Message      string              `json:"message,omitempty"` // for error responses
    ID           string              `json:"id,omitempty"`      // for error responses
}
```

### 10.5 Parsed URL

```go
type ParsedURL struct {
    Scheme string
    Host   string
    Port   int
    Path   string
    Query  string
}
```

Default port: 80 for HTTP, 443 for HTTPS. Default path: `/`.

### 10.6 JWK (Client Certificate)

```go
type JWK struct {
    Kty string   `json:"kty"`
    // RSA
    N   string   `json:"n,omitempty"`
    E   string   `json:"e,omitempty"`
    // EC (ECDSA)
    Crv string   `json:"crv,omitempty"`
    X   string   `json:"x,omitempty"`
    Y   string   `json:"y,omitempty"`
    // Ed25519 (OKP) — uses Crv and X fields
    // Certificate chain
    X5C []string `json:"x5c"`
}
```

---

## 11. Per-Request State Management

### 11.1 Problem

The access and response phases execute at different points in Kong's lifecycle. Data from the access phase (the `original_request` and `state` from PingAuthorize) must be available in the response phase.

### 11.2 Go Implementation

Use Kong's per-request context (`kong.Ctx`) to store per-request data safely:

```go
// In Access phase — serialize to JSON for storage
func StorePerRequestContext(kong *pdk.PDK, originalRequest *SidebandAccessRequest, state json.RawMessage) error {
    reqJSON, _ := json.Marshal(originalRequest)
    kong.Ctx.SetShared("paz_original_request", string(reqJSON))
    if state != nil {
        kong.Ctx.SetShared("paz_state", string(state))
    }
    return nil
}

// In Response phase — deserialize
func LoadPerRequestContext(kong *pdk.PDK) (*SidebandAccessRequest, json.RawMessage, error) {
    reqStr, _ := kong.Ctx.GetSharedString("paz_original_request")
    var req SidebandAccessRequest
    json.Unmarshal([]byte(reqStr), &req)

    stateStr, err := kong.Ctx.GetSharedString("paz_state")
    var state json.RawMessage
    if err == nil && stateStr != "" {
        state = json.RawMessage(stateStr)
    }
    return &req, state, nil
}
```

This fixes the race condition in the Lua version which used module-level globals shared across requests.

---

## 12. HTTP Client

### 12.1 Configuration

| Parameter | Source | Description |
|---|---|---|
| Timeout | `config.ConnectionTimeoutMs` | Combined connection/read/write timeout |
| Keep-Alive | `config.ConnectionKeepaliveMs` | Keep-alive duration for connection reuse |
| TLS Verify | `config.VerifyServiceCert` | Whether to verify PingAuthorize's TLS cert |
| HTTP Version | Hardcoded 1.1 | All sideband requests use HTTP/1.1 |
| Max Retries | `config.MaxRetries` | Retry count for transient failures |
| Retry Backoff | `config.RetryBackoffMs` | Delay between retries |

### 12.2 Go Implementation

```go
type SidebandHTTPClient struct {
    client *http.Client
    cb     *CircuitBreaker
    config *Config
}

func NewSidebandHTTPClient(config *Config) *SidebandHTTPClient {
    transport := &http.Transport{
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: !config.VerifyServiceCert,
            // TLSClientConfig can be extended for future mTLS to PingAuthorize
        },
        IdleConnTimeout:     time.Duration(config.ConnectionKeepaliveMs) * time.Millisecond,
        MaxIdleConnsPerHost: 10,
        ForceAttemptHTTP2:   false, // Force HTTP/1.1
    }

    client := &http.Client{
        Timeout:   time.Duration(config.ConnectionTimeoutMs) * time.Millisecond,
        Transport: transport,
    }

    var cb *CircuitBreaker
    if config.CircuitBreakerEnabled {
        cb = NewCircuitBreaker()
    }

    return &SidebandHTTPClient{client: client, cb: cb, config: config}
}
```

### 12.3 mTLS Extensibility

The `TLSClientConfig` in the transport is designed to be extended for future mTLS support to PingAuthorize. When that feature is implemented, add `Certificates` and `RootCAs` fields to the TLS config, controlled by new configuration fields (e.g., `client_cert_path`, `client_key_path`).

### 12.4 Request Construction

Both sideband endpoints use the same HTTP request structure:

```
POST {path} HTTP/1.1
Host: {host}:{port}
Connection: Keep-Alive
Content-Type: application/json
User-Agent: Kong/{plugin_version}
Content-Length: {len(body)}
{secret_header_name}: {shared_secret}

{json_body}
```

### 12.5 Connection Reuse

Go's `http.Client` with `http.Transport` provides connection pooling automatically. Configure `MaxIdleConnsPerHost` and `IdleConnTimeout` to match connection reuse behavior.

---

## 13. Observability

### 13.1 Structured Logging

All log output uses structured JSON format with named fields for machine-parseable log entries.

**Standard fields** included in every log entry:

| Field | Description |
|---|---|
| `plugin` | Always `"idpartners-ping-authorize"` |
| `phase` | `"access"` or `"response"` |
| `service_url` | The configured PingAuthorize URL |
| `level` | `"debug"`, `"info"`, `"warn"`, or `"error"` |
| `msg` | Human-readable message |

**Example log entries**:
```json
{"plugin":"idpartners-ping-authorize","phase":"access","level":"debug","msg":"Sending sideband request","service_url":"https://paz.example.com","path":"/sideband/request"}
{"plugin":"idpartners-ping-authorize","phase":"access","level":"info","msg":"Request denied by policy provider","status_code":403}
{"plugin":"idpartners-ping-authorize","phase":"access","level":"error","msg":"PingAuthorize unreachable","error":"dial timeout"}
```

### 13.2 Debug Log Redaction

When `enable_debug_logging` is `true`, request/response payloads are logged. To protect sensitive data:

1. **Header redaction**: Header values for names listed in `redact_headers` are replaced with `[REDACTED]`. The `secret_header_name` is always redacted regardless of the `redact_headers` list.
2. **Body truncation**: Bodies exceeding `debug_body_max_bytes` are truncated with a `... [truncated, {total_size} bytes]` suffix. Set to `0` to disable truncation.

### 13.3 OpenTelemetry Integration

When `enable_otel` is `true`, the plugin emits:

**Traces**:
- Span per sideband call (`ping-authorize.access`, `ping-authorize.response`)
- Attributes: `http.method`, `http.status_code`, `http.url`, `policy.decision` (allow/deny), `circuit_breaker.state`
- Error status on failures

**Metrics**:
- `ping_authorize_sideband_duration_ms` (histogram) — sideband call latency
- `ping_authorize_sideband_total` (counter) — total sideband calls, labeled by phase and result
- `ping_authorize_circuit_breaker_state` (gauge) — 0 = closed, 1 = open
- `ping_authorize_policy_decisions_total` (counter) — allow/deny counts

### 13.4 Kong PDK Logging

Regardless of OTel configuration, the plugin always uses Kong's PDK logging (`kong.Log`) for warnings and errors, ensuring they appear in Kong's standard error log.

---

## 14. Known Limitations

1. **Transfer-Encoding not supported**: Due to Kong issue [#8083](https://github.com/Kong/kong/issues/8083). This is a Kong platform limitation.

2. **mTLS client cert requires Kong Enterprise**: Full client certificate chain extraction requires the `mtls-auth` Kong Enterprise plugin. On Kong OSS, the `client_certificate` field is omitted from sideband payloads.

3. **Unsupported request modifications**: PingAuthorize cannot modify `source_ip`, `source_port`, URL `scheme`, or `client_certificate` via the `/sideband/request` response. Changes to these fields are logged as warnings but ignored.

4. **Query string argument limit**: Query strings are decoded with a maximum of 100 arguments. Arguments beyond this limit are silently dropped.

5. **Sideband calls are HTTP/1.1 only**: Even when clients use HTTP/2 to Kong, all sideband calls to PingAuthorize use HTTP/1.1.

6. **External process IPC overhead**: As a Go PDK plugin, there is inherent IPC latency for every Kong PDK call. PDK calls should be minimized and batched where possible.

---

## 15. Go Implementation

### 15.1 Project Structure

```
idpartners-ping-authorize/
├── go.mod
├── go.sum
├── main.go                // Plugin entry point, Kong PDK registration
├── config.go              // Config struct, validation, defaults
├── provider.go            // PolicyProvider interface
├── sideband_provider.go   // PingAuthorize Sideband implementation
├── access.go              // Access phase logic
├── response.go            // Response phase logic
├── network.go             // SidebandHTTPClient, retry logic
├── circuit_breaker.go     // Circuit breaker implementation
├── headers.go             // Header format conversion (Sideband ↔ standard)
├── certificate.go         // Client certificate → JWK (RSA, EC, Ed25519)
├── types.go               // Shared data structures
├── observability.go       // Structured logging, OTel setup, redaction
├── access_test.go
├── response_test.go
├── network_test.go
├── circuit_breaker_test.go
├── headers_test.go
├── certificate_test.go
├── observability_test.go
└── integration_test.go    // Integration tests with mock PingAuthorize
```

### 15.2 Kong Go PDK Integration

```go
package main

import (
    "github.com/Kong/go-pdk"
    "github.com/Kong/go-pdk/server"
)

const (
    PluginName = "idpartners-ping-authorize"
    Version    = "2.0.0"
    Priority   = 999
)

func New() interface{} {
    return &Config{}
}

func (conf *Config) Access(kong *pdk.PDK) {
    // Access phase logic — fail-closed by default
    defer func() {
        if r := recover(); r != nil {
            kong.Log.Err("Unexpected panic in access phase: ", r)
            kong.Response.Exit(500, nil, nil)
        }
    }()
    executeAccess(kong, conf)
}

func (conf *Config) Response(kong *pdk.PDK) {
    if conf.SkipResponsePhase {
        return
    }
    // Response phase logic — fail-closed by default
    defer func() {
        if r := recover(); r != nil {
            kong.Log.Err("Unexpected panic in response phase: ", r)
            kong.Response.Exit(500, nil, nil)
        }
    }()
    executeResponse(kong, conf)
}

func main() {
    server.StartServer(New, Version, Priority)
}
```

### 15.3 Testing Strategy

#### Unit Tests (mock servers)

For all pure functions:
- Header format conversion (both directions, edge cases)
- URL parsing
- Status string mapping
- Client certificate JWK construction (RSA, EC, Ed25519)
- Request/response payload composition
- Circuit breaker state transitions (429, 5xx, timeout triggers)
- Retry logic (backoff, retry conditions)
- Config validation
- Log redaction (header masking, body truncation)

#### Integration Tests (mock PingAuthorize via `httptest.Server`)

- Full access phase: allow, deny, modify scenarios
- Full response phase: allow, modify scenarios
- Skip response phase when configured
- Error scenarios: unreachable provider, invalid JSON, 4xx/5xx responses
- Fail-open behavior when provider is down
- Circuit breaker: triggering on 429/5xx/timeout and recovery
- Retry exhaustion
- Passthrough status codes
- Concurrent request handling (verify no race conditions)

#### Live Integration Tests (PingAuthorize instance)

Separate test suite tagged with `//go:build integration` for running against a live PingAuthorize instance. Example request/response payloads to be provided.

### 15.4 Key Differences from Lua

| Aspect | Lua (`ping-auth`) | Go (`idpartners-ping-authorize`) |
|---|---|---|
| Kong version | 2.5.x+ | 3.x only |
| Concurrency | Single-threaded per worker | Multi-threaded; mutex-protected state |
| Per-request state | Module-level globals (race condition bug) | Kong PDK `kong.Ctx` per-request context |
| HTTP client | `resty.http` (OpenResty) | `net/http` with retry support |
| HTTP/2 | Rejected with 400/500 | Accepted from clients; sideband uses HTTP/1.1 |
| Circuit breaker scope | Module-level (per-worker) | Per plugin instance with mutex |
| Circuit breaker triggers | 429 only | 429, 5xx, and timeouts |
| Fail mode | Always fail-closed | Configurable (default fail-closed) |
| Response phase | Always runs | Configurable (`skip_response_phase`) |
| Accept-Encoding | Always stripped | Configurable (`strip_accept_encoding`) |
| JSON | `cjson.safe` (returns nil on error) | `encoding/json` (returns error) |
| Certificate keys | RSA only | RSA, EC, Ed25519 |
| Certificate chain | Leaf only | Configurable (leaf or full chain) |
| Logging | String-formatted, prefixed `[ping-auth]` | Structured JSON with named fields |
| Observability | None | OpenTelemetry traces + metrics (configurable) |
| Debug redaction | None | Configurable header redaction + body truncation |
| State field | `interface{}` | `json.RawMessage` |
| Retries | None | Configurable with backoff |
| Passthrough codes | Hardcoded 413 | Configurable list |
| Plugin hosting | Embedded in Kong worker | External process (socket communication) |

---

## 16. Appendix

### A. Complete Sideband API Endpoint Summary

| Endpoint | Method | Phase | Purpose |
|---|---|---|---|
| `{service_url}/sideband/request` | POST | Access | Submit client request for policy evaluation |
| `{service_url}/sideband/response` | POST | Response | Submit upstream response for final policy evaluation |

### B. HTTP Status Code Usage Summary

| Code | Meaning in Plugin Context |
|---|---|
| 400 | Client error (bad cert, bad JSON, multidimensional headers) |
| 413 | Default passthrough from PingAuthorize (configurable) |
| 429 | Circuit breaker active (rate limited by PingAuthorize) |
| 500 | Unexpected internal error (always fail-closed) |
| 502 | PingAuthorize communication failure or error response (fail-closed) |
| * | Any code from PingAuthorize deny response is passed through to client |

### C. Header Processing Rules Summary

| Scenario | Action |
|---|---|
| Header in original, not in policy response | Remove from request |
| Header in original, changed in policy response | Update to new values |
| Header not in original, added in policy response | Add to request |
| `Accept-Encoding` header | Removed if `strip_accept_encoding` is `true` (default) |
| Response phase: header in upstream, not in policy response | Remove (except allowed list) |
| Response phase: allowed headers (`content-length`, `date`, `connection`, `vary`) | Always preserved |

### D. Full Configuration Example

```yaml
plugins:
  - name: idpartners-ping-authorize
    config:
      service_url: "https://pingauthorize.example.com:443/policy"
      shared_secret: "{vault://env/PAZ_SECRET}"
      secret_header_name: "X-Ping-Secret"
      connection_timeout_ms: 5000
      connection_keepalive_ms: 30000
      verify_service_cert: true
      skip_response_phase: false
      fail_open: false
      passthrough_status_codes: [413]
      max_retries: 2
      retry_backoff_ms: 250
      circuit_breaker_enabled: true
      strip_accept_encoding: true
      include_full_cert_chain: false
      enable_debug_logging: false
      enable_otel: true
      redact_headers: ["authorization", "cookie", "x-api-key"]
      debug_body_max_bytes: 8192
```

## 12. MCP Support (v2.1.0)

### 12.1 Overview

MCP (Model Context Protocol) support adds the ability to detect, parse, and enrich JSON-RPC 2.0 MCP traffic passing through Kong. When `enable_mcp` is `true`, the plugin inspects request bodies for MCP JSON-RPC messages and adds MCP context to sideband payloads, enabling PingAuthorize to make MCP-aware authorization decisions.

### 12.2 MCP Sideband Payload Schema

**Access Request (additional fields when MCP detected):**

```json
{
  "traffic_type": "mcp",
  "mcp": {
    "mcp_method": "tools/call",
    "mcp_tool_name": "get_weather",
    "mcp_tool_arguments": {"city": "London"},
    "mcp_jsonrpc_id": 1
  },
  "extracted_headers": {
    "authorization": "Bearer token..."
  }
}
```

**Response Payload (additional fields):**

```json
{
  "traffic_type": "mcp",
  "mcp": {
    "mcp_method": "tools/call",
    "mcp_tool_name": "get_weather",
    "mcp_jsonrpc_id": 1
  }
}
```

### 12.3 SSE Stream Handling

When the upstream response has `Content-Type: text/event-stream`, the plugin extracts the last JSON-RPC response from the SSE event stream. SSE events are `data: <json>\n\n` separated. Only the final JSON-RPC response (with `id` field) is sent to PingAuthorize for evaluation.

### 12.4 JSON-RPC Error Response Format

When `mcp_jsonrpc_errors` is `true` and the request is MCP traffic, deny responses are formatted as JSON-RPC 2.0 errors:

```json
{"jsonrpc": "2.0", "id": 1, "error": {"code": -32600, "message": "Access denied"}}
```

**HTTP to JSON-RPC error code mapping:**

| HTTP Status | JSON-RPC Code | Meaning |
|-------------|---------------|---------|
| 400 | -32600 | Invalid Request |
| 401/403 | -32600 | Invalid Request (unauthorized) |
| 404 | -32601 | Method not found |
| 429 | -32000 | Server error (rate limited) |
| 500 | -32603 | Internal error |
| 502/503 | -32000 | Server error (unavailable) |

### 12.5 Tool Argument Modification

When PingAuthorize modifies the request body for an MCP `tools/call` request, the plugin validates that the modified body is still valid JSON-RPC 2.0. If validation fails, the body is used as-is with a warning logged.

### 12.6 Tools/List Response Filtering

PingAuthorize can filter `tools/list` and `resources/list` responses by returning a modified body in the `SidebandResponseResult`. The plugin passes the modified body through to the client, enabling tool-level authorization.

### 12.7 MCP-Aware Retry

Non-idempotent MCP methods (`tools/call`, `resources/read`, `prompts/get`) are never retried by default. Idempotent methods (`tools/list`, `resources/list`, `prompts/list`, `initialize`) follow normal retry configuration. The `mcp_retry_methods` config field allows operators to override which methods are retryable.

### 12.8 MCP Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enable_mcp` | bool | false | Master toggle for MCP detection |
| `mcp_jsonrpc_errors` | bool | false | Return JSON-RPC 2.0 error format |
| `max_sideband_body_bytes` | int | 0 | Max payload size (0 = unlimited) |
| `extract_headers` | []string | [] | Headers to extract as top-level fields |
| `mcp_retry_methods` | []string | [tools/list, resources/list, prompts/list, initialize] | Retryable MCP methods |
