# Design Interview — 7 February 2026

## Ping-Auth Kong Plugin: Go Re-Implementation

This document captures design decisions from the interview session.

---

### Q1. Kong Gateway Version Target
**Q:** Which Kong Gateway version are you targeting? The Lua plugin targets Kong 2.5.x+. Kong 3.x introduced breaking changes to the PDK and plugin schema. Do you need to support Kong 2.x, Kong 3.x, or both?

**A:** Kong 3.x only — target the latest PDK and drop 2.x compatibility.

---

### Q2. Kong Go PDK Type
**Q:** Are you using the standard Kong Go PDK (external process / socket-based), or Kong's newer embedded Go plugin support?

**A:** Standard Go PDK — external process communicating with Kong via MessagePack over sockets.

---

### Q3. Plugin Deployment Model
**Q:** Should this be a single standalone binary, or are you building within a broader Go plugin ecosystem?

**A:** Standalone binary — single self-contained binary for just this plugin.

---

### Q4. Kong Vault References
**Q:** The Lua schema marks `shared_secret` as `referenceable`, meaning it can reference Kong Vault. Does the Go version need to support Kong Vault references for secrets?

**A:** Yes, required — must support Kong Vault references for production secret management.

---

### Q5. Plugin Scope
**Q:** The Lua version can be applied to a Route, Service, or globally. Are all three scopes required for the Go version?

**A:** All three scopes — support Route, Service, and Global (same as Lua version).

---

### Q6. PingAuthorize vs. Generic Ping Sideband
**Q:** Is the Go re-implementation specifically for PingAuthorize? Are there PingAuthorize-specific behaviors, API versions, or payload fields not covered by the current Lua implementation?

**A:** Initially, this plugin will support only the PingAuthorize Sideband API, but later will want to separately support AuthZen standard APIs. Design with PingAuthorize as the primary target, with future AuthZen support in mind.

---

### Q7. Sideband API Specification
**Q:** Is there a formal version or specification document for the Ping Sideband API protocol? Do you have an OpenAPI/Swagger definition or protocol document to reference?

**A:** PingAuthorize docs only — the PingAuthorize product documentation describes the sideband protocol. No formal OpenAPI/Swagger spec.

---

### Q8. State Field Type
**Q:** The `state` field returned by `/sideband/request` is treated as opaque. In practice with PingAuthorize, is it always a string, always a JSON object, or truly arbitrary?

**A:** Always a JSON object. Use `json.RawMessage` in Go to preserve it without interpretation.

---

### Q9. Policy Decision Types
**Q:** Does PingAuthorize support any other decision types beyond allow and deny that the Go version should handle?

**A:** Additional types exist, but they follow the same response structure as deny with different status codes/bodies. The current allow/deny handling pattern is sufficient — the deny path already passes through arbitrary status codes and bodies from the policy provider.

---

### Q10. Response Phase — Optional?
**Q:** Can the response phase sideband call (`/sideband/response`) be made optional via configuration?

**A:** Make it configurable — add a config option to skip the response phase call when not needed for performance.

---

### Q11. Bug Fixes vs. Backward Compatibility
**Q:** Should the Go version fix all identified bugs or preserve buggy behaviors for compatibility?

**A:** Fix all bugs — this is a clean re-implementation, not a port. Accept behavioral differences.

---

### Q12. HTTP/2 Support
**Q:** Should the Go version continue rejecting HTTP/2 requests, accept them from clients but use HTTP/1.1 for sideband, or support HTTP/2 end-to-end?

**A:** Accept HTTP/2 from clients but always use HTTP/1.1 for sideband API calls to PingAuthorize. Remove the HTTP/2 rejection logic.

---

### Q13. Accept-Encoding Stripping
**Q:** The Lua version unconditionally removes the `Accept-Encoding` header from upstream requests. Is this still required?

**A:** Make it configurable — add a config option to control whether `Accept-Encoding` is stripped, defaulting to the current behavior (strip) for safety.

---

### Q14. Query String Handling
**Q:** Should the Go version preserve the raw query string verbatim, or continue the decode/re-encode behavior with the 100-argument limit?

**A:** Keep decode/re-encode — maintain the current behavior for compatibility with PingAuthorize expectations.

---

