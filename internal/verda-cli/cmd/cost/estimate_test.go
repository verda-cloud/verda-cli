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
	"bytes"
	"math"
	"strings"
	"testing"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func TestVolumeCostItem(t *testing.T) {
	t.Parallel()

	vtMap := map[string]verda.VolumeType{
		"NVMe": {Type: "NVMe", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.10}},
		"HDD":  {Type: "HDD", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.03}},
	}

	item, err := volumeCostItem("NVMe", 100, vtMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Monthly = 0.10 * 100 = $10.00
	if math.Abs(item.Monthly-10.0) > 0.01 {
		t.Fatalf("expected monthly $10.00, got $%.2f", item.Monthly)
	}
	// Spec formula: hourly = ceil(monthly*size/730*10000)/10000.
	expectedHourly := math.Ceil(0.10*100/730*10000) / 10000
	if math.Abs(item.Hourly-expectedHourly) > 0.0001 {
		t.Fatalf("expected hourly $%.4f, got $%.4f", expectedHourly, item.Hourly)
	}
	// Cross-check the production helper agrees.
	if item.Hourly != cmdutil.VolumeHourlyPrice(0.10, 100) {
		t.Fatalf("hourly $%.4f disagrees with cmdutil.VolumeHourlyPrice $%.4f", item.Hourly, cmdutil.VolumeHourlyPrice(0.10, 100))
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

	item, err := volumeCostItem("HDD", 500, vtMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Monthly = 0.03 * 500 = $15.00
	if math.Abs(item.Monthly-15.0) > 0.01 {
		t.Fatalf("expected monthly $15.00, got $%.2f", item.Monthly)
	}
}

func TestVolumeCostItemUnknownType(t *testing.T) {
	t.Parallel()

	vtMap := map[string]verda.VolumeType{
		"NVMe": {Type: "NVMe", Price: verda.VolumeTypePrice{PricePerMonthPerGB: 0.10}},
	}
	_, err := volumeCostItem("nvme", 100, vtMap)

	// Unknown types (here: wrong case) must error and list the valid types —
	// previously they silently priced at $0 into the estimate total.
	if err == nil {
		t.Fatal("expected error for unknown volume type, got nil")
	}
	if !strings.Contains(err.Error(), `"nvme"`) || !strings.Contains(err.Error(), "NVMe") {
		t.Fatalf("error should name the invalid type and list valid types, got: %v", err)
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

	e.computeTotals()

	expected := 0.44 + 0.0137 + 0.0685
	if math.Abs(e.Total.Hourly-expected) > 0.001 {
		t.Fatalf("expected total hourly $%.4f, got $%.4f", expected, e.Total.Hourly)
	}
	if e.Total.Monthly != 321.20+10.00+50.00 {
		t.Fatalf("expected total monthly $381.20, got $%.2f", e.Total.Monthly)
	}
}

// The disclaimer belongs in human output only: adding it to JSON/YAML would
// change the contract agents parse.
func TestEstimateStructuredOutputHasNoDisclaimer(t *testing.T) {
	t.Parallel()

	e := Estimate{
		InstanceType: "CPU.4V.16G",
		Instance:     LineItem{Hourly: 0.0279, Daily: 0.6696, Monthly: 20.367},
	}
	e.computeTotals()

	var buf bytes.Buffer
	if _, err := cmdutil.WriteStructured(&buf, "json", e); err != nil {
		t.Fatalf("WriteStructured: %v", err)
	}
	if strings.Contains(buf.String(), cmdutil.PriceDisclaimer) {
		t.Errorf("disclaimer leaked into JSON:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "disclaimer") {
		t.Errorf("JSON gained a disclaimer field:\n%s", buf.String())
	}
}
