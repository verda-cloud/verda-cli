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

package instancetypes

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestFilterGPU(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{
		{InstanceType: "1V100.6V", GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
		{InstanceType: "CPU.4V.16G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
		{InstanceType: "4A100.80G", GPU: verda.InstanceGPU{NumberOfGPUs: 4}},
	}

	var filtered []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs > 0 {
			filtered = append(filtered, types[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 GPU types, got %d", len(filtered))
	}
}

func TestFilterCPU(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{
		{InstanceType: "1V100.6V", GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
		{InstanceType: "CPU.4V.16G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
		{InstanceType: "CPU.8V.32G", GPU: verda.InstanceGPU{NumberOfGPUs: 0}},
	}

	var filtered []verda.InstanceTypeInfo
	for i := range types {
		if types[i].GPU.NumberOfGPUs == 0 {
			filtered = append(filtered, types[i])
		}
	}

	if len(filtered) != 2 {
		t.Fatalf("expected 2 CPU types, got %d", len(filtered))
	}
}

func TestCleanGPUDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		gpus int
		desc string
		want string
	}{
		{1, "1x H100 SXM5 80GB", "1x H100 SXM5"},
		{2, "2x B200 SXM6 180GB", "2x B200 SXM6"},
		{8, "8x A100 SXM4 40GB", "8x A100 SXM4"},
		{1, "1x Tesla V100 16GB", "1x Tesla V100"},
		{4, "4x RTX PRO 6000 96GB", "4x RTX PRO 6000"},
		{1, "H100 SXM5 80GB", "1x H100 SXM5"},       // no count prefix
		{1, "RTX 6000 Ada 48GB", "1x RTX 6000 Ada"}, // no count prefix
	}

	for _, tt := range tests {
		info := &verda.InstanceTypeInfo{
			GPU: verda.InstanceGPU{NumberOfGPUs: tt.gpus, Description: tt.desc},
		}
		got := cleanGPUDescription(info)
		if got != tt.want {
			t.Errorf("cleanGPUDescription(%d, %q) = %q, want %q", tt.gpus, tt.desc, got, tt.want)
		}
	}
}

func TestFormatGB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int
		want  string
	}{
		{16, "16GB"},
		{256, "256GB"},
		{960, "960GB"},
		{1000, "1.0TB"},
		{1440, "1.4TB"},
		{2200, "2.2TB"},
	}

	for _, tt := range tests {
		got := formatGB(tt.input)
		if got != tt.want {
			t.Errorf("formatGB(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFilterEmpty(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{}
	var filtered []verda.InstanceTypeInfo
	for _, t := range types {
		if t.GPU.NumberOfGPUs > 0 {
			filtered = append(filtered, t)
		}
	}

	if len(filtered) != 0 {
		t.Fatalf("expected 0, got %d", len(filtered))
	}
}
