package main

import (
	"encoding/json"
	"testing"
)

func TestParseMCPRequest_ToolsCall(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather","arguments":{"city":"London"}}}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "tools/call" {
		t.Errorf("expected method tools/call, got %s", ctx.Method)
	}
	if ctx.ToolName != "get_weather" {
		t.Errorf("expected tool name get_weather, got %s", ctx.ToolName)
	}
	if ctx.ToolArguments == nil {
		t.Fatal("expected tool arguments")
	}
	// Verify arguments preserved as raw JSON
	var args map[string]interface{}
	if err := json.Unmarshal(ctx.ToolArguments, &args); err != nil {
		t.Fatalf("failed to unmarshal tool arguments: %v", err)
	}
	if args["city"] != "London" {
		t.Errorf("expected city London, got %v", args["city"])
	}
	// Verify ID preserved
	if string(ctx.JsonrpcID) != "1" {
		t.Errorf("expected id 1, got %s", string(ctx.JsonrpcID))
	}
}

func TestParseMCPRequest_ToolsCallNoArguments(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_files"}}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.ToolName != "list_files" {
		t.Errorf("expected tool name list_files, got %s", ctx.ToolName)
	}
	if ctx.ToolArguments != nil {
		t.Errorf("expected nil tool arguments, got %s", string(ctx.ToolArguments))
	}
}

func TestParseMCPRequest_ToolsList(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "tools/list" {
		t.Errorf("expected method tools/list, got %s", ctx.Method)
	}
	if ctx.ToolName != "" {
		t.Errorf("expected empty tool name, got %s", ctx.ToolName)
	}
}

func TestParseMCPRequest_ResourcesRead(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"file:///data/config.json"}}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "resources/read" {
		t.Errorf("expected method resources/read, got %s", ctx.Method)
	}
	if ctx.ResourceURI != "file:///data/config.json" {
		t.Errorf("expected resource URI file:///data/config.json, got %s", ctx.ResourceURI)
	}
}

func TestParseMCPRequest_ResourcesList(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "resources/list" {
		t.Errorf("expected method resources/list, got %s", ctx.Method)
	}
	if ctx.ResourceURI != "" {
		t.Errorf("expected empty resource URI, got %s", ctx.ResourceURI)
	}
}

func TestParseMCPRequest_PromptsGet(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":6,"method":"prompts/get","params":{"name":"summarize"}}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "prompts/get" {
		t.Errorf("expected method prompts/get, got %s", ctx.Method)
	}
	if ctx.PromptName != "summarize" {
		t.Errorf("expected prompt name summarize, got %s", ctx.PromptName)
	}
}

func TestParseMCPRequest_PromptsList(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":7,"method":"prompts/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "prompts/list" {
		t.Errorf("expected method prompts/list, got %s", ctx.Method)
	}
}

func TestParseMCPRequest_Initialize(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":8,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{}}}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if ctx.Method != "initialize" {
		t.Errorf("expected method initialize, got %s", ctx.Method)
	}
	if string(ctx.JsonrpcID) != "8" {
		t.Errorf("expected id 8, got %s", string(ctx.JsonrpcID))
	}
}

func TestParseMCPRequest_NonJSON(t *testing.T) {
	body := []byte(`not json at all`)
	ctx := ParseMCPRequest(body)
	if ctx != nil {
		t.Errorf("expected nil for non-JSON body, got %+v", ctx)
	}
}

func TestParseMCPRequest_JSONButNotJsonRPC(t *testing.T) {
	body := []byte(`{"method":"tools/call","params":{"name":"test"}}`)
	ctx := ParseMCPRequest(body)
	if ctx != nil {
		t.Errorf("expected nil for non-JSON-RPC body (missing jsonrpc field), got %+v", ctx)
	}
}

func TestParseMCPRequest_UnrecognizedMethod(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"foo/bar"}`)
	ctx := ParseMCPRequest(body)
	if ctx != nil {
		t.Errorf("expected nil for unrecognized method, got %+v", ctx)
	}
}

func TestParseMCPRequest_Notification(t *testing.T) {
	// JSON-RPC notification has no id field
	body := []byte(`{"jsonrpc":"2.0","method":"tools/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context for notification")
	}
	if ctx.JsonrpcID != nil {
		t.Errorf("expected nil id for notification, got %s", string(ctx.JsonrpcID))
	}
}

func TestParseMCPRequest_IDAsInteger(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":42,"method":"tools/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if string(ctx.JsonrpcID) != "42" {
		t.Errorf("expected id 42, got %s", string(ctx.JsonrpcID))
	}
}

func TestParseMCPRequest_IDAsString(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":"req-001","method":"tools/list"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context")
	}
	if string(ctx.JsonrpcID) != `"req-001"` {
		t.Errorf("expected id \"req-001\", got %s", string(ctx.JsonrpcID))
	}
}

