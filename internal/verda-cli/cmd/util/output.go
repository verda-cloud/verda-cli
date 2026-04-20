// Copyright 2026 Verda Cloud Oy
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
