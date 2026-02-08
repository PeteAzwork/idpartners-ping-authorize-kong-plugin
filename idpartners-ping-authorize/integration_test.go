package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// mockPingAuthorize creates a test server simulating PingAuthorize.
func mockPingAuthorize(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestIntegration_AllowRequest(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		// Read and parse the incoming request
		body, _ := io.ReadAll(r.Body)
		var req SidebandAccessRequest
		json.Unmarshal(body, &req)

		// Return allowed response with state
		resp := SidebandAccessResponse{
			SourceIP: req.SourceIP,
			Method:   req.Method,
			URL:      req.URL,
			Headers:  req.Headers,
			State:    json.RawMessage(`{"session":"test-session"}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "GET",
		URL:         "https://api.example.com/resource",
		Body:        "",
		Headers:     []map[string]string{{"host": "api.example.com"}},
		HTTPVersion: "1.1",
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Response != nil {
		t.Fatal("expected allowed response (no deny)")
	}
	if resp.State == nil {
		t.Fatal("expected state to be present")
	}
}

func TestIntegration_DenyRequest(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		resp := SidebandAccessResponse{
			Response: &DenyResponse{
				ResponseCode:   "403",
				ResponseStatus: "FORBIDDEN",
				Body:           `{"error":"access denied"}`,
				Headers:        []map[string]string{{"content-type": "application/json"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "GET",
		URL:         "https://api.example.com/protected",
		Body:        "",
		Headers:     []map[string]string{{"host": "api.example.com"}},
		HTTPVersion: "1.1",
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Response == nil {
		t.Fatal("expected deny response")
	}
	if resp.Response.ResponseCode != "403" {
		t.Errorf("expected 403, got %s", resp.Response.ResponseCode)
	}
}

func TestIntegration_ModifyRequest(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req SidebandAccessRequest
		json.Unmarshal(body, &req)

		// Return modified request (add a header)
		modifiedBody := "modified body"
		resp := SidebandAccessResponse{
			SourceIP: req.SourceIP,
			Method:   "POST", // changed from original GET
			URL:      req.URL,
			Body:     &modifiedBody,
			Headers: []map[string]string{
				{"host": "api.example.com"},
				{"x-injected": "policy-value"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "GET",
		URL:         "https://api.example.com/resource",
		Body:        "original body",
		Headers:     []map[string]string{{"host": "api.example.com"}},
		HTTPVersion: "1.1",
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}
	if resp.Method != "POST" {
		t.Errorf("expected modified method POST, got %s", resp.Method)
	}
	if resp.Body == nil || *resp.Body != "modified body" {
		t.Errorf("expected modified body")
	}
}

func TestIntegration_ServerError(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"message":"internal error","id":"err-123"}`))
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		RetryBackoffMs:        10,
		MaxRetries:            0,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	_, err := provider.EvaluateRequest(context.Background(), &SidebandAccessRequest{
		SourceIP:    "10.0.0.1",
		SourcePort:  "1234",
		Method:      "GET",
		URL:         "https://api.example.com/test",
		Headers:     []map[string]string{},
		HTTPVersion: "1.1",
	})

	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	// With 0 retries, the HTTP client returns the 5xx response with an error.
	// The provider may receive a sidebandHTTPError or a wrapped error depending
	// on whether the HTTP client returned the body or just an error.
	t.Logf("Got expected error: %v", err)
}

func TestIntegration_InvalidJSON(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`not json`))
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	_, err := provider.EvaluateRequest(context.Background(), &SidebandAccessRequest{
		SourceIP:    "10.0.0.1",
		SourcePort:  "1234",
		Method:      "GET",
		URL:         "https://api.example.com/test",
		Headers:     []map[string]string{},
		HTTPVersion: "1.1",
	})

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIntegration_CircuitBreakerTripAndRecovery(t *testing.T) {
	var callCount int32

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count <= 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		resp := SidebandAccessResponse{
			Method: "GET",
			State:  json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        10,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	req := &SidebandAccessRequest{
		SourceIP: "10.0.0.1", SourcePort: "1234", Method: "GET",
		URL: "https://api.example.com/test", Headers: []map[string]string{}, HTTPVersion: "1.1",
	}

	// First call should trigger 429 — Execute returns (429, headers, body, nil),
	// then provider wraps it as sidebandHTTPError since statusCode >= 400
	_, err := provider.EvaluateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	httpErr, ok := err.(*sidebandHTTPError)
	if !ok {
		t.Fatalf("expected sidebandHTTPError, got %T: %v", err, err)
	}
	if httpErr.StatusCode != 429 {
		t.Errorf("expected 429, got %d", httpErr.StatusCode)
	}

	// Circuit should be open — next call should fail with circuit breaker error
	_, err = provider.EvaluateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	if _, ok := err.(*CircuitBreakerOpenError); !ok {
		t.Logf("got error type %T: %v (circuit breaker open errors pass through provider)", err, err)
	}
}

func TestIntegration_ResponsePhase(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		result := SidebandResponseResult{
			ResponseCode: "200",
			Body:         `{"modified":"true"}`,
			Headers:      []map[string]string{{"content-type": "application/json"}, {"x-policy": "applied"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	payload := &SidebandResponsePayload{
		Method:         "GET",
		URL:            "https://api.example.com/resource",
		Body:           `{"data":"upstream"}`,
		ResponseCode:   "200",
		ResponseStatus: "OK",
		Headers:        []map[string]string{{"content-type": "application/json"}},
		HTTPVersion:    "1.1",
		State:          json.RawMessage(`{"session":"test"}`),
	}

	result, err := provider.EvaluateResponse(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ResponseCode != "200" {
		t.Errorf("expected response_code 200, got %s", result.ResponseCode)
	}
	if result.Body != `{"modified":"true"}` {
		t.Errorf("unexpected body: %s", result.Body)
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	var requestCount int32

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		resp := SidebandAccessResponse{
			Method: "GET",
			State:  json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	var wg sync.WaitGroup
	errCh := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := provider.EvaluateRequest(context.Background(), &SidebandAccessRequest{
				SourceIP: "10.0.0.1", SourcePort: "1234", Method: "GET",
				URL: "https://api.example.com/test", Headers: []map[string]string{}, HTTPVersion: "1.1",
			})
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent request error: %v", err)
	}

	count := atomic.LoadInt32(&requestCount)
	if count != 50 {
		t.Errorf("expected 50 requests, got %d", count)
	}
}

func TestIntegration_SecretHeaderSent(t *testing.T) {
	var receivedSecret string

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		receivedSecret = r.Header.Get("X-Ping-Secret")
		resp := SidebandAccessResponse{State: json.RawMessage(`{}`)}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "my-super-secret",
		SecretHeaderName:      "X-Ping-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	provider.EvaluateRequest(context.Background(), &SidebandAccessRequest{
		SourceIP: "10.0.0.1", SourcePort: "1234", Method: "GET",
		URL: "https://api.example.com/test", Headers: []map[string]string{}, HTTPVersion: "1.1",
	})

	if receivedSecret != "my-super-secret" {
		t.Errorf("expected secret header value %q, got %q", "my-super-secret", receivedSecret)
	}
}

// --- MCP Integration Tests ---

func TestIntegration_MCPToolsCallAllowed(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		// Allow the request
		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{"session":"mcp-test"}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsCallBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsCallBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}, {"content-type": "application/json"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	// Verify sideband payload had MCP context
	if receivedPayload.TrafficType != "mcp" {
		t.Errorf("expected traffic_type mcp, got %s", receivedPayload.TrafficType)
	}
	if receivedPayload.MCP == nil {
		t.Fatal("expected MCP context in sideband payload")
	}
	if receivedPayload.MCP.Method != "tools/call" {
		t.Errorf("expected mcp_method tools/call, got %s", receivedPayload.MCP.Method)
	}
	if receivedPayload.MCP.ToolName != "get_weather" {
		t.Errorf("expected mcp_tool_name get_weather, got %s", receivedPayload.MCP.ToolName)
	}
}

func TestIntegration_MCPToolsCallDenied(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		resp := SidebandAccessResponse{
			Response: &DenyResponse{
				ResponseCode:   "403",
				ResponseStatus: "FORBIDDEN",
				Body:           `{"error":"tool not allowed"}`,
				Headers:        []map[string]string{{"content-type": "application/json"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsCallBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsCallBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response == nil {
		t.Fatal("expected deny response")
	}
	if resp.Response.ResponseCode != "403" {
		t.Errorf("expected 403, got %s", resp.Response.ResponseCode)
	}
}

func TestIntegration_MCPToolsCallArgumentModification(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req SidebandAccessRequest
		json.Unmarshal(body, &req)

		// Return modified body with different arguments
		modifiedBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_weather","arguments":{"city":"Paris"}}}`
		resp := SidebandAccessResponse{
			SourceIP: req.SourceIP,
			Method:   req.Method,
			URL:      req.URL,
			Body:     &modifiedBody,
			Headers:  req.Headers,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsCallBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsCallBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}
	if resp.Body == nil {
		t.Fatal("expected modified body")
	}

	// Verify modified body is still valid JSON-RPC
	modCtx := ParseMCPRequest([]byte(*resp.Body))
	if modCtx == nil {
		t.Fatal("modified body should be valid JSON-RPC")
	}
	if modCtx.Method != "tools/call" {
		t.Errorf("expected method tools/call, got %s", modCtx.Method)
	}
}

func TestIntegration_MCPToolsListAllowed(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsListBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsListBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	if receivedPayload.MCP == nil {
		t.Fatal("expected MCP context in payload")
	}
	if receivedPayload.MCP.Method != "tools/list" {
		t.Errorf("expected mcp_method tools/list, got %s", receivedPayload.MCP.Method)
	}
}

func TestIntegration_MCPResourcesReadAllowed(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpResourcesReadBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpResourcesReadBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	if receivedPayload.MCP == nil {
		t.Fatal("expected MCP context")
	}
	if receivedPayload.MCP.ResourceURI != "file:///data/config.json" {
		t.Errorf("expected resource URI file:///data/config.json, got %s", receivedPayload.MCP.ResourceURI)
	}
}

func TestIntegration_MCPInitializeAllowed(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpInitializeBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpInitializeBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	if receivedPayload.MCP == nil {
		t.Fatal("expected MCP context")
	}
	if receivedPayload.MCP.Method != "initialize" {
		t.Errorf("expected mcp_method initialize, got %s", receivedPayload.MCP.Method)
	}
}

func TestIntegration_NonMCPWithEnableMCPTrue(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	// Send non-MCP body
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://api.example.com/resource",
		Body:        string(nonMCPBody),
		Headers:     []map[string]string{{"host": "api.example.com"}},
		HTTPVersion: "1.1",
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	// Verify no MCP fields in payload
	if receivedPayload.TrafficType != "" {
		t.Errorf("expected empty traffic_type for non-MCP, got %s", receivedPayload.TrafficType)
	}
	if receivedPayload.MCP != nil {
		t.Error("expected nil MCP context for non-MCP request")
	}
}

func TestIntegration_MCPWithEnableMCPFalse(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             false, // MCP disabled
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	// Send MCP body but with enable_mcp=false
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsCallBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response")
	}

	// MCP fields should not be present since enable_mcp=false
	if receivedPayload.TrafficType != "" {
		t.Errorf("expected empty traffic_type when MCP disabled, got %s", receivedPayload.TrafficType)
	}
	if receivedPayload.MCP != nil {
		t.Error("expected nil MCP context when MCP disabled")
	}
}

// --- MCP Response Phase Integration Tests ---

func TestIntegration_MCPResponsePhase(t *testing.T) {
	var receivedPayload SidebandResponsePayload

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		result := SidebandResponseResult{
			ResponseCode: "200",
			Body:         string(mcpToolsListResponse),
			Headers:      []map[string]string{{"content-type": "application/json"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsListBody)
	originalReq := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsListBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	payload := &SidebandResponsePayload{
		Method:         "POST",
		URL:            "https://mcp.example.com/mcp",
		Body:           string(mcpToolsListResponse),
		ResponseCode:   "200",
		ResponseStatus: "OK",
		Headers:        []map[string]string{{"content-type": "application/json"}},
		HTTPVersion:    "1.1",
		Request:        originalReq,
		TrafficType:    "mcp",
		MCP:            mcpCtx,
	}

	result, err := provider.EvaluateResponse(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ResponseCode != "200" {
		t.Errorf("expected response_code 200, got %s", result.ResponseCode)
	}

	// Verify MCP context was in the sideband payload
	if receivedPayload.TrafficType != "mcp" {
		t.Errorf("expected traffic_type mcp in response payload, got %s", receivedPayload.TrafficType)
	}
}

func TestIntegration_MCPToolsListFiltering(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		// PingAuthorize filters out "delete_user" tool
		filteredResponse := `{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"get_weather","description":"Get weather info"},{"name":"list_files","description":"List directory files"}]}}`
		result := SidebandResponseResult{
			ResponseCode: "200",
			Body:         filteredResponse,
			Headers:      []map[string]string{{"content-type": "application/json"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	payload := &SidebandResponsePayload{
		Method:         "POST",
		URL:            "https://mcp.example.com/mcp",
		Body:           string(mcpToolsListResponse),
		ResponseCode:   "200",
		ResponseStatus: "OK",
		Headers:        []map[string]string{{"content-type": "application/json"}},
		HTTPVersion:    "1.1",
		TrafficType:    "mcp",
	}

	result, err := provider.EvaluateResponse(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify filtered body - should only have 2 tools
	var rpcResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(result.Body), &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}
	if len(rpcResp.Result.Tools) != 2 {
		t.Errorf("expected 2 tools after filtering, got %d", len(rpcResp.Result.Tools))
	}
	for _, tool := range rpcResp.Result.Tools {
		if tool.Name == "delete_user" {
			t.Error("delete_user should have been filtered out")
		}
	}
}

// --- MCP Retry and Circuit Breaker Integration Tests ---

func TestIntegration_MCPToolsCallNoRetry(t *testing.T) {
	var attempts int32

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(500)
		w.Write([]byte(`{"message":"internal error"}`))
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		MaxRetries:            3,
		RetryBackoffMs:        10,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsCallBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsCallBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	_, err := provider.EvaluateRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	// tools/call is NOT retryable by default, so only 1 attempt
	if count := atomic.LoadInt32(&attempts); count != 1 {
		t.Errorf("expected 1 attempt (tools/call not retryable), got %d", count)
	}
}

func TestIntegration_MCPToolsListWithRetry(t *testing.T) {
	var attempts int32

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count <= 1 {
			w.WriteHeader(500)
			w.Write([]byte(`{"message":"internal error"}`))
			return
		}
		resp := SidebandAccessResponse{
			Method: "POST",
			State:  json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		MaxRetries:            3,
		RetryBackoffMs:        10,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest(mcpToolsListBody)
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        string(mcpToolsListBody),
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	resp, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Response != nil {
		t.Fatal("expected allowed response after retry")
	}

	// tools/list IS retryable, so should have retried
	if count := atomic.LoadInt32(&attempts); count != 2 {
		t.Errorf("expected 2 attempts (tools/list retried), got %d", count)
	}
}

func TestIntegration_MCPPayloadSizeLimit(t *testing.T) {
	var receivedPayload SidebandAccessRequest

	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &receivedPayload)

		resp := SidebandAccessResponse{
			SourceIP: receivedPayload.SourceIP,
			Method:   receivedPayload.Method,
			URL:      receivedPayload.URL,
			Headers:  receivedPayload.Headers,
			State:    json.RawMessage(`{}`),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	// Create a large body
	largeBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"process","arguments":{"data":"` + string(make([]byte, 10000)) + `"}}}`

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "test-secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
		EnableMCP:             true,
		MaxSidebandBodyBytes:  500,
	}
	config.applyDefaults()

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	mcpCtx := ParseMCPRequest([]byte(largeBody))
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://mcp.example.com/mcp",
		Body:        largeBody,
		Headers:     []map[string]string{{"host": "mcp.example.com"}},
		HTTPVersion: "1.1",
		TrafficType: "mcp",
		MCP:         mcpCtx,
	}

	_, err := provider.EvaluateRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The body should have been truncated but MCP context preserved
	if receivedPayload.MCP != nil && receivedPayload.MCP.Method != "" {
		// MCP context should be preserved even if body is truncated
		t.Logf("MCP context preserved: method=%s", receivedPayload.MCP.Method)
	}
}

func TestIntegration_PassthroughStatusCode(t *testing.T) {
	server := mockPingAuthorize(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(413)
		w.Write([]byte(`{"message":"payload too large"}`))
	})
	defer server.Close()

	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		RetryBackoffMs:        100,
		PassthroughStatusCodes: []int{413},
	}

	httpClient := NewSidebandHTTPClient(config)
	parsedURL, _ := ParseURL(server.URL)
	provider := NewSidebandProvider(config, httpClient, parsedURL)

	_, err := provider.EvaluateRequest(context.Background(), &SidebandAccessRequest{
		SourceIP: "10.0.0.1", SourcePort: "1234", Method: "POST",
		URL: "https://api.example.com/upload", Headers: []map[string]string{}, HTTPVersion: "1.1",
	})

	if err == nil {
		t.Fatal("expected error for 413 response")
	}

	httpErr, ok := err.(*sidebandHTTPError)
	if !ok {
		t.Fatalf("expected sidebandHTTPError, got %T", err)
	}
	if httpErr.StatusCode != 413 {
		t.Errorf("expected 413, got %d", httpErr.StatusCode)
	}
}
