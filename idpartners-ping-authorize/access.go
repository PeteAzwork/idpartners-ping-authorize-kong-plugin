package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Kong/go-pdk"
)

// executeAccess implements the access phase logic.
func executeAccess(kong *pdk.PDK, conf *Config) {
	logger := NewPluginLogger(kong, "access", conf.ServiceURL)

	parsedURL, err := ParseURL(conf.ServiceURL)
	if err != nil {
		logger.Err("Failed to parse service URL", "error", err.Error())
		kong.Response.Exit(500, nil, nil)
		return
	}

	payload, err := composeAccessPayload(kong, conf, parsedURL)
	if err != nil {
		logger.Err("Failed to compose access payload", "error", err.Error())
		kong.Response.Exit(400, nil, nil)
		return
	}

	DebugLogPayload(logger, "Sending sideband request", payload, conf)

	httpClient := conf.getHTTPClient()
	provider := NewSidebandProvider(conf, httpClient, parsedURL)

	resp, err := provider.EvaluateRequest(context.Background(), payload)
	if err != nil {
		// Check if it's a circuit breaker error
		if cbErr, ok := err.(*CircuitBreakerOpenError); ok {
			handleCircuitBreakerError(kong, cbErr, conf)
			return
		}

		// Check if it's a sideband HTTP error with passthrough status code
		if httpErr, ok := err.(*sidebandHTTPError); ok {
			if isPassthroughCode(httpErr.StatusCode, conf) {
				kong.Response.Exit(httpErr.StatusCode, httpErr.Body,
					map[string][]string{"Content-Type": {"application/json"}})
				return
			}
			logger.Warn("Sideband request failed", "status", httpErr.StatusCode, "message", httpErr.Message, "id", httpErr.ID)
		} else {
			logger.Err("PingAuthorize unreachable", "error", err.Error())
		}

		if conf.FailOpen {
			logger.Warn("PingAuthorize unreachable, fail-open enabled, allowing request")
			storePerRequestContext(kong, payload, nil)
			return
		}
		kong.Response.Exit(502, nil, nil)
		return
	}

	DebugLogPayload(logger, "Received sideband response", resp, conf)

	state, err := handleAccessResponse(kong, conf, resp, logger)
	if err != nil {
		// handleAccessResponse already sent a response to the client
		return
	}

	storePerRequestContext(kong, payload, state)
}

// composeAccessPayload builds the JSON payload for the /sideband/request call.
func composeAccessPayload(kong *pdk.PDK, conf *Config, parsedURL *ParsedURL) (*SidebandAccessRequest, error) {
	sourceIP, err := kong.Client.GetIp()
	if err != nil {
		return nil, fmt.Errorf("failed to get client IP: %w", err)
	}

	sourcePort, err := kong.Client.GetPort()
	if err != nil {
		return nil, fmt.Errorf("failed to get client port: %w", err)
	}

	method, err := kong.Request.GetMethod()
	if err != nil {
		return nil, fmt.Errorf("failed to get method: %w", err)
	}

	// Reconstruct forwarded URL
	reqURL, err := buildForwardedURL(kong)
	if err != nil {
		return nil, fmt.Errorf("failed to build URL: %w", err)
	}

	rawBody, err := kong.Request.GetRawBody()
	if err != nil {
		return nil, fmt.Errorf("failed to get request body: %w", err)
	}

	headers, err := kong.Request.GetHeaders(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to get headers: %w", err)
	}

	formattedHeaders, err := FormatHeaders(headers)
	if err != nil {
		return nil, err
	}

	httpVersion, err := getHTTPVersion(kong)
	if err != nil {
		return nil, fmt.Errorf("failed to get HTTP version: %w", err)
	}

	req := &SidebandAccessRequest{
		SourceIP:    sourceIP,
		SourcePort:  strconv.Itoa(sourcePort),
		Method:      method,
		URL:         reqURL,
		Body:        string(rawBody),
		Headers:     formattedHeaders,
		HTTPVersion: httpVersion,
	}

	// Try to extract client certificate (optional, fails silently on Kong OSS)
	certPEM, err := getClientCertPEM(kong)
	if err == nil && certPEM != "" {
		jwk, err := ExtractClientCertJWK(certPEM, conf.IncludeFullCertChain)
		if err != nil {
			return nil, fmt.Errorf("failed to extract client certificate JWK: %w", err)
		}
		req.ClientCertificate = jwk
	}

	return req, nil
}

