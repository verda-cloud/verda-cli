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
