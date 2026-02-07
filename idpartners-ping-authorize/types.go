package main

import "encoding/json"

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
