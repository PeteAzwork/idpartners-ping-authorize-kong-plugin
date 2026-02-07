package main

import (
	"strings"
	"testing"
)

func TestRedactHeaders_Basic(t *testing.T) {
	headers := []map[string]string{
		{"authorization": "Bearer token123"},
		{"cookie": "session=abc"},
		{"content-type": "application/json"},
	}

	redactSet := map[string]bool{
		"authorization": true,
		"cookie":        true,
	}

	result := RedactHeaders(headers, redactSet, "x-secret")

	for _, entry := range result {
		if v, ok := entry["authorization"]; ok && v != "[REDACTED]" {
			t.Errorf("expected authorization to be redacted, got %q", v)
		}
		if v, ok := entry["cookie"]; ok && v != "[REDACTED]" {
			t.Errorf("expected cookie to be redacted, got %q", v)
		}
		if v, ok := entry["content-type"]; ok && v != "application/json" {
			t.Errorf("expected content-type to be preserved, got %q", v)
		}
	}
}

func TestRedactHeaders_SecretHeaderAlwaysRedacted(t *testing.T) {
	headers := []map[string]string{
		{"x-ping-secret": "my-secret-value"},
		{"content-type": "application/json"},
	}

	// Secret header name is not in the redact set, but should still be redacted
	redactSet := map[string]bool{}

	result := RedactHeaders(headers, redactSet, "X-Ping-Secret")

	for _, entry := range result {
		if v, ok := entry["x-ping-secret"]; ok && v != "[REDACTED]" {
			t.Errorf("expected secret header to be redacted, got %q", v)
		}
	}
}

func TestRedactHeaders_CaseInsensitive(t *testing.T) {
	headers := []map[string]string{
		{"Authorization": "Bearer token"},
	}

	redactSet := map[string]bool{
		"authorization": true,
	}

	result := RedactHeaders(headers, redactSet, "")

	for _, entry := range result {
		if v, ok := entry["Authorization"]; ok && v != "[REDACTED]" {
			t.Errorf("expected case-insensitive redaction, got %q", v)
		}
	}
}

func TestRedactHeaders_Empty(t *testing.T) {
	result := RedactHeaders(nil, map[string]bool{}, "")
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestTruncateBody_NoTruncation(t *testing.T) {
	body := "short body"
	result := TruncateBody(body, 100)
	if result != body {
		t.Errorf("expected no truncation, got %q", result)
	}
}

func TestTruncateBody_Truncated(t *testing.T) {
	body := "this is a long body that should be truncated"
	result := TruncateBody(body, 10)
	if !strings.HasPrefix(result, "this is a ") {
		t.Errorf("unexpected truncation prefix: %q", result)
	}
	if !strings.Contains(result, "[truncated,") {
		t.Errorf("expected truncation marker, got %q", result)
	}
}

func TestTruncateBody_ZeroDisablesTruncation(t *testing.T) {
	body := "this body should not be truncated regardless of length"
	result := TruncateBody(body, 0)
	if result != body {
		t.Errorf("expected zero to disable truncation, got %q", result)
	}
}

func TestTruncateBody_ExactBoundary(t *testing.T) {
	body := "1234567890"
	result := TruncateBody(body, 10)
	if result != body {
		t.Errorf("expected no truncation at exact boundary, got %q", result)
	}
}

func TestTruncateBody_OneByteTooLong(t *testing.T) {
	body := "12345678901"
	result := TruncateBody(body, 10)
	if !strings.HasPrefix(result, "1234567890") {
		t.Errorf("unexpected prefix: %q", result)
	}
	if !strings.Contains(result, "[truncated,") {
		t.Errorf("expected truncation marker: %q", result)
	}
}
