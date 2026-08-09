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
	"testing"
)

func TestVolumeHourlyPrice(t *testing.T) {
	t.Parallel()

	// Golden: staging 2026-08-09 — 500GB NVMe at $0.20/GB/mo bills $0.1370/hr.
	tests := []struct {
		name         string
		monthlyPerGB float64
		sizeGB       int
		want         float64
	}{
		{name: "500GB NVMe at $0.20/GB/mo", monthlyPerGB: 0.20, sizeGB: 500, want: 0.1370},
		{name: "100GB NVMe at $0.10/GB/mo", monthlyPerGB: 0.10, sizeGB: 100, want: 0.0137},
		{name: "exact 4-decimal division stays exact", monthlyPerGB: 0.73, sizeGB: 10, want: 0.0100},
		{name: "zero size", monthlyPerGB: 0.20, sizeGB: 0, want: 0},
		{name: "zero price", monthlyPerGB: 0, sizeGB: 500, want: 0},
		{
			// Per-GB rounding would give ceil(0.20/730*1e4)/1e4 * 500 = 0.0003*500 = 0.15;
			// the ceiling must apply after the size multiplication.
			name:         "ceiling after size multiplication, not per GB",
			monthlyPerGB: 0.20,
			sizeGB:       500,
			want:         0.1370,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := VolumeHourlyPrice(tt.monthlyPerGB, tt.sizeGB)
			if got != tt.want {
				t.Fatalf("VolumeHourlyPrice(%v, %d) = %v, want %v", tt.monthlyPerGB, tt.sizeGB, got, tt.want)
			}
		})
	}
}

func TestVolumeMonthlyPrice(t *testing.T) {
	t.Parallel()

	// Golden: 500GB NVMe at $0.20/GB/mo → $100.00/mo.
	if got := VolumeMonthlyPrice(0.20, 500); got != 100.0 {
		t.Fatalf("VolumeMonthlyPrice(0.20, 500) = %v, want 100.0", got)
	}
	if got := VolumeMonthlyPrice(0.10, 100); got != 10.0 {
		t.Fatalf("VolumeMonthlyPrice(0.10, 100) = %v, want 10.0", got)
	}
}
