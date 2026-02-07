# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the `ping-auth` Kong Gateway plugin - a Lua authentication plugin that integrates Kong with Ping Identity products via the Sideband API protocol. It intercepts requests during Kong's access and response phases to consult a Ping policy provider.

## Development Commands

**Installation via LuaRocks:**
```bash
luarocks install kong-plugin-ping-auth
```

**Manual installation (copy to Kong plugins directory):**
```bash
cp -r ping-auth/* /usr/local/share/lua/5.1/kong/plugins/ping-auth/
```

**Enable in Kong** (edit `kong.conf`):
```
plugins = bundled,ping-auth
```

**Enable debug logging:**
- Set `enable_debug_logging = true` in plugin config
- Set `log_level = debug` in `kong.conf`
- View logs: `tail -f /usr/local/kong/logs/error.log`

There is no test framework, Makefile, or build system in this repository. Testing is done by deploying to a Kong instance.

## Architecture

The plugin follows Kong's plugin lifecycle with four Lua modules in `ping-auth/`:

```
handler.lua          Kong entry point (priority 999, version 1.2.0)
    ├── access.lua       Access phase: POST to /sideband/request
    ├── response.lua     Response phase: POST to /sideband/response
    └── network_handler.lua   HTTP client, URL parsing, header formatting
schema.lua           Configuration schema with validation
```

**Request flow:**
1. Client request arrives → Kong access phase
2. `access.lua` builds JSON payload (source IP, headers, body, method, URL, client cert)
3. `network_handler.lua` POSTs to policy provider's `/sideband/request`
4. Policy provider allows/denies/modifies request
5. If allowed, upstream API is called
6. `response.lua` builds response payload with state from access phase
7. `network_handler.lua` POSTs to policy provider's `/sideband/response`
8. Final response sent to client

**Fail-closed design:** Unexpected errors return HTTP 500; failed policy provider calls return HTTP 502.

## Key Configuration Fields (schema.lua)

| Field | Required | Default | Purpose |
|-------|----------|---------|---------|
| `service_url` | Yes | - | Ping policy provider URL (no `/sideband...` suffix) |
| `shared_secret` | Yes | - | Auth secret for policy provider |
| `secret_header_name` | Yes | - | Header name for shared secret |
| `connection_timeout_ms` | No | 10000 | Connection timeout |
| `connection_keepAlive_ms` | No | 60000 | Keep-alive timeout |
| `verify_service_certificate` | No | true | SSL verification (false for testing) |
| `enable_debug_logging` | No | false | Debug-level request/response logging |

## Dependencies

- Kong 2.5.x+ with Kong PDK
- OpenResty (Lua NGINX)
- `resty.http` - HTTP client
- `resty.openssl.x509` - Certificate parsing
- `cjson.safe` - JSON encoding/decoding

## Known Limitations

- HTTP/2 not supported
- `Transfer-Encoding` header not supported (Kong issue #8083)
- mTLS requires Kong Enterprise `mtls-auth` plugin
- Cannot modify: source_ip, source_port, scheme, client_certificate in responses
