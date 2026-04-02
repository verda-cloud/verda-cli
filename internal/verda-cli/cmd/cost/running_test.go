package cost

import (
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func strPtr(s string) *string { return &s }

func TestUniqueVolumeIDs(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{
		OSVolumeID: strPtr("vol-os"),
		VolumeIDs:  []string{"vol-os", "vol-data", "vol-data"},
	}

	ids := uniqueVolumeIDs(inst)
	if len(ids) != 2 {
		t.Fatalf("expected 2 unique IDs, got %d: %v", len(ids), ids)
	}
	if ids[0] != "vol-os" || ids[1] != "vol-data" {
		t.Fatalf("unexpected order: %v", ids)
	}
}

func TestUniqueVolumeIDsNoOS(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{
		VolumeIDs: []string{"vol-1", "vol-2"},
	}

	ids := uniqueVolumeIDs(inst)
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
}

func TestUniqueVolumeIDsEmpty(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{}
	ids := uniqueVolumeIDs(inst)
	if len(ids) != 0 {
		t.Fatalf("expected 0 IDs, got %d", len(ids))
	}
}

func TestUniqueVolumeIDsEmptyOSString(t *testing.T) {
	t.Parallel()

	empty := ""
	inst := &verda.Instance{
		OSVolumeID: &empty,
		VolumeIDs:  []string{"vol-1"},
	}

	ids := uniqueVolumeIDs(inst)
	if len(ids) != 1 {
		t.Fatalf("expected 1 ID (empty OS skipped), got %d: %v", len(ids), ids)
	}
}

func TestRunningCostSummaryTotals(t *testing.T) {
	t.Parallel()

	s := RunningCostSummary{
		Instances: []RunningInstanceCost{
			{Hourly: 0.50, Daily: 12.0, Monthly: 365.0},
			{Hourly: 0.08, Daily: 1.92, Monthly: 58.4},
		},
	}

	var totalH, totalD, totalM float64
	for _, inst := range s.Instances {
		totalH += inst.Hourly
		totalD += inst.Daily
		totalM += inst.Monthly
	}

	if totalH != 0.58 {
		t.Fatalf("expected total hourly 0.58, got %f", totalH)
	}
	if totalD != 13.92 {
		t.Fatalf("expected total daily 13.92, got %f", totalD)
	}
}
