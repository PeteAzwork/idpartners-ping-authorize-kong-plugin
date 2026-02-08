package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Kong/go-pdk"
)

// Status code to status string mapping per DESIGN.md §4.3.2
var statusStrings = map[int]string{
	200: "OK",
	400: "BAD REQUEST",
	401: "UNAUTHORIZED",
	404: "NOT FOUND",
	413: "PAYLOAD TOO LARGE",
	429: "TOO MANY REQUESTS",
	500: "INTERNAL SERVER ERROR",
	503: "SERVICE UNAVAILABLE",
}

// Headers that are always preserved from the upstream response, even if not in PingAuthorize's response.
var preservedResponseHeaders = map[string]bool{
	"content-length": true,
	"date":           true,
	"connection":     true,
	"vary":           true,
}

// getStatusString returns the human-readable status text for a status code.
func getStatusString(code int) string {
	if s, ok := statusStrings[code]; ok {
		return s
	}
	return ""
}

// executeResponse implements the response phase logic.
func executeResponse(kong *pdk.PDK, conf *Config) {
	logger := NewPluginLogger(kong, "response", conf.ServiceURL)

	parsedURL, err := ParseURL(conf.ServiceURL)
	if err != nil {
		logger.Err("Failed to parse service URL", "error", err.Error())
		kong.Response.Exit(500, nil, nil)
		return
	}

	originalRequest, state, err := loadPerRequestContext(kong)
	if err != nil {
		logger.Err("Failed to load per-request context", "error", err.Error())
		kong.Response.Exit(500, nil, nil)
		return
	}

	payload, err := composeResponsePayload(kong, conf, originalRequest, state, parsedURL)
	if err != nil {
		logger.Err("Failed to compose response payload", "error", err.Error())
		kong.Response.Exit(500, nil, nil)
		return
	}

	DebugLogPayload(logger, "Sending sideband response", payload, conf)

	httpClient := conf.getHTTPClient()
	provider := NewSidebandProvider(conf, httpClient, parsedURL)

	result, err := provider.EvaluateResponse(context.Background(), payload)
	if err != nil {
		// Check circuit breaker error
		if cbErr, ok := err.(*CircuitBreakerOpenError); ok {
			handleCircuitBreakerErrorResponse(kong, cbErr, conf, originalRequest)
			return
		}

		// Check passthrough
		if httpErr, ok := err.(*sidebandHTTPError); ok {
			if isPassthroughCode(httpErr.StatusCode, conf) {
				kong.Response.Exit(httpErr.StatusCode, httpErr.Body,
					map[string][]string{"Content-Type": {"application/json"}})
				return
			}
			logger.Warn("Sideband response failed", "status", httpErr.StatusCode, "message", httpErr.Message, "id", httpErr.ID)
		} else {
			logger.Err("PingAuthorize unreachable during response phase", "error", err.Error())
		}

		if conf.FailOpen {
			logger.Warn("PingAuthorize unreachable during response phase, fail-open, passing upstream response through")
			return // pass upstream response through unmodified
		}
		kong.Response.Exit(502, nil, nil)
		return
	}

	DebugLogPayload(logger, "Received sideband response result", result, conf)

	handleResponseResult(kong, conf, result, logger)
}

// composeResponsePayload builds the JSON payload for the /sideband/response call.
func composeResponsePayload(kong *pdk.PDK, conf *Config, originalRequest *SidebandAccessRequest, state json.RawMessage, parsedURL *ParsedURL) (*SidebandResponsePayload, error) {
	method, err := kong.Request.GetMethod()
	if err != nil {
		return nil, fmt.Errorf("failed to get method: %w", err)
	}

	reqURL, err := buildForwardedURL(kong)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}

	// Get upstream response body (returns []byte)
	responseBodyBytes, err := kong.ServiceResponse.GetRawBody()
	if err != nil {
		return nil, fmt.Errorf("failed to get response body: %w", err)
	}

	// Get upstream response status code
	statusCode, err := kong.ServiceResponse.GetStatus()
	if err != nil {
		return nil, fmt.Errorf("failed to get response status: %w", err)
	}

	// Get upstream response headers (returns map[string][]string)
	responseHeaders, err := kong.ServiceResponse.GetHeaders(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to get response headers: %w", err)
	}

	formattedHeaders, err := FormatHeaders(responseHeaders)
	if err != nil {
		return nil, err
	}

	httpVersion, err := getHTTPVersion(kong)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP version: %w", err)
	}

	responseBody := string(responseBodyBytes)

	// MCP: SSE stream parsing — extract final JSON-RPC message
	if conf.EnableMCP {
		contentType := getResponseContentType(responseHeaders)
		if isSSEContentType(contentType) {
			finalMsg := ParseSSEFinalMessage(responseBodyBytes, contentType)
			responseBody = string(finalMsg)
		}
	}

	payload := &SidebandResponsePayload{
		Method:         method,
		URL:            reqURL,
		Body:           responseBody,
		ResponseCode:   strconv.Itoa(statusCode),
		ResponseStatus: getStatusString(statusCode),
		Headers:        formattedHeaders,
		HTTPVersion:    httpVersion,
	}

	// state and request are mutually exclusive
	if len(state) > 0 {
		payload.State = state
	} else if originalRequest != nil {
		payload.Request = originalRequest
	}

	// MCP: add MCP context to response payload
	if conf.EnableMCP {
		// Try to parse the response body itself for MCP context
		mcpCtx := ParseMCPRequest([]byte(responseBody))
		if mcpCtx != nil {
			payload.TrafficType = "mcp"
			payload.MCP = mcpCtx
		} else if originalRequest != nil && originalRequest.MCP != nil {
			// Carry forward MCP context from the original request
			payload.TrafficType = "mcp"
			payload.MCP = originalRequest.MCP
		}
	}

	return payload, nil
}

