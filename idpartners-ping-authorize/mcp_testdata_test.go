package main

// MCP test fixtures for reuse across test files.

// MCP tools/call fixture with tool name and arguments.
var mcpToolsCallBody = []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather","arguments":{"city":"London"}}}`)

// MCP tools/call fixture with no arguments.
var mcpToolsCallNoArgsBody = []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_files"}}`)

// MCP tools/list fixture.
var mcpToolsListBody = []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)

// MCP resources/read fixture with URI.
var mcpResourcesReadBody = []byte(`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"file:///data/config.json"}}`)

// MCP resources/list fixture.
var mcpResourcesListBody = []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)

// MCP prompts/get fixture with name.
var mcpPromptsGetBody = []byte(`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"summarize"}}`)

// MCP prompts/list fixture.
var mcpPromptsListBody = []byte(`{"jsonrpc":"2.0","id":7,"method":"prompts/list"}`)

// MCP initialize fixture with protocol version and capabilities.
var mcpInitializeBody = []byte(`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{"roots":{"listChanged":true}},"clientInfo":{"name":"test-client","version":"1.0.0"}}}`)

// MCP tools/list response with 3 tools (for filtering tests).
var mcpToolsListResponse = []byte(`{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"get_weather","description":"Get weather info"},{"name":"delete_user","description":"Delete a user"},{"name":"list_files","description":"List directory files"}]}}`)

// SSE stream with 3 events, last is JSON-RPC response.
var mcpSSEStream = []byte("data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":0.5}}\n\n" +
	"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progressToken\":1,\"progress\":1.0}}\n\n" +
	"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"The weather in London is sunny.\"}]}}\n\n")

// JSON-RPC error response fixture.
var mcpJsonRPCErrorResponse = []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`)

// Non-MCP JSON body (regular HTTP API).
var nonMCPBody = []byte(`{"action":"get","resource":"user","id":"123"}`)