### Q15. Circuit Breaker Scope
**Q:** What should be the scope of the circuit breaker?

**A:** Per plugin instance — each Route/Service config gets its own circuit breaker to isolate failures.

---

### Q16. Circuit Breaker Half-Open State
**Q:** Should the Go version implement a half-open state for safer recovery?

**A:** No, keep simple — maintain the OPEN/CLOSED model from the Lua version.

---

### Q17. Circuit Breaker Triggers
**Q:** Should the circuit breaker also trip on connection timeouts or 5xx errors, or remain limited to 429 only?

**A:** Trip on 429, 5xx, and connection/read timeouts. Expand beyond the current 429-only behavior.

---

### Q18. Circuit Breaker Persistence
**Q:** Should circuit breaker state survive process restarts?

**A:** Reset on restart — circuit breaker state is ephemeral (current behavior).

---

### Q19. Fail-Closed vs. Fail-Open
**Q:** Should timeouts have a configurable fallback behavior, or always remain fail-closed?

**A:** Configurable with default closed — add a `fail_open` config option but default to fail-closed. Fail-open requires explicit opt-in.

---

### Q20. Status Code Passthrough
**Q:** Are there other status codes beyond 413 that should pass the policy provider's body through to the client?

**A:** Make the list of passthrough status codes configurable, defaulting to `[413]` for backward compatibility.

---

### Q21. Error Response Bodies
**Q:** Should plugin-generated error responses (500, 502) include structured JSON bodies?

**A:** Empty bodies — continue returning empty bodies as the Lua version does.

---

### Q22. New Configuration Fields
**Q:** Should any new configuration fields be added for the Go version?

**A:** Yes — add:
- `max_retries` / `retry_backoff_ms` — retry support for failed sideband calls
- `circuit_breaker_enabled` — toggle to allow disabling the circuit breaker

(In addition to fields decided in earlier questions: `skip_response_phase`, `fail_open`, `strip_accept_encoding`, `passthrough_status_codes`)

---

### Q23. Timeout Granularity
**Q:** Should the Go version separate the single timeout into connection, read, and write timeouts?

**A:** Keep single timeout — one combined `connection_timeout_ms` as in the current implementation.

---

### Q24. Mutual TLS to Policy Provider
**Q:** Should the Go version support mutual TLS to PingAuthorize (presenting a client certificate)?

**A:** Future enhancement — not needed now, but design the HTTP client so mTLS support can be added later without major refactoring.

---

### Q25. Client Certificate (mTLS) Support
**Q:** Is mTLS/client certificate support required? Will you be running Kong Enterprise?

**A:** Support both — detect the environment and gracefully handle client certs when available (Kong Enterprise), skip silently when not (Kong OSS).

---

### Q26. Certificate Chain Depth
**Q:** Should the Go version include the full certificate chain in `x5c` or just the leaf?

**A:** Configurable — add a config option to control whether to include leaf only or full chain, defaulting to leaf only for backward compatibility.

---

### Q27. Client Certificate Key Types
**Q:** Should the Go version support EC and Ed25519 client certificates in addition to RSA?

**A:** RSA, EC, and Ed25519 — support all common key types for maximum compatibility.

---

### Q28. Structured Logging
**Q:** Should the Go version use structured logging (JSON-formatted entries) or plain text?

**A:** Structured JSON logging — use named fields (e.g., `phase`, `status_code`, `service_url`) for machine-parseable log entries and better log aggregation integration.

---

### Q29. Metrics & Tracing
**Q:** Should the Go version emit metrics or integrate with OpenTelemetry for distributed tracing?

**A:** Both OpenTelemetry traces + metrics AND Kong PDK logging should be supported. Make it configurable — allow enabling/disabling OTel independently from Kong PDK logging.

---

### Q30. Debug Log Redaction
**Q:** Should there be redaction or size limits on debug log output for sensitive data?

**A:** Configurable redaction — add config options for:
- A list of sensitive header names to redact (e.g., `Authorization`, `Cookie`, the secret header)
- A body size limit for log output (truncate large bodies)

---

### Q31. Test Environment
**Q:** Do you have access to a running PingAuthorize instance for integration testing, or should all tests run against mock servers?

