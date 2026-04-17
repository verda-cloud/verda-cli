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

import "github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

// UniqueVolumeIDs returns deduplicated volume IDs for an instance with the OS
// volume first (if present), followed by data volumes in their original order.
func UniqueVolumeIDs(inst *verda.Instance) []string {
	seen := make(map[string]bool)
	var ids []string

	// OS volume first.
	if inst.OSVolumeID != nil && *inst.OSVolumeID != "" {
		ids = append(ids, *inst.OSVolumeID)
		seen[*inst.OSVolumeID] = true
	}
	// Then data volumes, deduplicating.
	for _, id := range inst.VolumeIDs {
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids
}