// buildForwardedURL reconstructs the full forwarded URL.
func buildForwardedURL(kong *pdk.PDK) (string, error) {
	scheme, err := kong.Request.GetForwardedScheme()
	if err != nil {
		return "", err
	}

	host, err := kong.Request.GetForwardedHost()
	if err != nil {
		return "", err
	}

	port, err := kong.Request.GetForwardedPort()
	if err != nil {
		return "", err
	}

	// Use GetPath since GetForwardedPath doesn't exist in the PDK
	path, err := kong.Request.GetPath()
	if err != nil {
		return "", err
	}

	reqURL := fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)

	// Decode and re-encode query string (max 100 args)
	rawQuery, err := kong.Request.GetRawQuery()
	if err != nil {
		return "", err
	}

	if rawQuery != "" {
		parsedQuery, err := url.ParseQuery(rawQuery)
		if err == nil {
			// Limit to 100 args
			count := 0
			limitedQuery := url.Values{}
			for key, values := range parsedQuery {
				for _, v := range values {
					if count >= 100 {
						break
					}
					limitedQuery.Add(key, v)
					count++
				}
				if count >= 100 {
					break
				}
			}
			encodedQuery := limitedQuery.Encode()
			if encodedQuery != "" {
				reqURL = reqURL + "?" + encodedQuery
			}
		} else {
			// If query parsing fails, use raw query as-is
			reqURL = reqURL + "?" + rawQuery
		}
	}

	return reqURL, nil
}

// getHTTPVersion returns the HTTP version as a string (e.g., "1.1", "2").
func getHTTPVersion(kong *pdk.PDK) (string, error) {
	version, err := kong.Request.GetHttpVersion()
	if err != nil {
		return "1.1", nil // default to 1.1
	}

	// GetHttpVersion returns a float64 (e.g., 1.1, 2.0)
	if version == 2.0 {
		return "2", nil
	}
	return fmt.Sprintf("%g", version), nil
}

// getClientCertPEM attempts to get the client certificate PEM from Kong.
func getClientCertPEM(kong *pdk.PDK) (string, error) {
	certPEM, err := kong.Nginx.GetVar("ssl_client_raw_cert")
	if err != nil {
		return "", err
	}
	return certPEM, nil
}

// handleAccessResponse processes the response from /sideband/request.
// Returns the state (may be nil) and any error.
// If the request is denied, it calls kong.Response.Exit and returns an error.
func handleAccessResponse(kong *pdk.PDK, conf *Config, resp *SidebandAccessResponse, logger *PluginLogger) (json.RawMessage, error) {
	// If response field is present → DENIED
	if resp.Response != nil {
		deny := resp.Response
		statusCode, err := strconv.Atoi(deny.ResponseCode)
		if err != nil {
			statusCode = 403
		}

		headers := FlattenHeaders(deny.Headers)
		logger.Info("Request denied by policy provider", "status_code", statusCode)

		kong.Response.Exit(statusCode, []byte(deny.Body), headers)
		return nil, fmt.Errorf("request denied with status %d", statusCode)
	}

	// ALLOWED — apply modifications
	updateRequest(kong, conf, resp, logger)

	return resp.State, nil
}

