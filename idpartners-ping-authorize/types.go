package main

import "encoding/json"

// MCPContext holds extracted MCP fields for the sideband payload.
type MCPContext struct {
	Method        string          `json:"mcp_method"`                   // JSON-RPC method (e.g. "tools/call")
	ToolName      string          `json:"mcp_tool_name,omitempty"`      // tools/call: $.params.name
	ToolArguments json.RawMessage `json:"mcp_tool_arguments,omitempty"` // tools/call: $.params.arguments
	ResourceURI   string          `json:"mcp_resource_uri,omitempty"`   // resources/read: $.params.uri
	PromptName    string          `json:"mcp_prompt_name,omitempty"`    // prompts/get: $.params.name
	JsonrpcID     json.RawMessage `json:"mcp_jsonrpc_id,omitempty"`     // $.id (string or int)
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

// JsonRPCErrorDetail holds the code and message for a JSON-RPC error.
type JsonRPCErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SidebandAccessRequest is the payload sent to POST /sideband/request during the access phase.
type SidebandAccessRequest struct {
	SourceIP          string              `json:"source_ip"`
	SourcePort        string              `json:"source_port"`
	Method            string              `json:"method"`
	URL               string              `json:"url"`
	Body              string              `json:"body"`
	Headers           []map[string]string `json:"headers"`
	HTTPVersion       string              `json:"http_version"`
	ClientCertificate *JWK                `json:"client_certificate,omitempty"`
	TrafficType       string              `json:"traffic_type,omitempty"`
	MCP               *MCPContext         `json:"mcp,omitempty"`
	ExtractedHeaders  map[string]string   `json:"extracted_headers,omitempty"`
}

// SidebandAccessResponse is the response from POST /sideband/request.
// If Response is non-nil, the request was denied.
// If Response is nil, the request is allowed and may contain modifications + state.
type SidebandAccessResponse struct {
	SourceIP          string              `json:"source_ip"`
	SourcePort        string              `json:"source_port"`
	Method            string              `json:"method"`
	URL               string              `json:"url"`
	Body              *string             `json:"body"`
	Headers           []map[string]string `json:"headers"`
	ClientCertificate *JWK                `json:"client_certificate,omitempty"`
	State             json.RawMessage     `json:"state,omitempty"`
	Response          *DenyResponse       `json:"response,omitempty"`
}

// DenyResponse represents a denial decision from PingAuthorize.
type DenyResponse struct {
	ResponseCode   string              `json:"response_code"`
	ResponseStatus string              `json:"response_status"`
	Body           string              `json:"body,omitempty"`
	Headers        []map[string]string `json:"headers,omitempty"`
}

// SidebandResponsePayload is the payload sent to POST /sideband/response during the response phase.
type SidebandResponsePayload struct {
	Method         string                 `json:"method"`
	URL            string                 `json:"url"`
	Body           string                 `json:"body"`
	ResponseCode   string                 `json:"response_code"`
	ResponseStatus string                 `json:"response_status"`
	Headers        []map[string]string    `json:"headers"`
	HTTPVersion    string                 `json:"http_version"`
	State          json.RawMessage        `json:"state,omitempty"`
	Request        *SidebandAccessRequest `json:"request,omitempty"`
	TrafficType    string                 `json:"traffic_type,omitempty"`
	MCP            *MCPContext            `json:"mcp,omitempty"`
}

// SidebandResponseResult is the response from POST /sideband/response.
type SidebandResponseResult struct {
	ResponseCode string              `json:"response_code"`
	Body         string              `json:"body,omitempty"`
	Headers      []map[string]string `json:"headers"`
	Message      string              `json:"message,omitempty"`
	ID           string              `json:"id,omitempty"`
}

// SidebandErrorResponse is used to parse error responses from PingAuthorize.
type SidebandErrorResponse struct {
	Message string `json:"message,omitempty"`
	ID      string `json:"id,omitempty"`
}

// ParsedURL holds a parsed URL broken into its components.
type ParsedURL struct {
	Scheme string
	Host   string
	Port   int
	Path   string
	Query  string
}

// JWK represents a JSON Web Key for client certificate public keys.
type JWK struct {
	Kty string   `json:"kty"`
	N   string   `json:"n,omitempty"`   // RSA modulus
	E   string   `json:"e,omitempty"`   // RSA exponent
	Crv string   `json:"crv,omitempty"` // EC curve / Ed25519
	X   string   `json:"x,omitempty"`   // EC x-coordinate / Ed25519 public key
	Y   string   `json:"y,omitempty"`   // EC y-coordinate
	X5C []string `json:"x5c"`           // Certificate chain (base64 DER)
}
