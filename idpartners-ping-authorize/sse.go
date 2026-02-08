package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ParseSSEFinalMessage extracts the last JSON-RPC response from an SSE stream body.
// SSE format: lines prefixed with "data: ", separated by blank lines.
// Returns the final JSON-RPC message body, or the original body if not SSE.
func ParseSSEFinalMessage(body []byte, contentType string) []byte {
	// Only parse SSE content types
	if !isSSEContentType(contentType) {
		return body
	}

	if len(body) == 0 {
		return body
	}

	// Split into SSE events by double newlines
	var lastValidJSON []byte

	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}

		// Extract data payload after "data:" prefix
		data := bytes.TrimPrefix(line, []byte("data:"))
		data = bytes.TrimSpace(data)

		if len(data) == 0 {
			continue
		}

		// Check if this is valid JSON with an "id" or "result" or "error" field (JSON-RPC response indicators)
		if json.Valid(data) && isJsonRPCResponse(data) {
			lastValidJSON = make([]byte, len(data))
			copy(lastValidJSON, data)
		}
	}

	if lastValidJSON != nil {
		return lastValidJSON
	}

	return body
}

// isSSEContentType checks if the content type indicates SSE.
func isSSEContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream")
}

// isJsonRPCResponse checks if JSON data looks like a JSON-RPC response (has jsonrpc field and id field).
func isJsonRPCResponse(data []byte) bool {
	var probe struct {
		Jsonrpc string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Jsonrpc == "2.0" && len(probe.ID) > 0
}
