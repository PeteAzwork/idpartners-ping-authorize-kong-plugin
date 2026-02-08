package main

import "encoding/json"

// mcpMethods is the set of recognized MCP JSON-RPC method names.
var mcpMethods = map[string]bool{
	"tools/call":     true,
	"tools/list":     true,
	"resources/read": true,
	"resources/list": true,
	"prompts/get":    true,
	"prompts/list":   true,
	"initialize":     true,
}

// IsMCPMethod returns true if the method is a recognized MCP method.
func IsMCPMethod(method string) bool {
	return mcpMethods[method]
}

// ParseMCPRequest parses a JSON-RPC 2.0 request body and extracts MCP context.
// Returns nil if the body is not a valid JSON-RPC 2.0 request with a recognized MCP method.
func ParseMCPRequest(body []byte) *MCPContext {
	if len(body) == 0 {
		return nil
	}

	var rpc JsonRPCRequest
	if err := json.Unmarshal(body, &rpc); err != nil {
		return nil
	}

	// Must have jsonrpc "2.0" field
	if rpc.Jsonrpc != "2.0" {
		return nil
	}

	// Must be a recognized MCP method
	if !IsMCPMethod(rpc.Method) {
		return nil
	}

	ctx := &MCPContext{
		Method:    rpc.Method,
		JsonrpcID: rpc.ID,
	}

	// Extract method-specific fields from params
	if len(rpc.Params) > 0 {
		extractMCPParams(rpc.Method, rpc.Params, ctx)
	}

	return ctx
}

// extractMCPParams extracts method-specific fields from the JSON-RPC params object.
func extractMCPParams(method string, params json.RawMessage, ctx *MCPContext) {
	var paramsMap map[string]json.RawMessage
	if err := json.Unmarshal(params, &paramsMap); err != nil {
		return
	}

	switch method {
	case "tools/call":
		if name, ok := paramsMap["name"]; ok {
			var toolName string
			if err := json.Unmarshal(name, &toolName); err == nil {
				ctx.ToolName = toolName
			}
		}
		if args, ok := paramsMap["arguments"]; ok {
			ctx.ToolArguments = args
		}

	case "resources/read":
		if uri, ok := paramsMap["uri"]; ok {
			var resourceURI string
			if err := json.Unmarshal(uri, &resourceURI); err == nil {
				ctx.ResourceURI = resourceURI
			}
		}

	case "prompts/get":
		if name, ok := paramsMap["name"]; ok {
			var promptName string
			if err := json.Unmarshal(name, &promptName); err == nil {
				ctx.PromptName = promptName
			}
		}
	}
}

// httpStatusToJsonRPCError maps an HTTP status code to a JSON-RPC 2.0 error code.
func httpStatusToJsonRPCError(statusCode int) int {
	switch statusCode {
	case 400:
		return -32600 // Invalid Request
	case 401, 403:
		return -32600 // Invalid Request (unauthorized)
	case 404:
		return -32601 // Method not found
	case 429:
		return -32000 // Server error (rate limited)
	case 500:
		return -32603 // Internal error
	case 502, 503:
		return -32000 // Server error (unavailable)
	default:
		if statusCode >= 400 && statusCode < 500 {
			return -32600
		}
		return -32603
	}
}

// formatMCPDenyResponse creates a JSON-RPC 2.0 error response body.
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

// isMCPMethodRetryable checks if the given MCP method is in the retryable methods list.
func isMCPMethodRetryable(method string, retryMethods []string) bool {
	for _, m := range retryMethods {
		if m == method {
			return true
		}
	}
	return false
}
