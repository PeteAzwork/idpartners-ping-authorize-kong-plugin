# Design Interview — MCP Server Support for PingAuthorize Sideband Plugin

## MCP + PingAuthorize: Extending idpartners-ping-authorize for MCP Server Evaluations

This document captures design questions for adding MCP (Model Context Protocol) awareness to the `idpartners-ping-authorize` Kong Go plugin, enabling PingAuthorize to make policy decisions about MCP server traffic flowing through Kong Gateway.

### Background

**What is MCP?** The Model Context Protocol (Anthropic, open standard) defines how AI agents connect to external tools and data sources. MCP traffic is JSON-RPC 2.0 over HTTP (Streamable HTTP transport: POST for client-to-server, optional SSE for server-to-client streaming).

**Kong MCP ecosystem (as of February 2026):**
- `ai-mcp-proxy` plugin (Enterprise, Kong 3.12+) — four modes: `passthrough-listener`, `conversion-listener`, `conversion-only`, `listener`
- `ai-mcp-oauth2` plugin (Enterprise, Kong 3.12+, tech preview) — OAuth 2.0 token validation for MCP
- Kong MCP Registry (Konnect, tech preview) — centralized MCP server discovery catalog
- No community/open-source MCP plugins exist

**PingIdentity MCP stance:**
- Published three OAuth 2.0 architecture patterns for securing MCP servers (native, proxy-to-external, agent-redirected)
- PingOne AIC MCP server exists for identity management
- No direct PingAuthorize-to-MCP integration exists today

**The gap:** The current Go plugin sends raw HTTP request/response data to PingAuthorize via the Sideband API. MCP traffic is JSON-RPC 2.0 wrapped in HTTP. PingAuthorize sees opaque HTTP bodies with no awareness of MCP methods, tool names, or JSON-RPC semantics unless the plugin surfaces them.

**MCP authorization specification (2025-06-18):** Protected MCP servers act as OAuth 2.1 resource servers. Clients use RFC 9728 (Protected Resource Metadata) and RFC 8414 (Authorization Server Metadata) for discovery. RFC 8693 Token Exchange supported for resource-specific tokens.

---

### Q1. Plugin Chain Position and Scope

**Q1.1:** Will this plugin run alongside Kong's Enterprise `ai-mcp-proxy` plugin, or is the goal to support MCP traffic on Kong OSS/free tier without that plugin?

**A:** Kong OSS only — support MCP traffic without requiring any Enterprise MCP plugins.

---

**Q1.2:** If running alongside `ai-mcp-proxy`, which execution order is needed? Should PingAuthorize evaluate the raw MCP JSON-RPC request *before* `ai-mcp-proxy` converts it to a REST call, or should it evaluate the *converted* REST request that goes to the upstream?

**A:** N/A — targeting Kong OSS without `ai-mcp-proxy`.

---

**Q1.3:** Should the plugin support all four `ai-mcp-proxy` modes (passthrough-listener, conversion-listener, conversion-only, listener), or only specific modes?

**A:** N/A — targeting Kong OSS without `ai-mcp-proxy`.

---

### Q2. MCP Protocol Awareness in the Sideband Payload

**Q2.1:** Should PingAuthorize receive MCP-specific fields extracted by the plugin (e.g., `mcp_method`, `mcp_tool_name`, `mcp_arguments`), or is the current raw HTTP body sufficient for PingAuthorize policies to parse the JSON-RPC body themselves?

**A:** Extract MCP fields — the plugin should parse the JSON-RPC body and add top-level MCP-specific fields to the sideband payload.

---

**Q2.2:** If MCP-aware fields are needed, which JSON-RPC 2.0 methods should be extracted? The candidates are:

| Method | Purpose | Authorization relevance |
|--------|---------|------------------------|
| `tools/call` | Tool invocation | Primary — which tool, with what arguments |
| `tools/list` | Tool discovery | Should the user see this tool at all? |
| `resources/read` | Resource access | Which resource, by URI |
| `resources/list` | Resource discovery | Which resources are visible? |
| `prompts/get` | Prompt retrieval | Which prompts are accessible? |
| `prompts/list` | Prompt discovery | Which prompts are visible? |
| `initialize` | Session setup | Client capabilities, protocol version |
| `notifications/*` | Server events | Typically not an authorization target |

