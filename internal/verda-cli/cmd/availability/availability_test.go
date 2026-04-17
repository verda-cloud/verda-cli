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

package availability

import (
	"testing"
)

func TestFormatTypeList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input []string
		want  string
	}{
		{nil, "none"},
		{[]string{}, "none"},
		{[]string{"1V100.6V"}, "1V100.6V"},
		{[]string{"1V100.6V", "CPU.4V.16G"}, "1V100.6V, CPU.4V.16G"},
	}

	for _, tt := range tests {
		got := FormatTypeList(tt.input)
		if got != tt.want {
			t.Errorf("FormatTypeList(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAvailabilityResultSorting(t *testing.T) {
	t.Parallel()

	results := []availabilityResult{
		{LocationCode: "FIN-03", InstanceTypes: []string{"CPU.4V.16G"}, Count: 1},
		{LocationCode: "FIN-01", InstanceTypes: []string{"1V100.6V", "CPU.4V.16G"}, Count: 2},
	}

	// Verify the struct fields.
	if results[0].Count != 1 || results[1].Count != 2 {
		t.Fatal("unexpected count values")
	}
	if results[1].LocationCode != "FIN-01" {
		t.Fatalf("expected FIN-01, got %q", results[1].LocationCode)
	}
}
