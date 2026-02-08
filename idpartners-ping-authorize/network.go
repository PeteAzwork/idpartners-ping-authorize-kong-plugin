package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// SidebandHTTPClient wraps an HTTP client with retry and circuit breaker support.
type SidebandHTTPClient struct {
	client *http.Client
	cb     *CircuitBreaker
	config *Config
}

// NewSidebandHTTPClient creates a new HTTP client configured for sideband communication.
func NewSidebandHTTPClient(config *Config) *SidebandHTTPClient {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !config.VerifyServiceCert,
		},
		IdleConnTimeout:     time.Duration(config.ConnectionKeepaliveMs) * time.Millisecond,
		MaxIdleConnsPerHost: 10,
		ForceAttemptHTTP2:   false,
	}

	client := &http.Client{
		Timeout:   time.Duration(config.ConnectionTimeoutMs) * time.Millisecond,
		Transport: transport,
	}

	cb := NewCircuitBreaker(config.CircuitBreakerEnabled)

	return &SidebandHTTPClient{
		client: client,
		cb:     cb,
		config: config,
	}
}

// Execute sends a POST request to the given path with the provided JSON body.
// It checks the circuit breaker, applies retries, and trips the breaker on final failure.
// mcpMethod is the MCP method name (e.g. "tools/call") for retry awareness, or empty for non-MCP requests.
// Returns the response status code, headers, body, and any error.
func (c *SidebandHTTPClient) Execute(ctx context.Context, requestURL string, body []byte, parsedURL *ParsedURL, mcpMethod string) (int, http.Header, []byte, error) {
	// Check circuit breaker
	ok, cbErr := c.cb.Allow()
	if !ok {
		return 0, nil, nil, cbErr
	}

	var lastErr error
	var lastStatus int
	var lastHeaders http.Header
	var lastBody []byte

	maxAttempts := 1 + c.config.MaxRetries

	// MCP-aware retry: non-retryable MCP methods get only 1 attempt
	if mcpMethod != "" && !isMCPMethodRetryable(mcpMethod, c.config.MCPRetryMethods) {
		maxAttempts = 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(c.config.RetryBackoffMs) * time.Millisecond)
		}

		statusCode, respHeaders, respBody, err := c.doRequest(ctx, requestURL, body, parsedURL)

		if err != nil {
			lastErr = err
			lastStatus = 0
			lastHeaders = nil
			lastBody = nil
			continue // retry on connection errors
		}

		// HTTP 429 — do NOT retry, trip circuit breaker immediately
		if statusCode == 429 {
			retryAfter := parseRetryAfter(respHeaders)
			c.cb.Trip(Trigger429, retryAfter)
			return statusCode, respHeaders, respBody, nil
		}

		// 5xx — retry
		if statusCode >= 500 {
			lastErr = fmt.Errorf("sideband returned %d", statusCode)
			lastStatus = statusCode
			lastHeaders = respHeaders
			lastBody = respBody
			continue
		}

		// Success or 4xx — no retry
		return statusCode, respHeaders, respBody, nil
	}

	// All retries exhausted
	if lastErr != nil {
		// Trip circuit breaker on connection failure or 5xx
		if lastStatus >= 500 {
			c.cb.Trip(Trigger5xx, defaultRetryAfterSec)
		} else if lastStatus == 0 {
			// Connection error/timeout
			c.cb.Trip(TriggerTimeout, defaultRetryAfterSec)
		}
	}

	// If we had an HTTP response (5xx), return it
	if lastStatus > 0 {
		return lastStatus, lastHeaders, lastBody, lastErr
	}

	return 0, nil, nil, lastErr
}

// doRequest performs a single HTTP POST request.
func (c *SidebandHTTPClient) doRequest(ctx context.Context, requestURL string, body []byte, parsedURL *ParsedURL) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers per the sideband protocol
	hostHeader := parsedURL.Host
	if parsedURL.Port > 0 {
		hostHeader = fmt.Sprintf("%s:%d", parsedURL.Host, parsedURL.Port)
	}

	req.Host = hostHeader
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("Kong/%s", Version))
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set(c.config.SecretHeaderName, c.config.SharedSecret)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return resp.StatusCode, resp.Header, respBody, nil
}

// parseRetryAfter parses the Retry-After header value as seconds.
// Returns defaultRetryAfterSec if the header is missing or invalid.
func parseRetryAfter(headers http.Header) int {
	val := headers.Get("Retry-After")
	if val == "" {
		return defaultRetryAfterSec
	}
	secs, err := strconv.Atoi(val)
	if err != nil || secs <= 0 {
		return defaultRetryAfterSec
	}
	return secs
}

// ParseURL parses a raw URL string into a ParsedURL struct.
// Sets default ports (80 for http, 443 for https) and default path "/".
func ParseURL(rawURL string) (*ParsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("URL must have a host")
	}

	port := 0
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil {
			return nil, fmt.Errorf("invalid port in URL: %w", err)
		}
	} else {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}

	path := u.Path
	if path == "" {
		path = "/"
	}

	return &ParsedURL{
		Scheme: scheme,
		Host:   host,
		Port:   port,
		Path:   path,
		Query:  u.RawQuery,
	}, nil
}

// BuildSidebandURL constructs the full URL for a sideband endpoint.
func BuildSidebandURL(parsedURL *ParsedURL, sidebandPath string) string {
	// Ensure single / separator between path and sideband endpoint
	basePath := strings.TrimRight(parsedURL.Path, "/")
	return fmt.Sprintf("%s://%s:%d%s%s", parsedURL.Scheme, parsedURL.Host, parsedURL.Port, basePath, sidebandPath)
}

// isTimeout checks if an error is a timeout error.
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	netErr, ok := err.(net.Error)
	return ok && netErr.Timeout()
}