// updateRequest applies PingAuthorize modifications to the Kong request.
func updateRequest(kong *pdk.PDK, conf *Config, resp *SidebandAccessResponse, logger *PluginLogger) {
	// Get current request headers for diffing
	currentHeaders, err := kong.Request.GetHeaders(-1)
	if err != nil {
		logger.Warn("Failed to get current headers for diffing", "error", err.Error())
		return
	}

	// Lowercase all current header names for comparison
	currentFlat := make(map[string][]string)
	for name, values := range currentHeaders {
		currentFlat[strings.ToLower(name)] = values
	}

	// Flatten response headers
	newFlat := FlattenHeaders(resp.Headers)

	// Remove headers that are in current but not in response
	for name := range currentFlat {
		if _, exists := newFlat[name]; !exists {
			kong.ServiceRequest.ClearHeader(name)
		}
	}

	// Update/add headers from response
	for name, values := range newFlat {
		currentValues, exists := currentFlat[name]
		if !exists || !stringSliceEqual(currentValues, values) {
			kong.ServiceRequest.SetHeader(name, values[0])
			for _, v := range values[1:] {
				kong.ServiceRequest.AddHeader(name, v)
			}
		}
	}

	// Strip Accept-Encoding if configured
	if conf.StripAcceptEncoding {
		kong.ServiceRequest.ClearHeader("Accept-Encoding")
	}

	// Update method if changed
	if resp.Method != "" {
		currentMethod, _ := kong.Request.GetMethod()
		if resp.Method != currentMethod {
			kong.ServiceRequest.SetMethod(resp.Method)
		}
	}

	// Update URL if changed
	if resp.URL != "" {
		currentURL, _ := buildForwardedURL(kong)
		if resp.URL != currentURL {
			updateURL(kong, resp.URL, currentURL, logger)
		}
	}

	// Update body if changed
	if resp.Body != nil {
		currentBody, _ := kong.Request.GetRawBody()
		if *resp.Body != string(currentBody) {
			kong.ServiceRequest.SetRawBody(*resp.Body)
		}
	}
}

// updateURL applies URL modifications from PingAuthorize.
func updateURL(kong *pdk.PDK, newURL, currentURL string, logger *PluginLogger) {
	newParsed, err := url.Parse(newURL)
	if err != nil {
		logger.Warn("Failed to parse new URL", "url", newURL, "error", err.Error())
		return
	}

	currentParsed, err := url.Parse(currentURL)
	if err != nil {
		logger.Warn("Failed to parse current URL", "url", currentURL, "error", err.Error())
		return
	}

	// Warn about unsupported scheme change
	if newParsed.Scheme != currentParsed.Scheme {
		logger.Warn("Scheme change not supported", "from", currentParsed.Scheme, "to", newParsed.Scheme)
	}

	// If host or port changed, update Host header
	if newParsed.Host != currentParsed.Host {
		kong.ServiceRequest.SetHeader("Host", newParsed.Host)
	}

	// If path changed
	if newParsed.Path != currentParsed.Path {
		kong.ServiceRequest.SetPath(newParsed.Path)
	}

	// If query changed
	if newParsed.RawQuery != currentParsed.RawQuery {
		kong.ServiceRequest.SetRawQuery(newParsed.RawQuery)
	}
}

// storePerRequestContext stores the original request and state in Kong's per-request context.
func storePerRequestContext(kong *pdk.PDK, originalRequest *SidebandAccessRequest, state json.RawMessage) {
	reqJSON, err := json.Marshal(originalRequest)
	if err == nil {
		kong.Ctx.SetShared("paz_original_request", string(reqJSON))
	}
	if state != nil {
		kong.Ctx.SetShared("paz_state", string(state))
	}
}

// handleCircuitBreakerError sends the appropriate response when the circuit breaker is open.
func handleCircuitBreakerError(kong *pdk.PDK, cbErr *CircuitBreakerOpenError, conf *Config) {
	if cbErr.Trigger == Trigger429 {
		remainingSec := (cbErr.RemainingMs + 999) / 1000 // round up
		if remainingSec < 1 {
			remainingSec = 1
		}
		body := fmt.Sprintf(`{"code":"LIMIT_EXCEEDED","message":"The request exceeded the allowed rate limit. Please try after %d second."}`, remainingSec)
		kong.Response.Exit(429, []byte(body), map[string][]string{
			"Content-Type": {"application/json"},
			"Retry-After":  {strconv.FormatInt(remainingSec, 10)},
		})
		return
	}

	// 5xx/timeout trigger
	if conf.FailOpen {
		return // allow through
	}
	kong.Response.Exit(502, nil, nil)
}

// isPassthroughCode checks if a status code is in the passthrough list.
func isPassthroughCode(code int, conf *Config) bool {
	for _, c := range conf.PassthroughStatusCodes {
		if c == code {
			return true
		}
	}
	return false
}

// stringSliceEqual checks if two string slices are equal (order-sensitive).
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
