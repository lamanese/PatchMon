// Package utils provides utility functions for common operations
//
//nolint:revive // utils is a common package name in Go projects
package utils

import (
	"bytes"
	"encoding/json"
)

// UnmarshalPSJSONArray decodes raw JSON produced by PowerShell's
// ConvertTo-Json into dst, a pointer to a slice.
//
// ConvertTo-Json collapses a single-element collection to a bare JSON
// object instead of a one-element array, and emits the literal "null" for
// an empty collection — both of which would otherwise fail json.Unmarshal
// into a slice. This normalizes those two cases before decoding; a real
// JSON array (including an empty one) is passed through unchanged, and
// anything else that still fails to parse is returned as an error.
func UnmarshalPSJSONArray[T any](raw []byte, dst *[]T) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*dst = nil
		return nil
	}
	if trimmed[0] != '[' {
		wrapped := make([]byte, 0, len(trimmed)+2)
		wrapped = append(wrapped, '[')
		wrapped = append(wrapped, trimmed...)
		wrapped = append(wrapped, ']')
		trimmed = wrapped
	}
	return json.Unmarshal(trimmed, dst)
}
