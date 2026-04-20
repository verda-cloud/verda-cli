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
	"math"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

func TestVolumeCostItem(t *testing.T) {
	t.Parallel()

	vtMap := map[string]verda.VolumeType{
		"NVMe": {Type: "NVMe", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.10}},
		"HDD":  {Type: "HDD", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.03}},
	}

	item := volumeCostItem("NVMe", 100, vtMap)

	// Monthly = 0.10 * 100 = $10.00
	if math.Abs(item.Monthly-10.0) > 0.01 {
		t.Fatalf("expected monthly $10.00, got $%.2f", item.Monthly)
	}
	// Hourly = ceil(0.10 * 100 / 730 * 10000) / 10000
	expectedHourly := math.Ceil(0.10*100/730*10000) / 10000
	if math.Abs(item.Hourly-expectedHourly) > 0.0001 {
		t.Fatalf("expected hourly $%.4f, got $%.4f", expectedHourly, item.Hourly)
	}
	// Daily = hourly * 24
	if math.Abs(item.Daily-item.Hourly*24) > 0.01 {
		t.Fatalf("expected daily $%.4f, got $%.4f", item.Hourly*24, item.Daily)
	}
}

func TestVolumeCostItemHDD(t *testing.T) {
	t.Parallel()

	vtMap := map[string]verda.VolumeType{
		"HDD": {Type: "HDD", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.03}},
	}

	item := volumeCostItem("HDD", 500, vtMap)

	// Monthly = 0.03 * 500 = $15.00
	if math.Abs(item.Monthly-15.0) > 0.01 {
		t.Fatalf("expected monthly $15.00, got $%.2f", item.Monthly)
	}
}

func TestVolumeCostItemUnknownType(t *testing.T) {
	t.Parallel()

	vtMap := map[string]verda.VolumeType{}
	item := volumeCostItem("Unknown", 100, vtMap)

	if item.Monthly != 0 || item.Hourly != 0 {
		t.Fatalf("expected zero pricing for unknown volume type, got hourly=$%.4f monthly=$%.2f", item.Hourly, item.Monthly)
	}
}

func TestInstanceDescription(t *testing.T) {
	t.Parallel()

	gpu := &verda.InstanceTypeInfo{
		GPU:       verda.InstanceGPU{Description: "V100", NumberOfGPUs: 1},
		GPUMemory: verda.InstanceMemory{SizeInGigabytes: 16},
		Memory:    verda.InstanceMemory{SizeInGigabytes: 64},
		CPU:       verda.InstanceCPU{NumberOfCores: 6},
	}
	desc := instanceDescription(gpu)
	if desc != "1x V100, 16GB VRAM, 64GB RAM" {
		t.Fatalf("unexpected GPU description: %q", desc)
	}

	cpu := &verda.InstanceTypeInfo{
		CPU:    verda.InstanceCPU{NumberOfCores: 4},
		Memory: verda.InstanceMemory{SizeInGigabytes: 16},
	}
	desc = instanceDescription(cpu)
	if desc != "4 CPU, 16GB RAM" {
		t.Fatalf("unexpected CPU description: %q", desc)
	}
}

func TestFormatPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input float64
		want  string
	}{
		{0.44, "$0.44"},
		{10.50, "$10.50"},
		{0.001, "$0.0010"},
		{0.0058, "$0.0058"},
		{0.0, "$0.0000"},
		{321.20, "$321.20"},
	}

	for _, tt := range tests {
		got := formatPrice(tt.input)
		if got != tt.want {
			t.Errorf("formatPrice(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFindInstanceType(t *testing.T) {
	t.Parallel()

	types := []verda.InstanceTypeInfo{
		{InstanceType: "1V100.6V", PricePerHour: 0.44},
		{InstanceType: "CPU.8V.32G", PricePerHour: 0.06},
	}

	found := findInstanceType(types, "CPU.8V.32G")
	if found == nil {
		t.Fatal("expected to find CPU.8V.32G")
	}
	if float64(found.PricePerHour) != 0.06 {
		t.Fatalf("unexpected price: %v", found.PricePerHour)
	}

	// Case insensitive.
	found = findInstanceType(types, "cpu.8v.32g")
	if found == nil {
		t.Fatal("expected case-insensitive match")
	}

	// Not found.
	found = findInstanceType(types, "nonexistent")
	if found != nil {
		t.Fatal("expected nil for nonexistent type")
	}
}

func TestEstimateTotals(t *testing.T) {
	t.Parallel()

	e := Estimate{
		Instance: LineItem{Hourly: 0.44, Daily: 10.56, Monthly: 321.20},
		OSVolume: &LineItem{Hourly: 0.0137, Daily: 0.3288, Monthly: 10.00},
		Storage:  &LineItem{Hourly: 0.0685, Daily: 1.644, Monthly: 50.00},
	}

	total := e.Instance.Hourly
	if e.OSVolume != nil {
		total += e.OSVolume.Hourly
	}
	if e.Storage != nil {
		total += e.Storage.Hourly
	}

	expected := 0.44 + 0.0137 + 0.0685
	if math.Abs(total-expected) > 0.001 {
		t.Fatalf("expected total hourly $%.4f, got $%.4f", expected, total)
	}
}