**A:** Both — use mock servers for unit/CI tests, with a separate integration test suite against a live PingAuthorize instance. Example payloads can be provided.

---

### Q32. Performance Targets
**Q:** Are there latency or throughput targets for the plugin?

**A:** No specific targets — just be comparable to or better than the Lua version.

---

### Q33. CI/CD Pipeline
**Q:** What CI/CD pipeline should the Go plugin target? Are there organizational Go standards?

**A:** No CI initially — skip CI setup for now, focus on the plugin code. Can be added later.

---

### Q34. Migration Strategy
**Q:** Will the Go and Lua versions need to run side-by-side during migration?

**A:** Full cutover — the Go version fully replaces the Lua version. No side-by-side needed.

---

### Q35. Plugin Name
**Q:** Should the Go version keep the name `ping-auth` or adopt a new name?

**A:** `idpartners-ping-authorize` — new name reflecting the organization and PingAuthorize focus.

---

### Q36. Configuration Compatibility
**Q:** Should the Go version accept the exact same configuration JSON field names as the Lua version, or is a config migration acceptable?

**A:** Clean break OK — free to rename fields to Go/idiomatic conventions. Config migration from the Lua version is acceptable since this is a full cutover with a new plugin name.

---

## Decision Summary

### Platform
| Decision | Answer |
|---|---|
| Kong version | 3.x only |
| Go PDK | Standard (external process, MessagePack) |
| Deployment | Standalone binary |
| Kong Vault | Required for `shared_secret` |
| Plugin scope | Route, Service, Global |
| Plugin name | `idpartners-ping-authorize` |
| Config compatibility | Clean break — idiomatic Go naming |
| Migration | Full cutover, no side-by-side |

### Protocol & Behavior
| Decision | Answer |
|---|---|
| Target product | PingAuthorize (with future AuthZen in mind) |
| API spec source | PingAuthorize product docs |
| State field type | JSON object (`json.RawMessage`) |
| Decision types | Allow/deny pattern sufficient (deny covers all non-allow types) |
| Response phase | Configurable (can be skipped) |
| HTTP/2 | Accept from clients, HTTP/1.1 for sideband |
| Accept-Encoding | Configurable stripping (default: strip) |
| Query strings | Decode/re-encode (current behavior) |
| Bug fixes | Fix all — clean re-implementation |

### Circuit Breaker
| Decision | Answer |
|---|---|
| Scope | Per plugin instance |
| States | OPEN/CLOSED only (no half-open) |
| Triggers | 429, 5xx, and timeouts |
| Persistence | Ephemeral (reset on restart) |

### Error Handling
| Decision | Answer |
|---|---|
| Fail mode | Configurable (default: fail-closed) |
| Status passthrough | Configurable list (default: `[413]`) |
| Error bodies | Empty (current behavior) |

### New Configuration Fields
| Field | Purpose |
|---|---|
| `skip_response_phase` | Disable `/sideband/response` call |
| `fail_open` | Allow requests when policy provider unreachable |
| `strip_accept_encoding` | Control Accept-Encoding removal |
| `passthrough_status_codes` | List of codes to pass through from provider |
| `max_retries` | Retry count for failed sideband calls |
| `retry_backoff_ms` | Backoff between retries |
| `circuit_breaker_enabled` | Toggle circuit breaker on/off |
| `include_full_cert_chain` | Leaf only vs full chain in x5c |
| `redact_headers` | List of header names to redact in debug logs |
| `debug_body_max_size` | Max body size in debug log output |

### Client Certificates
| Decision | Answer |
|---|---|
| mTLS support | Detect and support when available (Kong Enterprise), skip silently on OSS |
| Certificate chain | Configurable (default: leaf only) |
| Key types | RSA, EC, and Ed25519 |
| mTLS to provider | Future enhancement (design for extensibility) |

### Observability
| Decision | Answer |
|---|---|
| Logging format | Structured JSON |
| Metrics/tracing | OpenTelemetry + Kong PDK logging, configurable |
| Debug redaction | Configurable header redaction + body size limit |

### Testing
| Decision | Answer |
|---|---|
| Test approach | Mock servers for unit/CI + live PingAuthorize integration suite |
| Performance | No hard targets, match/exceed Lua version |
| CI/CD | Deferred — focus on plugin code first |
