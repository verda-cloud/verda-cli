package util

import (
	"encoding/json"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// FormatPrice formats a price value for display. Values below $0.01 use
// 4 decimal places; values at or above use 2 decimal places.
func FormatPrice(v float64) string {
	if v < 0.01 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

// WriteStructured writes data as JSON or YAML to w based on the format string.
// Returns true if structured output was written (json/yaml), false for "table".
func WriteStructured(w io.Writer, format string, data any) (bool, error) {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return true, enc.Encode(data)
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(data); err != nil {
			return true, err
		}
		return true, enc.Close()
	default:
		return false, nil
	}
}
