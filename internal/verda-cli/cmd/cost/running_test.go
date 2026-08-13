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

package cost

import (
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func strPtr(s string) *string { return &s }

func TestUniqueVolumeIDs(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{
		OSVolumeID: strPtr("vol-os"),
		VolumeIDs:  []string{"vol-os", "vol-data", "vol-data"},
	}

	ids := cmdutil.UniqueVolumeIDs(inst)
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

	ids := cmdutil.UniqueVolumeIDs(inst)
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d", len(ids))
	}
}

func TestUniqueVolumeIDsEmpty(t *testing.T) {
	t.Parallel()

	inst := &verda.Instance{}
	ids := cmdutil.UniqueVolumeIDs(inst)
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

	ids := cmdutil.UniqueVolumeIDs(inst)
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

	// Assert the production aggregation, not a re-implemented inline sum.
	s.computeTotals()

	if s.Total.Hourly != 0.58 {
		t.Fatalf("expected total hourly 0.58, got %f", s.Total.Hourly)
	}
	if s.Total.Daily != 13.92 {
		t.Fatalf("expected total daily 13.92, got %f", s.Total.Daily)
	}
	if s.Total.Monthly != 423.4 {
		t.Fatalf("expected total monthly 423.4, got %f", s.Total.Monthly)
	}
}

// The web console is the billing source of truth, so a CLI total must read as an
// estimate. This pins the wording that says so.
func TestRenderRunningLabelsTheTotalAsAnEstimate(t *testing.T) {
	t.Parallel()

	s := &RunningCostSummary{
		Instances: []RunningInstanceCost{{
			Hostname: "box-a", InstanceType: "CPU.4V.16G",
			Hourly: 0.0279, Daily: 0.6696, Monthly: 20.367,
		}},
	}
	s.computeTotals()

	var b strings.Builder
	renderRunning(&b, s)
	out := b.String()

	if !strings.Contains(out, "Est. Burn") {
		t.Errorf("total is not labeled as an estimate:\n%s", out)
	}
	if strings.Contains(out, "Total Burn") {
		t.Errorf("total still reads as an authoritative charge:\n%s", out)
	}
	if !strings.Contains(out, "Verda dashboard for actual charges") {
		t.Errorf("missing the pointer to the billing source of truth:\n%s", out)
	}
}