**A:** All methods — extract fields for `tools/call`, `tools/list`, `resources/read`, `resources/list`, `prompts/get`, `prompts/list`, and `initialize`. Cover the full MCP method surface.

---

**Q2.3:** For `tools/call` requests, should the tool arguments be included in the sideband payload as structured data, or is the tool name alone sufficient for policy decisions?

**A:** Tool name + arguments — include both the tool name and the full arguments object for fine-grained argument-level policy evaluation.

---

**Q2.4:** Should the `mcp_session_id` (from the `Mcp-Session-Id` HTTP header) be extracted and sent as a first-class field in the sideband payload so PingAuthorize can make session-aware decisions?

**A:** No — the session ID is already present in the HTTP headers forwarded in the sideband payload. No need for a separate first-class field.

---

### Q3. PingAuthorize Policy Engine Capabilities

**Q3.1:** Does PingAuthorize's policy engine already have the ability to inspect JSON request bodies and make decisions based on nested fields (e.g., `$.method == "tools/call" && $.params.name == "delete_user"`)? Or does the plugin need to surface these as top-level sideband payload fields?

**A:** PingAuthorize can already inspect JSON bodies and navigate into nested fields for policy evaluation. However, extracted top-level fields are still desirable per Q2.1 for convenience and policy clarity.

---

**Q3.2:** Can PingAuthorize policies differentiate between MCP traffic and regular API traffic today, or does the plugin need to signal the traffic type (e.g., via a `traffic_type: "mcp"` field or a specific header)?

**A:** Plugin should signal it — add a `traffic_type` or similar field so PingAuthorize policies can easily distinguish MCP from REST traffic.

---

**Q3.3:** Does PingAuthorize need to return MCP-specific deny responses in JSON-RPC 2.0 error format (e.g., `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Unauthorized"}}`), or are standard HTTP error responses acceptable for MCP clients?

**A:** Configurable — let the operator choose via config whether to return JSON-RPC 2.0 error format or standard HTTP error responses.

---

### Q4. Streaming and Transport

**Q4.1:** MCP Streamable HTTP responses can be `application/json` (single response) or `text/event-stream` (SSE stream). Can the response phase of the sideband protocol handle SSE streams, or should the response phase be skipped (`skip_response_phase: true`) for MCP traffic?

**A:** Buffer and evaluate — buffer the SSE stream, then send the complete content to PingAuthorize for evaluation in the response phase.

---

**Q4.2:** If the response phase needs to inspect SSE streams, should the plugin buffer the entire stream before sending to PingAuthorize, or send only the final JSON-RPC response message?

**A:** Final JSON-RPC message only — parse the SSE stream, extract the final JSON-RPC response message, and send only that to PingAuthorize.

---

**Q4.3:** Does PingAuthorize have a maximum payload size concern for the sideband request body? MCP tool call arguments and responses can be large (e.g., file contents, database query results).

**A:** Configurable max size — add a config option for maximum sideband body size, and truncate or reject payloads that exceed it.

---

### Q5. Identity and Authentication Context

**Q5.1:** MCP uses OAuth 2.1 for authentication. When Kong's `ai-mcp-oauth2` plugin (or another auth plugin) validates the token, how should the resolved identity be passed to PingAuthorize — via the existing headers in the sideband payload, or as a dedicated identity field?

**A:** Via existing headers — auth plugins inject identity into request headers, which are already forwarded in the sideband payload. No dedicated identity field needed.

---

**Q5.2:** Should the plugin extract specific OAuth claims (e.g., `sub`, `scope`, `client_id`) from validated tokens and include them as first-class fields in the sideband payload?

**A:** Configurable — make it an option. Allow operators to configure which headers (injected by auth plugins) should be extracted into top-level sideband payload fields.

---

