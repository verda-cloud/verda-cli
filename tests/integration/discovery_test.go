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

//go:build integration

package integration

import (
	"testing"
)

func TestLocations(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "locations")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var locations []map[string]any
	parseJSON(t, r, &locations)

	if len(locations) == 0 {
		t.Fatal("expected at least one location")
	}

	// Verify structure
	loc := locations[0]
	for _, field := range []string{"code", "name", "country_code"} {
		if _, ok := loc[field]; !ok {
			t.Errorf("location missing field %q", field)
		}
	}
}

func TestInstanceTypes_All(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "instance-types")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var types []map[string]any
	parseJSON(t, r, &types)

	if len(types) == 0 {
		t.Fatal("expected at least one instance type")
	}
}

func TestInstanceTypes_GPUOnly(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "instance-types", "--gpu")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var types []map[string]any
	parseJSON(t, r, &types)

	for _, it := range types {
		gpu, ok := it["gpu"].(map[string]any)
		if !ok {
			continue
		}
		numGPUs, _ := gpu["number_of_gpus"].(float64)
		if numGPUs == 0 {
			t.Errorf("--gpu returned non-GPU type: %v", it["instance_type"])
		}
	}
}

func TestInstanceTypes_CPUOnly(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "instance-types", "--cpu")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var types []map[string]any
	parseJSON(t, r, &types)

	for _, it := range types {
		gpu, ok := it["gpu"].(map[string]any)
		if !ok {
			continue
		}
		numGPUs, _ := gpu["number_of_gpus"].(float64)
		if numGPUs > 0 {
			t.Errorf("--cpu returned GPU type: %v", it["instance_type"])
		}
	}
}

func TestAvailability(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "availability")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var avail []map[string]any
	parseJSON(t, r, &avail)

	if len(avail) == 0 {
		t.Fatal("expected at least one availability entry")
	}
}

func TestAvailability_ByLocation(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "availability", "--location", "FIN-01")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}
}

func TestImages(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "images")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var images []map[string]any
	parseJSON(t, r, &images)

	if len(images) == 0 {
		t.Fatal("expected at least one image")
	}
}

func TestImages_ByType(t *testing.T) {
	requireProfile(t, "test")

	r := runWithProfile(t, "test", "images", "--type", "1A6000.10V")
	if r.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d: %s", r.ExitCode, r.Stderr)
	}

	var images []map[string]any
	parseJSON(t, r, &images)
	// Should return images compatible with V100
}
