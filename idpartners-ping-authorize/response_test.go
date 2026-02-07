package main

import (
	"encoding/json"
	"testing"
)

func TestGetStatusString(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "OK"},
		{400, "BAD REQUEST"},
		{401, "UNAUTHORIZED"},
		{404, "NOT FOUND"},
		{413, "PAYLOAD TOO LARGE"},
		{429, "TOO MANY REQUESTS"},
		{500, "INTERNAL SERVER ERROR"},
		{503, "SERVICE UNAVAILABLE"},
		{201, ""},  // not in map
		{302, ""},  // not in map
		{999, ""},  // not in map
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := getStatusString(tt.code)
			if got != tt.want {
				t.Errorf("getStatusString(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestSidebandResponsePayloadJSON_WithState(t *testing.T) {
	state := json.RawMessage(`{"session":"abc"}`)
	payload := &SidebandResponsePayload{
		Method:         "GET",
		URL:            "https://api.example.com/resource",
		Body:           `{"data":"response"}`,
		ResponseCode:   "200",
		ResponseStatus: "OK",
		Headers:        []map[string]string{{"content-type": "application/json"}},
		HTTPVersion:    "1.1",
		State:          state,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SidebandResponsePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Request != nil {
		t.Error("expected request to be nil when state is present")
	}
	if decoded.State == nil {
		t.Error("expected state to be present")
	}
	if string(decoded.State) != `{"session":"abc"}` {
		t.Errorf("unexpected state: %s", string(decoded.State))
	}
}

func TestSidebandResponsePayloadJSON_WithRequest(t *testing.T) {
	originalReq := &SidebandAccessRequest{
		SourceIP:    "10.0.0.1",
		SourcePort:  "12345",
		Method:      "POST",
		URL:         "https://api.example.com/data",
		Body:        "request body",
		Headers:     []map[string]string{{"content-type": "text/plain"}},
		HTTPVersion: "1.1",
	}

	payload := &SidebandResponsePayload{
		Method:         "POST",
		URL:            "https://api.example.com/data",
		Body:           `{"result":"ok"}`,
		ResponseCode:   "200",
		ResponseStatus: "OK",
		Headers:        []map[string]string{{"content-type": "application/json"}},
		HTTPVersion:    "1.1",
		Request:        originalReq,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	var decoded SidebandResponsePayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.State != nil {
		t.Error("expected state to be nil when request is present")
	}
	if decoded.Request == nil {
		t.Fatal("expected request to be present")
	}
	if decoded.Request.SourceIP != "10.0.0.1" {
		t.Errorf("unexpected source_ip: %s", decoded.Request.SourceIP)
	}
}

func TestSidebandResponseResultJSON(t *testing.T) {
	jsonData := `{
		"response_code": "200",
		"body": "{\"data\":\"value\"}",
		"headers": [
			{"content-type": "application/json"},
			{"x-custom": "header-value"}
		]
	}`

	var result SidebandResponseResult
	if err := json.Unmarshal([]byte(jsonData), &result); err != nil {
		t.Fatal(err)
	}

	if result.ResponseCode != "200" {
		t.Errorf("expected response_code 200, got %s", result.ResponseCode)
	}
	if len(result.Headers) != 2 {
		t.Errorf("expected 2 headers, got %d", len(result.Headers))
	}
}

func TestPreservedResponseHeaders(t *testing.T) {
	expected := map[string]bool{
		"content-length": true,
		"date":           true,
		"connection":     true,
		"vary":           true,
	}

	for name, want := range expected {
		if preservedResponseHeaders[name] != want {
			t.Errorf("preservedResponseHeaders[%q] = %v, want %v", name, preservedResponseHeaders[name], want)
		}
	}

	// Verify non-preserved headers are not in the map
	if preservedResponseHeaders["x-custom"] {
		t.Error("x-custom should not be preserved")
	}
}
