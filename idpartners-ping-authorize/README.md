# idpartners-ping-authorize

A Kong Gateway Go plugin that integrates with PingAuthorize via the Sideband API protocol. It intercepts requests during Kong's access and response phases to consult PingAuthorize for policy decisions (allow, deny, or modify).

**Version:** 2.1.0
**Kong:** 3.x (Go external process plugin)
**Priority:** 999

## Prerequisites

- Kong Gateway 3.x
- Go 1.21+ (build only)
- PingAuthorize instance with Sideband API enabled

## Build

```bash
cd idpartners-ping-authorize
go build -o idpartners-ping-authorize .
```

This produces a single standalone binary.

### Cross-compilation

```bash
# Linux (typical for production Kong deployments)
GOOS=linux GOARCH=amd64 go build -o idpartners-ping-authorize .

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o idpartners-ping-authorize .
```

## Run Tests

```bash
go test ./... -v        # verbose output
go test ./... -race     # race condition detection
go vet ./...            # static analysis
```

## Deploy to Kong

### 1. Place the binary

Copy the compiled binary to a path accessible by the Kong process:

```bash
cp idpartners-ping-authorize /usr/local/bin/
chmod +x /usr/local/bin/idpartners-ping-authorize
```

### 2. Configure Kong

Add to `kong.conf`:

```
plugins = bundled,idpartners-ping-authorize
pluginserver_names = idpartners-ping-authorize
pluginserver_idpartners_ping_authorize_start_cmd = /usr/local/bin/idpartners-ping-authorize
pluginserver_idpartners_ping_authorize_query_cmd = /usr/local/bin/idpartners-ping-authorize -dump
```

> **Note:** Kong config keys use underscores in the pluginserver directives. The plugin name `idpartners-ping-authorize` maps to `idpartners_ping_authorize` in these keys.

### 3. Restart Kong

```bash
kong restart
```

### 4. Enable the plugin

**On a specific route:**

```bash
curl -i -X POST http://localhost:8001/routes/{route_id}/plugins \
  --data "name=idpartners-ping-authorize" \
  --data "config.service_url=https://pingauthorize.example.com:443" \
  --data "config.shared_secret=your-secret" \
  --data "config.secret_header_name=X-Ping-Secret"
```

**On a specific service:**

```bash
curl -i -X POST http://localhost:8001/services/{service_id}/plugins \
  --data "name=idpartners-ping-authorize" \
  --data "config.service_url=https://pingauthorize.example.com:443" \
  --data "config.shared_secret=your-secret" \
  --data "config.secret_header_name=X-Ping-Secret"
```

**Globally:**

```bash
curl -i -X POST http://localhost:8001/plugins \
  --data "name=idpartners-ping-authorize" \
  --data "config.service_url=https://pingauthorize.example.com:443" \
  --data "config.shared_secret=your-secret" \
  --data "config.secret_header_name=X-Ping-Secret"
```

## Deploy with Docker

```dockerfile
FROM kong:3.6

# Copy the plugin binary
COPY idpartners-ping-authorize /usr/local/bin/idpartners-ping-authorize
RUN chmod +x /usr/local/bin/idpartners-ping-authorize

# Configure Kong to load the plugin
ENV KONG_PLUGINS=bundled,idpartners-ping-authorize
ENV KONG_PLUGINSERVER_NAMES=idpartners-ping-authorize
ENV KONG_PLUGINSERVER_IDPARTNERS_PING_AUTHORIZE_START_CMD=/usr/local/bin/idpartners-ping-authorize
ENV KONG_PLUGINSERVER_IDPARTNERS_PING_AUTHORIZE_QUERY_CMD="/usr/local/bin/idpartners-ping-authorize -dump"
```

Build and run:

```bash
# Build the plugin binary for Linux first
GOOS=linux GOARCH=amd64 go build -o idpartners-ping-authorize .

# Build the Docker image
docker build -t kong-with-ping-authorize .

# Run
docker run -d --name kong \
  -e "KONG_DATABASE=off" \
  -e "KONG_DECLARATIVE_CONFIG=/etc/kong/kong.yml" \
  -p 8000:8000 \
  -p 8001:8001 \
  kong-with-ping-authorize
```

