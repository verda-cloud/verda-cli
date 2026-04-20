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

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestUniqueVolumeIDs(t *testing.T) {
	ptr := func(s string) *string { return &s }

	tests := []struct {
		name string
		inst *verda.Instance
		want []string
	}{
		{
			name: "OS volume plus data volumes with duplicates",
			inst: &verda.Instance{
				OSVolumeID: ptr("os-1"),
				VolumeIDs:  []string{"os-1", "data-1", "data-2", "data-1"},
			},
			want: []string{"os-1", "data-1", "data-2"},
		},
		{
			name: "nil OS volume",
			inst: &verda.Instance{
				OSVolumeID: nil,
				VolumeIDs:  []string{"data-1", "data-2"},
			},
			want: []string{"data-1", "data-2"},
		},
		{
			name: "empty instance",
			inst: &verda.Instance{},
			want: nil,
		},
		{
			name: "empty OS volume string",
			inst: &verda.Instance{
				OSVolumeID: ptr(""),
				VolumeIDs:  []string{"data-1"},
			},
			want: []string{"data-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UniqueVolumeIDs(tt.inst)
			if len(got) != len(tt.want) {
				t.Fatalf("UniqueVolumeIDs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("UniqueVolumeIDs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
