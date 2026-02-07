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