**Q5.3:** For MCP clients that use Dynamic Client Registration (DCR), does PingAuthorize need the registered client metadata to make policy decisions?

**A:** Future enhancement — not needed now, but design for extensibility so DCR client metadata support can be added later.

---

### Q6. Tool-Level Authorization

**Q6.1:** Kong's `ai-mcp-proxy` has built-in ACL for MCP tools (allow/deny lists per consumer, added in v3.13). Should PingAuthorize *replace* this ACL (full externalized authorization), *supplement* it (Kong ACL as coarse filter, PingAuthorize as fine-grained policy), or be independent?

**A:**

---

**Q6.2:** Should PingAuthorize be able to *modify* MCP tool call arguments before they reach the MCP server (similar to how it modifies HTTP request headers/body today)? For example: redacting sensitive fields, injecting context, or constraining argument values.

**A:** Yes — PingAuthorize can modify tool call arguments in the sideband response, and the plugin rewrites the JSON-RPC body before forwarding upstream.

---

**Q6.3:** Should PingAuthorize be able to *filter* the `tools/list` response to hide tools that a given identity is not authorized to see?

**A:** Yes — PingAuthorize can remove tools from the `tools/list` response so unauthorized users don't see them.

---

### Q7. Sideband Payload Schema Changes

**Q7.1:** Should the MCP context be an optional extension to the existing `SidebandAccessRequest` struct (additive, backward-compatible), or a separate payload type with its own sideband endpoint?

**A:** Additive extension — add optional MCP fields to the existing `SidebandAccessRequest` struct. Backward-compatible, non-MCP requests are unchanged.

---

**Q7.2:** Proposed additional fields for MCP-aware sideband payloads — which are needed, which are not, and are any missing?

| Field | Type | Source | Purpose |
|-------|------|--------|---------|
| `mcp_method` | string | `$.method` from JSON-RPC body | Policy routing (tools/call vs tools/list) |
| `mcp_tool_name` | string | `$.params.name` for tools/call | Tool-level authorization |
| `mcp_tool_arguments` | object | `$.params.arguments` for tools/call | Argument-level policy |
| `mcp_session_id` | string | `Mcp-Session-Id` HTTP header | Session-aware policies |
| `mcp_jsonrpc_id` | string/int | `$.id` from JSON-RPC body | Correlation for deny responses |
| `mcp_resource_uri` | string | `$.params.uri` for resources/read | Resource-level authorization |
| `traffic_type` | string | Detection logic in plugin | Distinguish MCP from REST traffic |
| `oauth_subject` | string | Upstream header from auth plugin | Identity for policy evaluation |
| `oauth_scopes` | []string | Upstream header from auth plugin | Scope-based policy evaluation |

**A:** Core fields only — include `mcp_method`, `mcp_tool_name`, `mcp_tool_arguments`, `mcp_resource_uri`, `traffic_type`, and `mcp_jsonrpc_id`. Drop `mcp_session_id` (already in headers per Q2.4), `oauth_subject`, and `oauth_scopes` (available via configurable header extraction per Q5.2).

---

**Q7.3:** If PingAuthorize denies an MCP request, should the plugin format the deny response as a JSON-RPC 2.0 error (preserving the original `id` from the request), or return it as a plain HTTP error?

**A:** Configurable — let the operator choose via config whether to return JSON-RPC 2.0 error format or plain HTTP errors (consistent with Q3.3 answer).

---

### Q8. Configuration

**Q8.1:** Should MCP awareness be a single boolean toggle (e.g., `enable_mcp_parsing: true`), or should individual MCP features be independently configurable (e.g., `extract_mcp_method`, `extract_mcp_tool_name`, `format_mcp_deny_response`)?

**A:** Toggle + overrides — one master `enable_mcp` toggle that enables all MCP features together, with optional granular overrides for specific features like deny response format.

---

**Q8.2:** Should the plugin auto-detect MCP traffic (by `Content-Type`, presence of `Mcp-Session-Id` header, or JSON-RPC body structure), or require explicit per-route configuration?

