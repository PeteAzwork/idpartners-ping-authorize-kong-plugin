package main

import (
	"fmt"
	"strings"
)

// FormatHeaders converts a standard header map to the Sideband array-of-objects format.
// All header names are lowercased. Multi-value headers produce multiple entries.
func FormatHeaders(headers map[string][]string) ([]map[string]string, error) {
	if len(headers) == 0 {
		return []map[string]string{}, nil
	}

	result := make([]map[string]string, 0, len(headers))
	for name, values := range headers {
		lowerName := strings.ToLower(name)
		for _, v := range values {
			result = append(result, map[string]string{lowerName: v})
		}
	}
	return result, nil
}

// FormatHeadersFromInterface converts a header map with interface{} values to Sideband format.
// Accepts string or []string values. Returns error for nested/multidimensional values.
func FormatHeadersFromInterface(headers map[string]interface{}) ([]map[string]string, error) {
	if len(headers) == 0 {
		return []map[string]string{}, nil
	}

	result := make([]map[string]string, 0, len(headers))
	for name, val := range headers {
		lowerName := strings.ToLower(name)
		switch v := val.(type) {
		case string:
			result = append(result, map[string]string{lowerName: v})
		case []string:
			for _, s := range v {
				result = append(result, map[string]string{lowerName: s})
			}
		case []interface{}:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return nil, fmt.Errorf("multidimensional header value for %q", name)
				}
				result = append(result, map[string]string{lowerName: s})
			}
		default:
			return nil, fmt.Errorf("multidimensional header value for %q", name)
		}
	}
	return result, nil
}

// FlattenHeaders converts the Sideband array-of-objects format back to a standard header map.
// All header names are lowercased. Duplicate names have their values collected into a single slice.
func FlattenHeaders(headers []map[string]string) map[string][]string {
	if len(headers) == 0 {
		return map[string][]string{}
	}

	result := make(map[string][]string)
	for _, entry := range headers {
		for name, value := range entry {
			lowerName := strings.ToLower(name)
			result[lowerName] = append(result[lowerName], value)
		}
	}
	return result
}