// getResponseContentType extracts the Content-Type header value from response headers.
func getResponseContentType(headers map[string][]string) string {
	for name, values := range headers {
		if strings.EqualFold(name, "content-type") && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// handleResponseResult processes the response from /sideband/response.
func handleResponseResult(kong *pdk.PDK, conf *Config, result *SidebandResponseResult, logger *PluginLogger) {
	statusCode, err := strconv.Atoi(result.ResponseCode)
	if err != nil {
		statusCode = 200
	}

	// Flatten response headers from PingAuthorize
	policyHeaders := FlattenHeaders(result.Headers)

	// Get current upstream response headers to remove those not in policy response
	upstreamHeaders, err := kong.ServiceResponse.GetHeaders(-1)
	if err == nil {
		for name := range upstreamHeaders {
			lowerName := strings.ToLower(name)
			if preservedResponseHeaders[lowerName] {
				continue
			}
			if _, inPolicy := policyHeaders[lowerName]; !inPolicy {
				// Header is in upstream but not in policy response — it will be excluded
				// since we're building a complete new response via kong.Response.Exit
			}
		}
	}

	logger.Info("Response phase complete", "status_code", statusCode)

	kong.Response.Exit(statusCode, []byte(result.Body), policyHeaders)
}

// loadPerRequestContext retrieves the original request, state, and MCP context from Kong's per-request context.
func loadPerRequestContext(kong *pdk.PDK) (*SidebandAccessRequest, json.RawMessage, error) {
	reqStr, err := kong.Ctx.GetSharedString("paz_original_request")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get original request from context: %w", err)
	}

	var req SidebandAccessRequest
	if reqStr != "" {
		if err := json.Unmarshal([]byte(reqStr), &req); err != nil {
			return nil, nil, fmt.Errorf("failed to unmarshal original request: %w", err)
		}
	}

	// Restore MCP context if stored
	mcpStr, mcpErr := kong.Ctx.GetSharedString(mcpContextKey)
	if mcpErr == nil && mcpStr != "" {
		var mcpCtx MCPContext
		if err := json.Unmarshal([]byte(mcpStr), &mcpCtx); err == nil {
			req.MCP = &mcpCtx
			req.TrafficType = "mcp"
		}
	}

	stateStr, err := kong.Ctx.GetSharedString("paz_state")
	var state json.RawMessage
	if err == nil && stateStr != "" {
		state = json.RawMessage(stateStr)
	}

	return &req, state, nil
}

// handleCircuitBreakerErrorResponse handles circuit breaker errors in the response phase.
func handleCircuitBreakerErrorResponse(kong *pdk.PDK, cbErr *CircuitBreakerOpenError, conf *Config, originalRequest *SidebandAccessRequest) {
	if cbErr.Trigger == Trigger429 {
		remainingSec := (cbErr.RemainingMs + 999) / 1000
		if remainingSec < 1 {
			remainingSec = 1
		}

		// JSON-RPC error format for MCP traffic
		if conf.MCPJsonrpcErrors && originalRequest != nil && originalRequest.MCP != nil {
			msg := fmt.Sprintf("Service temporarily unavailable. Retry after %d seconds.", remainingSec)
			body := formatMCPDenyResponse(429, msg, originalRequest.MCP.JsonrpcID)
			kong.Response.Exit(429, body, map[string][]string{
				"Content-Type": {"application/json"},
				"Retry-After":  {strconv.FormatInt(remainingSec, 10)},
			})
			return
		}

		body := fmt.Sprintf(`{"code":"LIMIT_EXCEEDED","message":"The request exceeded the allowed rate limit. Please try after %d second."}`, remainingSec)
		kong.Response.Exit(429, []byte(body), map[string][]string{
			"Content-Type": {"application/json"},
			"Retry-After":  {strconv.FormatInt(remainingSec, 10)},
		})
		return
	}

	if conf.FailOpen {
		return // pass upstream response through
	}

	// JSON-RPC error format for MCP traffic
	if conf.MCPJsonrpcErrors && originalRequest != nil && originalRequest.MCP != nil {
		body := formatMCPDenyResponse(502, "Service temporarily unavailable.", originalRequest.MCP.JsonrpcID)
		kong.Response.Exit(502, body, map[string][]string{
			"Content-Type": {"application/json"},
		})
		return
	}
	kong.Response.Exit(502, nil, nil)
}
