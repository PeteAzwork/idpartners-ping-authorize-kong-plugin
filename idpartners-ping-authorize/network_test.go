package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestParseURL_Basic(t *testing.T) {
	tests := []struct {
		input    string
		scheme   string
		host     string
		port     int
		path     string
		wantErr  bool
	}{
		{"https://example.com/api", "https", "example.com", 443, "/api", false},
		{"http://example.com", "http", "example.com", 80, "/", false},
		{"https://example.com:8443/path", "https", "example.com", 8443, "/path", false},
		{"http://localhost:9090/", "http", "localhost", 9090, "/", false},
		{"ftp://example.com", "", "", 0, "", true},
		{"not a url", "", "", 0, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			parsed, err := ParseURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.Scheme != tt.scheme {
				t.Errorf("scheme: want %q, got %q", tt.scheme, parsed.Scheme)
			}
			if parsed.Host != tt.host {
				t.Errorf("host: want %q, got %q", tt.host, parsed.Host)
			}
			if parsed.Port != tt.port {
				t.Errorf("port: want %d, got %d", tt.port, parsed.Port)
			}
			if parsed.Path != tt.path {
				t.Errorf("path: want %q, got %q", tt.path, parsed.Path)
			}
		})
	}
}

func TestBuildSidebandURL(t *testing.T) {
	parsed := &ParsedURL{
		Scheme: "https",
		Host:   "example.com",
		Port:   443,
		Path:   "/api",
	}

	url := BuildSidebandURL(parsed, "/sideband/request")
	expected := "https://example.com:443/api/sideband/request"
	if url != expected {
		t.Errorf("want %q, got %q", expected, url)
	}
}

func TestBuildSidebandURL_TrailingSlash(t *testing.T) {
	parsed := &ParsedURL{
		Scheme: "https",
		Host:   "example.com",
		Port:   443,
		Path:   "/api/",
	}

	url := BuildSidebandURL(parsed, "/sideband/request")
	expected := "https://example.com:443/api/sideband/request"
	if url != expected {
		t.Errorf("want %q, got %q", expected, url)
	}
}

func TestExecute_SuccessfulRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"method":"GET"}`))
	}))
	defer server.Close()

	parsed, _ := ParseURL(server.URL)
	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        100,
	}

	client := NewSidebandHTTPClient(config)

	status, _, body, err := client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 200 {
		t.Errorf("expected status 200, got %d", status)
	}
	if string(body) != `{"method":"GET"}` {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestExecute_RetryOnServerError(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&attempts, 1)
		if count <= 2 {
			w.WriteHeader(500)
			w.Write([]byte(`error`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`ok`))
	}))
	defer server.Close()

	parsed, _ := ParseURL(server.URL)
	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		MaxRetries:            2,
		RetryBackoffMs:        10,
	}

	client := NewSidebandHTTPClient(config)

	status, _, body, err := client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 200 {
		t.Errorf("expected status 200 after retries, got %d", status)
	}
	if string(body) != "ok" {
		t.Errorf("unexpected body: %s", body)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestExecute_RetryExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`error`))
	}))
	defer server.Close()

	parsed, _ := ParseURL(server.URL)
	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		MaxRetries:            1,
		RetryBackoffMs:        10,
	}

	client := NewSidebandHTTPClient(config)

	status, _, _, err := client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if status != 500 {
		t.Errorf("expected status 500, got %d", status)
	}

	// Circuit breaker should be tripped
	if client.cb.IsClosed() {
		t.Error("expected circuit breaker to be open after exhausting retries")
	}
}

func TestExecute_CircuitBreakerTripsOn429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(429)
		w.Write([]byte(`rate limited`))
	}))
	defer server.Close()

	parsed, _ := ParseURL(server.URL)
	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: true,
		RetryBackoffMs:        10,
	}

	client := NewSidebandHTTPClient(config)

	status, _, _, err := client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 429 {
		t.Errorf("expected status 429, got %d", status)
	}

	// Circuit breaker should be open
	if client.cb.IsClosed() {
		t.Error("expected circuit breaker to be open after 429")
	}

	// Next request should be rejected by circuit breaker
	_, _, _, err = client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
	_, ok := err.(*CircuitBreakerOpenError)
	if !ok {
		t.Errorf("expected CircuitBreakerOpenError, got %T", err)
	}
}

func TestExecute_NoRetryOn4xx(t *testing.T) {
	var attempts int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(400)
		w.Write([]byte(`bad request`))
	}))
	defer server.Close()

	parsed, _ := ParseURL(server.URL)
	config := &Config{
		ServiceURL:            server.URL,
		SharedSecret:          "secret",
		SecretHeaderName:      "X-Secret",
		ConnectionTimeoutMs:   5000,
		ConnectionKeepaliveMs: 60000,
		VerifyServiceCert:     false,
		CircuitBreakerEnabled: false,
		MaxRetries:            3,
		RetryBackoffMs:        10,
	}

	client := NewSidebandHTTPClient(config)

	status, _, _, err := client.Execute(context.Background(), server.URL+"/sideband/request", []byte(`{}`), parsed, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 400 {
		t.Errorf("expected status 400, got %d", status)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", atomic.LoadInt32(&attempts))
	}
}
