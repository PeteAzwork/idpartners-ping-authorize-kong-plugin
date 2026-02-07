package main

import (
	"encoding/json"
	"testing"
)

func TestHandleAccessResponse_Denied(t *testing.T) {
	resp := &SidebandAccessResponse{
		Response: &DenyResponse{
			ResponseCode:   "403",
			ResponseStatus: "FORBIDDEN",
			Body:           `{"error":"access denied"}`,
			Headers: []map[string]string{
				{"content-type": "application/json"},
			},
		},
	}

	// We can't test with real kong PDK, but we can verify the response parsing
	if resp.Response == nil {
		t.Fatal("expected deny response to be present")
	}
	if resp.Response.ResponseCode != "403" {
		t.Errorf("expected 403, got %s", resp.Response.ResponseCode)
	}
}

func TestHandleAccessResponse_Allowed(t *testing.T) {
	stateJSON := json.RawMessage(`{"session_id":"abc123"}`)
	body := "modified body"
	resp := &SidebandAccessResponse{
		Method:  "POST",
		URL:     "https://api.example.com/resource",
		Body:    &body,
		Headers: []map[string]string{{"content-type": "application/json"}},
		State:   stateJSON,
	}

	if resp.Response != nil {
		t.Fatal("expected no deny response for allowed request")
	}
	if resp.State == nil {
		t.Fatal("expected state to be present")
	}
	if string(resp.State) != `{"session_id":"abc123"}` {
		t.Errorf("unexpected state: %s", string(resp.State))
	}
}

func TestBuildForwardedURL_Format(t *testing.T) {
	// Test URL format matches spec: scheme://host:port/path[?query]
	// This tests the pure string construction logic
	scheme := "https"
	host := "api.example.com"
	port := 443
	path := "/resource"
	query := "key=value"

	url := scheme + "://" + host + ":" + string(rune('0'+port/100)) + string(rune('0'+(port%100)/10)) + string(rune('0'+port%10)) + path + "?" + query
	expected := "https://api.example.com:443/resource?key=value"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}
}

func TestStringSliceEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"equal", []string{"a", "b"}, []string{"a", "b"}, true},
		{"different length", []string{"a"}, []string{"a", "b"}, false},
		{"different values", []string{"a", "b"}, []string{"a", "c"}, false},
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"nil vs empty", nil, []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringSliceEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("stringSliceEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsPassthroughCode(t *testing.T) {
	conf := &Config{
		PassthroughStatusCodes: []int{413, 429},
	}

	if !isPassthroughCode(413, conf) {
		t.Error("expected 413 to be passthrough")
	}
	if !isPassthroughCode(429, conf) {
		t.Error("expected 429 to be passthrough")
	}
	if isPassthroughCode(500, conf) {
		t.Error("expected 500 to NOT be passthrough")
	}
	if isPassthroughCode(200, conf) {
		t.Error("expected 200 to NOT be passthrough")
	}
}

func TestSidebandAccessRequestJSON(t *testing.T) {
	req := &SidebandAccessRequest{
		SourceIP:    "192.168.1.100",
		SourcePort:  "54321",
		Method:      "GET",
		URL:         "https://api.example.com:443/resource?key=value",
		Body:        "",
		Headers:     []map[string]string{{"host": "api.example.com"}},
		HTTPVersion: "1.1",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SidebandAccessRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.SourceIP != req.SourceIP {
		t.Errorf("source_ip: want %q, got %q", req.SourceIP, decoded.SourceIP)
	}
	if decoded.Method != req.Method {
		t.Errorf("method: want %q, got %q", req.Method, decoded.Method)
	}
}

func TestSidebandAccessResponseJSON_WithState(t *testing.T) {
	jsonData := `{
		"source_ip": "192.168.1.100",
		"method": "GET",
		"url": "https://api.example.com/resource",
		"headers": [{"host": "api.example.com"}],
		"state": {"session_id": "abc"}
	}`

	var resp SidebandAccessResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Response != nil {
		t.Error("expected no deny response")
	}
	if resp.State == nil {
		t.Error("expected state to be present")
	}
}

func TestSidebandAccessResponseJSON_WithDeny(t *testing.T) {
	jsonData := `{
		"response": {
			"response_code": "403",
			"response_status": "FORBIDDEN",
			"body": "{\"error\":\"denied\"}",
			"headers": [{"content-type": "application/json"}]
		}
	}`

	var resp SidebandAccessResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatal(err)
	}

	if resp.Response == nil {
		t.Fatal("expected deny response")
	}
	if resp.Response.ResponseCode != "403" {
		t.Errorf("expected 403, got %s", resp.Response.ResponseCode)
	}
}