func TestParseMCPRequest_EmptyBody(t *testing.T) {
	ctx := ParseMCPRequest([]byte{})
	if ctx != nil {
		t.Errorf("expected nil for empty body, got %+v", ctx)
	}
}

func TestParseMCPRequest_NilBody(t *testing.T) {
	ctx := ParseMCPRequest(nil)
	if ctx != nil {
		t.Errorf("expected nil for nil body, got %+v", ctx)
	}
}

func TestParseMCPRequest_MalformedParams(t *testing.T) {
	// Params is not a valid JSON object but method is valid
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not an object"}`)
	ctx := ParseMCPRequest(body)
	if ctx == nil {
		t.Fatal("expected MCP context even with malformed params")
	}
	if ctx.Method != "tools/call" {
		t.Errorf("expected method tools/call, got %s", ctx.Method)
	}
	// Fields should be empty since params couldn't be parsed
	if ctx.ToolName != "" {
		t.Errorf("expected empty tool name with malformed params, got %s", ctx.ToolName)
	}
}

func TestIsMCPMethod(t *testing.T) {
	validMethods := []string{"tools/call", "tools/list", "resources/read", "resources/list", "prompts/get", "prompts/list", "initialize"}
	for _, m := range validMethods {
		if !IsMCPMethod(m) {
			t.Errorf("expected %q to be a valid MCP method", m)
		}
	}

	invalidMethods := []string{"foo/bar", "tools", "call", "", "tools/call/extra"}
	for _, m := range invalidMethods {
		if IsMCPMethod(m) {
			t.Errorf("expected %q to NOT be a valid MCP method", m)
		}
	}
}

func TestHttpStatusToJsonRPCError(t *testing.T) {
	tests := []struct {
		httpStatus int
		wantCode   int
	}{
		{400, -32600},
		{401, -32600},
		{403, -32600},
		{404, -32601},
		{429, -32000},
		{500, -32603},
		{502, -32000},
		{503, -32000},
	}

	for _, tt := range tests {
		got := httpStatusToJsonRPCError(tt.httpStatus)
		if got != tt.wantCode {
			t.Errorf("httpStatusToJsonRPCError(%d) = %d, want %d", tt.httpStatus, got, tt.wantCode)
		}
	}
}

func TestFormatMCPDenyResponse(t *testing.T) {
	id := json.RawMessage(`1`)
	body := formatMCPDenyResponse(403, "Access denied", id)

	var resp JsonRPCError
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Jsonrpc != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.Jsonrpc)
	}
	if string(resp.ID) != "1" {
		t.Errorf("expected id 1, got %s", string(resp.ID))
	}
	if resp.Error.Code != -32600 {
		t.Errorf("expected error code -32600, got %d", resp.Error.Code)
	}
	if resp.Error.Message != "Access denied" {
		t.Errorf("expected message 'Access denied', got %s", resp.Error.Message)
	}
}

func TestFormatMCPDenyResponse_StringID(t *testing.T) {
	id := json.RawMessage(`"req-001"`)
	body := formatMCPDenyResponse(500, "Internal error", id)

	var resp JsonRPCError
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if string(resp.ID) != `"req-001"` {
		t.Errorf("expected id \"req-001\", got %s", string(resp.ID))
	}
	if resp.Error.Code != -32603 {
		t.Errorf("expected error code -32603, got %d", resp.Error.Code)
	}
}

func TestFormatMCPDenyResponse_NilID(t *testing.T) {
	body := formatMCPDenyResponse(429, "Rate limited", nil)

	var resp JsonRPCError
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// json.Marshal of nil json.RawMessage produces "null", which is valid JSON-RPC for null id
	if resp.ID != nil && string(resp.ID) != "null" {
		t.Errorf("expected null id, got %s", string(resp.ID))
	}
}

func TestIsMCPMethodRetryable(t *testing.T) {
	retryMethods := []string{"tools/list", "resources/list", "prompts/list", "initialize"}

	if !isMCPMethodRetryable("tools/list", retryMethods) {
		t.Error("expected tools/list to be retryable")
	}
	if !isMCPMethodRetryable("initialize", retryMethods) {
		t.Error("expected initialize to be retryable")
	}
	if isMCPMethodRetryable("tools/call", retryMethods) {
		t.Error("expected tools/call to NOT be retryable")
	}
	if isMCPMethodRetryable("resources/read", retryMethods) {
		t.Error("expected resources/read to NOT be retryable")
	}
	if isMCPMethodRetryable("prompts/get", retryMethods) {
		t.Error("expected prompts/get to NOT be retryable")
	}
}