**A:** Explicit per-route — operator must explicitly configure which routes carry MCP traffic (e.g., via `enable_mcp` on specific route plugin instances).

---

**Q8.3:** Are there MCP-specific headers that should be added to the default `redact_headers` list for debug logging?

**A:** No new defaults needed, but the existing `redact_headers` config option should be used — operators can add any MCP-specific headers they want redacted via the existing configurable list.

---

### Q9. Circuit Breaker and Retry Behavior

**Q9.1:** MCP tool calls may be idempotent or non-idempotent. Should the retry configuration distinguish between MCP method types (e.g., retry `tools/list` but not `tools/call`)?

**A:** Configurable retry lists — allow operators to configure which MCP methods are safe to retry via a config list.

---

**Q9.2:** When the circuit breaker is open, should MCP clients receive a JSON-RPC 2.0 error response instead of the current HTTP 429/502 response?

**A:** Follow Q7.3 config — use the same configurable setting (`mcp_jsonrpc_errors`) to determine circuit breaker error format. When enabled, circuit breaker errors are returned as JSON-RPC 2.0 errors; otherwise, plain HTTP 429/502.

---

### Q10. Testing and Validation

**Q10.1:** Is there a PingAuthorize test environment or sandbox with MCP-aware policies available for integration testing?

**A:** No — no MCP-aware PingAuthorize test environment exists yet. Integration testing will rely on mock servers.

---

**Q10.2:** Does Ping have sample PingAuthorize policies for MCP tool authorization that can serve as a reference for expected sideband payload format?

**A:** Will be created — samples don't exist yet but will be created alongside this plugin development.

---

**Q10.3:** Should the integration test suite include mock MCP JSON-RPC payloads (`tools/call`, `tools/list`, `initialize`) to validate correct field extraction and policy evaluation?

**A:** Yes, comprehensive mocks — include mock MCP JSON-RPC payloads for all supported methods (`tools/call`, `tools/list`, `resources/read`, `resources/list`, `prompts/get`, `prompts/list`, `initialize`) in the test suite.

---

## Priority Guidance

The answers to these questions are not all equally impactful. The decisions that most fundamentally shape the implementation are:

| Question | Why it matters |
|----------|---------------|
| **Q2.1** (MCP fields in sideband) | Determines whether this is a small change (pass raw HTTP) or a significant protocol extension (MCP-aware sideband fields) |
| **Q3.1** (PingAuthorize JSON inspection) | If PingAuthorize can already inspect JSON bodies, many proposed fields may be unnecessary |
| **Q7.1** (additive vs separate payload) | Determines backward compatibility and API surface area |
| **Q8.2** (auto-detect vs explicit config) | Determines how the plugin identifies MCP traffic at runtime |
| **Q4.1** (SSE stream handling) | Determines whether the response phase is viable for MCP at all |
| **Q7.3** (JSON-RPC error format) | Determines whether MCP clients get protocol-compliant error responses |

---

## Reference Links

- [MCP Specification — Transports (2025-06-18)](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
- [MCP Specification — Authorization (2025-06-18)](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization)
- [Kong AI MCP Proxy Plugin](https://developer.konghq.com/plugins/ai-mcp-proxy/)
- [Kong AI MCP OAuth2 Plugin](https://developer.konghq.com/plugins/ai-mcp-oauth2/)
- [Kong MCP Traffic Gateway](https://developer.konghq.com/mcp/)
- [PingIdentity — What is MCP](https://developer.pingidentity.com/identity-for-ai/agents/idai-what-is-mcp.html)
- [PingIdentity — Securing MCP Servers](https://developer.pingidentity.com/identity-for-ai/agents/idai-securing-mcp-servers.html)
- [PingAuthorize Sideband API](https://docs.pingidentity.com/pingauthorize/10.3/pingauthorize_server_administration_guide/paz_about_sideband_api.html)
- [Kong Konnect + PingAuthorize Integration](https://docs.pingidentity.com/pingauthorize/11.0/pingauthorize_integrations/paz_configuring_konnect_for_integration.html)
