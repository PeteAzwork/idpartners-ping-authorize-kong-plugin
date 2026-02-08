package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// SidebandProvider implements PolicyProvider using the PingAuthorize Sideband API.
type SidebandProvider struct {
	httpClient *SidebandHTTPClient
	config     *Config
	parsedURL  *ParsedURL
}

// NewSidebandProvider creates a new SidebandProvider.
func NewSidebandProvider(config *Config, httpClient *SidebandHTTPClient, parsedURL *ParsedURL) *SidebandProvider {
	return &SidebandProvider{
		httpClient: httpClient,
		config:     config,
		parsedURL:  parsedURL,
	}
}

// EvaluateRequest sends the access phase payload to /sideband/request and returns the parsed response.
func (p *SidebandProvider) EvaluateRequest(ctx context.Context, req *SidebandAccessRequest) (*SidebandAccessResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode access request: %w", err)
	}

	// Extract MCP method for retry awareness
	mcpMethod := ""
	if req.MCP != nil {
		mcpMethod = req.MCP.Method
	}

	requestURL := BuildSidebandURL(p.parsedURL, "/sideband/request")
	statusCode, _, respBody, err := p.httpClient.Execute(ctx, requestURL, body, p.parsedURL, mcpMethod)
	if err != nil {
		return nil, err
	}

	// Check for failed request (4xx/5xx from PingAuthorize)
	if statusCode >= 400 {
		var errResp SidebandErrorResponse
		json.Unmarshal(respBody, &errResp)
		return nil, &sidebandHTTPError{
			StatusCode: statusCode,
			Body:       respBody,
			Message:    errResp.Message,
			ID:         errResp.ID,
		}
	}

	var resp SidebandAccessResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode access response: %w", err)
	}

	return &resp, nil
}

// EvaluateResponse sends the response phase payload to /sideband/response and returns the parsed result.
func (p *SidebandProvider) EvaluateResponse(ctx context.Context, req *SidebandResponsePayload) (*SidebandResponseResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to encode response payload: %w", err)
	}

	// Extract MCP method for retry awareness
	mcpMethod := ""
	if req.MCP != nil {
		mcpMethod = req.MCP.Method
	}

	requestURL := BuildSidebandURL(p.parsedURL, "/sideband/response")
	statusCode, _, respBody, err := p.httpClient.Execute(ctx, requestURL, body, p.parsedURL, mcpMethod)
	if err != nil {
		return nil, err
	}

	// Check for failed request (4xx/5xx from PingAuthorize)
	if statusCode >= 400 {
		var errResp SidebandErrorResponse
		json.Unmarshal(respBody, &errResp)
		return nil, &sidebandHTTPError{
			StatusCode: statusCode,
			Body:       respBody,
			Message:    errResp.Message,
			ID:         errResp.ID,
		}
	}

	var result SidebandResponseResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response result: %w", err)
	}

	return &result, nil
}

// sidebandHTTPError represents an HTTP error response from PingAuthorize.
type sidebandHTTPError struct {
	StatusCode int
	Body       []byte
	Message    string
	ID         string
}

func (e *sidebandHTTPError) Error() string {
	return fmt.Sprintf("sideband request failed with status %d: %s", e.StatusCode, e.Message)
}
