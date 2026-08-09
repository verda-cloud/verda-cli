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

package status

import (
	"math"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestBuildDashboard(t *testing.T) {
	t.Parallel()

	// price_per_hour values are TOTAL hourly prices (staging fixture
	// temp/docs/c1-ondemand-instance.json: CPU.4V.16G on-demand = 0.0279).
	instances := []verda.Instance{
		{ID: "i1", Status: verda.StatusRunning, Location: "FIN-01", PricePerHour: 0.50, GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
		{ID: "i2", Status: verda.StatusRunning, Location: "FIN-01", PricePerHour: 0.0279, IsSpot: true, CPU: verda.InstanceCPU{NumberOfCores: 4}},
		{ID: "i3", Status: verda.StatusRunning, Location: "US-TX-3", PricePerHour: 4.00, GPU: verda.InstanceGPU{NumberOfGPUs: 8}}, // 8x i1's rate, like the catalog
		{ID: "i4", Status: verda.StatusOffline, Location: "US-TX-5", PricePerHour: 0.10, GPU: verda.InstanceGPU{NumberOfGPUs: 1}},
	}
	volumes := []verda.Volume{
		{ID: "v1", Status: verda.VolumeStatusAttached, Size: 50, BaseHourlyCost: 0.007},
		{ID: "v2", Status: verda.VolumeStatusAttached, Size: 50, BaseHourlyCost: 0.007},
		{ID: "v3", Status: verda.VolumeStatusDetached, Size: 20, BaseHourlyCost: 0.003},
	}
	balance := &verda.Balance{Amount: 847.23, Currency: "USD"}

	d := buildDashboard(instances, volumes, balance)

	// Instance counts
	if d.Instances.Total != 4 {
		t.Fatalf("expected 4 total instances, got %d", d.Instances.Total)
	}
	if d.Instances.Running != 3 {
		t.Fatalf("expected 3 running, got %d", d.Instances.Running)
	}
	if d.Instances.Offline != 1 {
		t.Fatalf("expected 1 offline, got %d", d.Instances.Offline)
	}
	if d.Instances.SpotRunning != 1 {
		t.Fatalf("expected 1 spot running, got %d", d.Instances.SpotRunning)
	}

	// Volume counts
	if d.Volumes.Total != 3 {
		t.Fatalf("expected 3 total volumes, got %d", d.Volumes.Total)
	}
	if d.Volumes.Attached != 2 {
		t.Fatalf("expected 2 attached, got %d", d.Volumes.Attached)
	}
	if d.Volumes.Detached != 1 {
		t.Fatalf("expected 1 detached, got %d", d.Volumes.Detached)
	}
	if d.Volumes.TotalSizeGB != 120 {
		t.Fatalf("expected 120 GB total, got %d", d.Volumes.TotalSizeGB)
	}

	// Financials: burn rate = sum of price_per_hour TOTALS + all volumes
	// i1: 0.50, i2: 0.0279, i3: 4.00 (NOT 4.00 * 8 GPUs = 32.00), i4: 0.10
	// Instance total: 4.6279
	// All volumes: 0.007 + 0.007 + 0.003 = 0.017
	expectedHourly := 4.6449
	if math.Abs(d.Financials.BurnRateHourly-expectedHourly) > 0.001 {
		t.Fatalf("expected hourly burn rate ~$%.4f, got $%.4f", expectedHourly, d.Financials.BurnRateHourly)
	}
	if math.Abs(d.Financials.BurnRateDaily-expectedHourly*24) > 0.1 {
		t.Fatalf("expected daily burn rate ~$%.2f, got $%.4f", expectedHourly*24, d.Financials.BurnRateDaily)
	}
	if d.Financials.Balance != 847.23 {
		t.Fatalf("expected balance $847.23, got $%.2f", d.Financials.Balance)
	}
	if d.Financials.Currency != "USD" {
		t.Fatalf("expected currency USD, got %s", d.Financials.Currency)
	}
	// Runway = 847.23 / (4.6449 * 24) ≈ 7 days
	if d.Financials.RunwayDays < 6 || d.Financials.RunwayDays > 9 {
		t.Fatalf("expected runway ~7 days, got %d", d.Financials.RunwayDays)
	}

	// Locations
	if len(d.Locations) != 3 {
		t.Fatalf("expected 3 locations, got %d", len(d.Locations))
	}
}

// TestBuildDashboardBurnRateUsesCatalogTotal (review C1 regression): an 8-GPU
// instance contributes exactly its catalog price_per_hour to the burn rate.
// A re-multiplication by GPU count breaks this exact equality (8 * 0.5 * 8).
func TestBuildDashboardBurnRateUsesCatalogTotal(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "gpu8", Status: verda.StatusRunning, Location: "FIN-01", PricePerHour: 4.00, GPU: verda.InstanceGPU{NumberOfGPUs: 8}},
	}

	d := buildDashboard(instances, nil, nil)

	if d.Financials.BurnRateHourly != 4.00 {
		t.Fatalf("8-GPU burn contribution = $%.4f/hr, want catalog total $4.00/hr", d.Financials.BurnRateHourly)
	}
}

func TestBuildDashboardEmpty(t *testing.T) {
	t.Parallel()

	d := buildDashboard(nil, nil, &verda.Balance{Amount: 100, Currency: "USD"})

	if d.Instances.Total != 0 {
		t.Fatalf("expected 0 instances, got %d", d.Instances.Total)
	}
	if d.Financials.BurnRateHourly != 0 {
		t.Fatalf("expected 0 burn rate, got %f", d.Financials.BurnRateHourly)
	}
	if d.Financials.RunwayDays != 0 {
		t.Fatalf("expected 0 runway (no burn), got %d", d.Financials.RunwayDays)
	}
}

func TestBuildDashboardLocationsSorted(t *testing.T) {
	t.Parallel()

	instances := []verda.Instance{
		{ID: "i1", Status: verda.StatusRunning, Location: "US-TX-3"},
		{ID: "i2", Status: verda.StatusRunning, Location: "FIN-01"},
		{ID: "i3", Status: verda.StatusRunning, Location: "FIN-01"},
	}

	d := buildDashboard(instances, nil, &verda.Balance{})

	// Locations sorted by instance count descending
	if d.Locations[0].Code != "FIN-01" {
		t.Fatalf("expected FIN-01 first (most instances), got %s", d.Locations[0].Code)
	}
	if d.Locations[0].Instances != 2 {
		t.Fatalf("expected 2 instances at FIN-01, got %d", d.Locations[0].Instances)
	}
}