## Deploy with Declarative Config (DB-less)

```yaml
_format_version: "3.0"

services:
  - name: my-api
    url: http://upstream-api:8080
    routes:
      - name: my-route
        paths:
          - /api
    plugins:
      - name: idpartners-ping-authorize
        config:
          service_url: "https://pingauthorize.example.com:443"
          shared_secret: "{vault://env/PAZ_SECRET}"
          secret_header_name: "X-Ping-Secret"
          connection_timeout_ms: 5000
          connection_keepalive_ms: 30000
          verify_service_cert: true
          fail_open: false
          max_retries: 2
          retry_backoff_ms: 250
          circuit_breaker_enabled: true
          strip_accept_encoding: true
```

## Configuration Reference

### Required

| Field | Type | Description |
|-------|------|-------------|
| `service_url` | string | PingAuthorize base URL. Do **not** include `/sideband/...` suffix. |
| `shared_secret` | string | Auth secret for PingAuthorize. Supports Kong Vault references. |
| `secret_header_name` | string | Header name under which the shared secret is sent. |

### Optional

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `connection_timeout_ms` | int | 10000 | Connection/read/write timeout in ms. |
| `connection_keepalive_ms` | int | 60000 | Keep-alive duration for connection reuse. |
| `verify_service_cert` | bool | true | Verify PingAuthorize TLS certificate. Set `false` for testing. |
| `skip_response_phase` | bool | false | Skip the `/sideband/response` call entirely. |
| `fail_open` | bool | false | Allow requests through when PingAuthorize is unreachable. |
| `passthrough_status_codes` | []int | [413] | HTTP status codes from PingAuthorize passed through to client. |
| `max_retries` | int | 0 | Retry attempts for failed sideband calls. |
| `retry_backoff_ms` | int | 500 | Fixed delay between retries in ms. |
| `circuit_breaker_enabled` | bool | true | Enable per-instance circuit breaker. |
| `strip_accept_encoding` | bool | true | Remove `Accept-Encoding` header from upstream requests. |
| `include_full_cert_chain` | bool | false | Include full cert chain in `x5c` JWK field. |
| `enable_debug_logging` | bool | false | Log sideband request/response payloads at DEBUG level. |
| `enable_otel` | bool | false | Enable OpenTelemetry traces and metrics. |
| `redact_headers` | []string | [authorization, cookie] | Headers to redact in debug logs. |
| `debug_body_max_bytes` | int | 8192 | Max body size in debug logs. 0 disables truncation. |

### MCP Support (v2.1.0)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enable_mcp` | bool | false | Enable MCP (Model Context Protocol) detection and JSON-RPC enrichment. |
| `mcp_jsonrpc_errors` | bool | false | Return JSON-RPC 2.0 error format for denied MCP requests. |
| `max_sideband_body_bytes` | int | 0 | Max sideband payload size in bytes. 0 = unlimited. Body is truncated when exceeded. |
| `extract_headers` | []string | [] | Headers to extract as top-level `extracted_headers` in sideband payload. |
| `mcp_retry_methods` | []string | [tools/list, resources/list, prompts/list, initialize] | MCP methods safe to retry on failure. Non-listed methods (e.g. `tools/call`) are never retried. |

## MCP Support

When `enable_mcp` is `true`, the plugin detects MCP (Model Context Protocol) JSON-RPC 2.0 traffic and enriches sideband payloads with MCP-specific fields:

- **`traffic_type: "mcp"`** is added to sideband payloads when MCP traffic is detected
- **`mcp` object** containing `mcp_method`, `mcp_tool_name`, `mcp_tool_arguments`, `mcp_resource_uri`, and `mcp_jsonrpc_id`
- **SSE stream parsing** extracts the final JSON-RPC response from `text/event-stream` upstream responses
- **MCP-aware retry** prevents retrying non-idempotent methods like `tools/call` while allowing retry of list operations
- **JSON-RPC error responses** (when `mcp_jsonrpc_errors` is enabled) wraps deny responses in JSON-RPC 2.0 error format

