package main

import (
	"testing"
)

func TestFormatHeaders_Basic(t *testing.T) {
	input := map[string][]string{
		"Content-Type": {"application/json"},
		"X-Custom":     {"val1", "val2"},
	}

	result, err := FormatHeaders(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 3 entries: 1 for content-type, 2 for x-custom
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}

	// Verify all names are lowercased
	for _, entry := range result {
		for name := range entry {
			if name != "content-type" && name != "x-custom" {
				t.Errorf("expected lowercased header name, got %q", name)
			}
		}
	}
}

func TestFormatHeaders_Empty(t *testing.T) {
	result, err := FormatHeaders(map[string][]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(result))
	}
}

func TestFormatHeaders_Nil(t *testing.T) {
	result, err := FormatHeaders(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(result))
	}
}

func TestFlattenHeaders_Basic(t *testing.T) {
	input := []map[string]string{
		{"content-type": "application/json"},
		{"x-custom": "val1"},
		{"x-custom": "val2"},
	}

	result := FlattenHeaders(input)

	if len(result["content-type"]) != 1 || result["content-type"][0] != "application/json" {
		t.Errorf("unexpected content-type: %v", result["content-type"])
	}
	if len(result["x-custom"]) != 2 || result["x-custom"][0] != "val1" || result["x-custom"][1] != "val2" {
		t.Errorf("unexpected x-custom: %v", result["x-custom"])
	}
}

func TestFlattenHeaders_CaseNormalization(t *testing.T) {
	input := []map[string]string{
		{"Content-Type": "application/json"},
		{"CONTENT-TYPE": "text/html"},
	}

	result := FlattenHeaders(input)

	vals := result["content-type"]
	if len(vals) != 2 {
		t.Fatalf("expected 2 values for content-type, got %d", len(vals))
	}
}

func TestFlattenHeaders_Empty(t *testing.T) {
	result := FlattenHeaders(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d entries", len(result))
	}
}

func TestRoundTrip(t *testing.T) {
	original := map[string][]string{
		"content-type": {"application/json"},
		"x-custom":     {"val1", "val2"},
		"accept":       {"text/html"},
	}

	formatted, err := FormatHeaders(original)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	flattened := FlattenHeaders(formatted)

	for name, values := range original {
		got := flattened[name]
		if len(got) != len(values) {
			t.Errorf("header %q: expected %d values, got %d", name, len(values), len(got))
			continue
		}
		// Values should all be present (order within a key is preserved)
		valMap := make(map[string]int)
		for _, v := range values {
			valMap[v]++
		}
		for _, v := range got {
			valMap[v]--
		}
		for v, count := range valMap {
			if count != 0 {
				t.Errorf("header %q: value %q count mismatch: %d", name, v, count)
			}
		}
	}
}

func TestFormatHeadersFromInterface_MultidimensionalError(t *testing.T) {
	input := map[string]interface{}{
		"x-nested": []interface{}{[]interface{}{"bad"}},
	}

	_, err := FormatHeadersFromInterface(input)
	if err == nil {
		t.Fatal("expected error for multidimensional values")
	}
}

func TestFormatHeadersFromInterface_StringValues(t *testing.T) {
	input := map[string]interface{}{
		"Content-Type": "application/json",
		"X-Custom":     []interface{}{"val1", "val2"},
	}

	result, err := FormatHeadersFromInterface(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(result))
	}
}
