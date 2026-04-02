package util

import (
	"encoding/json"
	"io"

	"go.yaml.in/yaml/v3"
)

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
