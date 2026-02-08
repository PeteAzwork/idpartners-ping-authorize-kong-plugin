package main

import (
	"encoding/json"
	"testing"
)

func TestParseSSEFinalMessage_SingleEvent(t *testing.T) {
	body := []byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n")
	result := ParseSSEFinalMessage(body, "text/event-stream")

	var rpc struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(result, &rpc); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if rpc.Jsonrpc != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", rpc.Jsonrpc)
	}
}

func TestParseSSEFinalMessage_MultipleEvents(t *testing.T) {
	body := []byte(
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"partial\":true}}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[\"a\",\"b\"]}}\n\n",
	)
	result := ParseSSEFinalMessage(body, "text/event-stream")

	var rpc struct {
		Result struct {
			Tools []string `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(result, &rpc); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(rpc.Result.Tools) != 2 {
		t.Errorf("expected last event with 2 tools, got %d", len(rpc.Result.Tools))
	}
}

func TestParseSSEFinalMessage_NotificationsFollowedByResponse(t *testing.T) {
	body := []byte(
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{\"content\":[{\"text\":\"hello\"}]}}\n\n",
	)
	result := ParseSSEFinalMessage(body, "text/event-stream")

	var rpc struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(result, &rpc); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if string(rpc.ID) != "5" {
		t.Errorf("expected id 5, got %s", string(rpc.ID))
	}
}

func TestParseSSEFinalMessage_ApplicationJSON(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`)
	result := ParseSSEFinalMessage(body, "application/json")

	if string(result) != string(body) {
		t.Errorf("expected body unchanged for application/json, got %s", string(result))
	}
}

func TestParseSSEFinalMessage_EmptyBody(t *testing.T) {
	result := ParseSSEFinalMessage([]byte{}, "text/event-stream")
	if len(result) != 0 {
		t.Errorf("expected empty result for empty body, got %s", string(result))
	}
}

func TestParseSSEFinalMessage_MalformedSSE(t *testing.T) {
	body := []byte("this is not SSE data\nno data: prefix here\n")
	result := ParseSSEFinalMessage(body, "text/event-stream")

	// Should return original body since no valid data: events found
	if string(result) != string(body) {
		t.Errorf("expected original body for malformed SSE, got %s", string(result))
	}
}

func TestParseSSEFinalMessage_NonJSONData(t *testing.T) {
	body := []byte(
		"data: not json content\n\n" +
			"data: also not json\n\n",
	)
	result := ParseSSEFinalMessage(body, "text/event-stream")

	// No valid JSON-RPC found, return original
	if string(result) != string(body) {
		t.Errorf("expected original body when no valid JSON-RPC in SSE, got %s", string(result))
	}
}

func TestParseSSEFinalMessage_LargeStream(t *testing.T) {
	// Build a stream with many events, only last is a response
	var stream string
	for i := 0; i < 100; i++ {
		stream += "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n"
	}
	stream += "data: {\"jsonrpc\":\"2.0\",\"id\":99,\"result\":{\"done\":true}}\n\n"

	result := ParseSSEFinalMessage([]byte(stream), "text/event-stream")

	var rpc struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(result, &rpc); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if string(rpc.ID) != "99" {
		t.Errorf("expected id 99, got %s", string(rpc.ID))
	}
}

func TestIsSSEContentType(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"text/event-stream", true},
		{"text/event-stream; charset=utf-8", true},
		{"TEXT/EVENT-STREAM", true},
		{"application/json", false},
		{"text/plain", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isSSEContentType(tt.ct)
		if got != tt.want {
			t.Errorf("isSSEContentType(%q) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}
