package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Config holds the plugin configuration. Kong creates one instance per plugin configuration.
type Config struct {
	// Required fields
	ServiceURL       string `json:"service_url"`
	SharedSecret     string `json:"shared_secret"`
	SecretHeaderName string `json:"secret_header_name"`

	// Timeouts and connection
	ConnectionTimeoutMs   int  `json:"connection_timeout_ms"`
	ConnectionKeepaliveMs int  `json:"connection_keepalive_ms"`
	VerifyServiceCert     bool `json:"verify_service_cert"`

	// Phase control
	SkipResponsePhase bool `json:"skip_response_phase"`

	// Error handling
	FailOpen               bool  `json:"fail_open"`
	PassthroughStatusCodes []int `json:"passthrough_status_codes"`

	// Retry
	MaxRetries     int `json:"max_retries"`
	RetryBackoffMs int `json:"retry_backoff_ms"`

	// Circuit breaker
	CircuitBreakerEnabled bool `json:"circuit_breaker_enabled"`

	// Request modification
	StripAcceptEncoding bool `json:"strip_accept_encoding"`

	// Client certificate
	IncludeFullCertChain bool `json:"include_full_cert_chain"`

	// Debug and observability
	EnableDebugLogging bool     `json:"enable_debug_logging"`
	EnableOtel         bool     `json:"enable_otel"`
	RedactHeaders      []string `json:"redact_headers"`
	DebugBodyMaxBytes  int      `json:"debug_body_max_bytes"`

	// MCP support
	EnableMCP            bool     `json:"enable_mcp"`              // Master toggle for MCP detection and enrichment
	MCPJsonrpcErrors     bool     `json:"mcp_jsonrpc_errors"`      // Return JSON-RPC 2.0 error format for MCP traffic
	MaxSidebandBodyBytes int      `json:"max_sideband_body_bytes"` // Max sideband payload size in bytes, 0 = unlimited
	ExtractHeaders       []string `json:"extract_headers"`         // Headers to extract as top-level fields in sideband payload
	MCPRetryMethods      []string `json:"mcp_retry_methods"`       // MCP methods safe to retry on failure

	// Lazy-initialized fields
	httpClientOnce sync.Once
	httpClient     *SidebandHTTPClient
	otelOnce       sync.Once
	otelShutdown   func()
}

// Validate performs custom validation on the config beyond what Kong schema validation provides.
func (c *Config) Validate() error {
	// service_url: must be valid http or https
	if c.ServiceURL == "" {
		return fmt.Errorf("service_url is required")
	}
	u, err := url.Parse(c.ServiceURL)
	if err != nil {
		return fmt.Errorf("service_url is not a valid URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("service_url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("service_url must have a host")
	}

	if c.SharedSecret == "" {
		return fmt.Errorf("shared_secret is required")
	}
	if c.SecretHeaderName == "" {
		return fmt.Errorf("secret_header_name is required")
	}
	if c.ConnectionTimeoutMs <= 0 {
		return fmt.Errorf("connection_timeout_ms must be > 0")
	}
	if c.ConnectionKeepaliveMs <= 0 {
		return fmt.Errorf("connection_keepalive_ms must be > 0")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be >= 0")
	}
	if c.RetryBackoffMs <= 0 {
		return fmt.Errorf("retry_backoff_ms must be > 0")
	}
	for _, code := range c.PassthroughStatusCodes {
		if code < 400 || code > 599 {
			return fmt.Errorf("passthrough_status_codes must be in range 400-599, got %d", code)
		}
	}
	if c.DebugBodyMaxBytes < 0 {
		return fmt.Errorf("debug_body_max_bytes must be >= 0")
	}
	if c.MaxSidebandBodyBytes < 0 {
		return fmt.Errorf("max_sideband_body_bytes must be >= 0")
	}
	for _, m := range c.MCPRetryMethods {
		if !IsMCPMethod(m) {
			return fmt.Errorf("mcp_retry_methods contains invalid MCP method %q", m)
		}
	}

	return nil
}

// getHTTPClient returns the lazily-initialized HTTP client.
func (c *Config) getHTTPClient() *SidebandHTTPClient {
	c.httpClientOnce.Do(func() {
		c.httpClient = NewSidebandHTTPClient(c)
	})
	return c.httpClient
}

// applyDefaults sets default values for fields that Kong would normally default.
// This is used for testing and when running outside Kong's config system.
func (c *Config) applyDefaults() {
	if c.ConnectionTimeoutMs == 0 {
		c.ConnectionTimeoutMs = 10000
	}
	if c.ConnectionKeepaliveMs == 0 {
		c.ConnectionKeepaliveMs = 60000
	}
	if c.RetryBackoffMs == 0 {
		c.RetryBackoffMs = 500
	}
	if c.PassthroughStatusCodes == nil {
		c.PassthroughStatusCodes = []int{413}
	}
	if c.RedactHeaders == nil {
		c.RedactHeaders = []string{"authorization", "cookie"}
	}
	if c.DebugBodyMaxBytes == 0 {
		c.DebugBodyMaxBytes = 8192
	}
	if c.MCPRetryMethods == nil {
		c.MCPRetryMethods = []string{"tools/list", "resources/list", "prompts/list", "initialize"}
	}
}