**Supported MCP methods:** `tools/call`, `tools/list`, `resources/read`, `resources/list`, `prompts/get`, `prompts/list`, `initialize`

### Example: MCP Route Configuration

```yaml
plugins:
  - name: idpartners-ping-authorize
    config:
      service_url: "https://pingauthorize.example.com:443"
      shared_secret: "{vault://env/PAZ_SECRET}"
      secret_header_name: "X-Ping-Secret"
      enable_mcp: true
      mcp_jsonrpc_errors: true
      extract_headers:
        - "authorization"
        - "x-session-id"
      mcp_retry_methods:
        - "tools/list"
        - "resources/list"
        - "prompts/list"
        - "initialize"
```

When `mcp_jsonrpc_errors` is enabled, denied MCP requests receive JSON-RPC 2.0 error responses:

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Access denied"}}
```

When `enable_mcp` is `false` (default), no MCP detection or enrichment occurs and the plugin behaves identically to v2.0.0.

## Error Handling

The plugin defaults to **fail-closed**: if PingAuthorize is unreachable, requests are blocked with HTTP 502.

Set `fail_open: true` to allow requests through when PingAuthorize is unavailable. Panics and local errors (bad JSON, bad client certs) always fail-closed regardless of this setting.

| Condition | Status |
|-----------|--------|
| PingAuthorize unreachable (fail-closed) | 502 |
| PingAuthorize unreachable (fail-open) | Request allowed through |
| Circuit breaker open (429 trigger) | 429 with `Retry-After` header |
| Circuit breaker open (5xx/timeout trigger) | 502 |
| Request denied by policy | Status code from PingAuthorize response |
| Unexpected panic | 500 |

## OpenTelemetry

Set the `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable and `enable_otel: true` to emit traces and metrics:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
```

**Traces:** One span per sideband call (`ping-authorize.access`, `ping-authorize.response`).

**Metrics:**
- `ping_authorize_sideband_duration_ms` (histogram)
- `ping_authorize_sideband_total` (counter, labels: phase, result)
- `ping_authorize_circuit_breaker_state` (gauge, 0=closed, 1=open)
- `ping_authorize_policy_decisions_total` (counter, labels: decision)
- `ping_authorize_mcp_requests_total` (counter, labels: mcp_method) — MCP requests only
- `ping_authorize_mcp_denied_total` (counter, labels: mcp_method, reason) — MCP denied requests
- `ping_authorize_mcp_tool_calls_total` (counter, labels: tool_name) — per-tool call tracking

## Debugging

Enable debug logging to see full sideband payloads:

```bash
curl -X PATCH http://localhost:8001/plugins/{plugin_id} \
  --data "config.enable_debug_logging=true"
```

Sensitive headers listed in `redact_headers` (plus `secret_header_name`) are replaced with `[REDACTED]`. Bodies exceeding `debug_body_max_bytes` are truncated.

View logs:

```bash
# Kong traditional
tail -f /usr/local/kong/logs/error.log

# Docker
docker logs -f kong
```

## Migrating from ping-auth (Lua)

This plugin replaces the Lua `ping-auth` plugin. Key differences:

- **Plugin name** changed from `ping-auth` to `idpartners-ping-authorize`
- **Kong 3.x only** (Lua version supported Kong 2.5.x+)
- **HTTP/2 clients accepted** (Lua version rejected HTTP/2 with 400)
- **Configurable fail-open/fail-closed** (Lua was always fail-closed)
- **Configurable response phase skip** via `skip_response_phase`
- **Retry support** via `max_retries` and `retry_backoff_ms`
- **Extended circuit breaker** triggers on 429, 5xx, and timeouts (Lua: 429 only)
- **EC and Ed25519 client certs** supported (Lua: RSA only)
- **No race conditions** -- per-request state uses Kong PDK context, not globals

To migrate, remove the `ping-auth` plugin configuration and add `idpartners-ping-authorize` with the equivalent settings. The `service_url`, `shared_secret`, and `secret_header_name` fields are the same. Other fields from the Lua version map directly, with `verify_service_certificate` renamed to `verify_service_cert` and `enable_debug_logging` kept as-is.
