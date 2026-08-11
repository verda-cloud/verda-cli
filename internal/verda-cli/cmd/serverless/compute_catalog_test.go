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

package serverless

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/verda-cloud/verda-cli/pkg/tui/wizard"
	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// Test data mirrors the 2026-08-11 production captures
// (temp/agent/fixtures/*.json): `model` carries the short name shared with
// /serverless-compute-resources, `name` carries the display form, and
// gpu_memory / serverless_price are per-row totals.

func gpuTypeRow(model, display string, gpus, vramTotal int, price, spot float64) verda.ContainerType {
	return verda.ContainerType{
		Model:               model,
		Name:                display,
		Manufacturer:        "NVIDIA",
		GPU:                 verda.InstanceGPU{NumberOfGPUs: gpus},
		GPUMemory:           verda.InstanceMemory{SizeInGigabytes: vramTotal},
		ServerlessPrice:     verda.FlexibleFloat(price),
		ServerlessSpotPrice: verda.FlexibleFloat(spot),
	}
}

func cpuTypeRow(model, display string, cores int, price, spot float64) verda.ContainerType {
	return verda.ContainerType{
		Model:               model,
		Name:                display,
		Manufacturer:        "AMD",
		CPU:                 verda.InstanceCPU{NumberOfCores: cores},
		ServerlessPrice:     verda.FlexibleFloat(price),
		ServerlessSpotPrice: verda.FlexibleFloat(spot),
	}
}

func computeRes(name string, size int, available bool) verda.ComputeResource {
	return verda.ComputeResource{Name: name, Size: size, IsAvailable: available}
}

// Real-flavored catalog subset: RTX PRO 6000 at sizes 1/2/8, B200 at 1/2,
// one CPU flavor at size 8.
func testTypes() []verda.ContainerType {
	return []verda.ContainerType{
		gpuTypeRow("RTX PRO 6000", "RTX PRO 6000 96GB", 1, 96, 2.08, 1.04),
		gpuTypeRow("RTX PRO 6000", "RTX PRO 6000 96GB", 2, 192, 4.16, 2.08),
		gpuTypeRow("RTX PRO 6000", "RTX PRO 6000 96GB", 8, 768, 16.63, 8.32),
		gpuTypeRow("B200", "B200 SXM6 180GB", 1, 180, 6.72, 3.36),
		gpuTypeRow("B200", "B200 SXM6 180GB", 2, 360, 13.44, 6.72),
		cpuTypeRow("CPU Node", "AMD EPYC", 8, 0.06, 0.03),
	}
}

func TestJoinComputeCatalog(t *testing.T) {
	rtx := func(size int, avail bool) ComputeOption {
		totals := map[int]struct{ vram, perGPU int }{1: {96, 96}, 2: {192, 96}, 8: {768, 96}}
		prices := map[int]struct{ price, spot float64 }{1: {2.08, 1.04}, 2: {4.16, 2.08}, 8: {16.63, 8.32}}
		return ComputeOption{
			Name: "RTX PRO 6000", Size: size, Available: avail, Manufacturer: "NVIDIA",
			TotalVRAMGB: totals[size].vram, VRAMPerGPUGB: totals[size].perGPU,
			HourlyPrice: prices[size].price, HourlySpotPrice: prices[size].spot, Enriched: true,
		}
	}

	cases := []struct {
		name  string
		types []verda.ContainerType
		res   []verda.ComputeResource
		want  []ComputeOption
	}{
		{
			name:  "happy path: 2 flavors x 3 sizes, exact sorted order",
			types: testTypes(),
			res: []verda.ComputeResource{
				computeRes("B200", 2, true),
				computeRes("RTX PRO 6000", 8, true),
				computeRes("RTX PRO 6000", 1, true),
				computeRes("B200", 1, true),
				computeRes("RTX PRO 6000", 2, true),
			},
			want: []ComputeOption{
				rtx(1, true),
				{Name: "B200", Size: 1, Available: true, Manufacturer: "NVIDIA", TotalVRAMGB: 180, VRAMPerGPUGB: 180, HourlyPrice: 6.72, HourlySpotPrice: 3.36, Enriched: true},
				rtx(2, true),
				{Name: "B200", Size: 2, Available: true, Manufacturer: "NVIDIA", TotalVRAMGB: 360, VRAMPerGPUGB: 180, HourlyPrice: 13.44, HourlySpotPrice: 6.72, Enriched: true},
				rtx(8, true),
			},
		},
		{
			name:  "resource with no matching type: kept, degraded, zeroed (A100 shape)",
			types: testTypes(),
			res:   []verda.ComputeResource{computeRes("A100 40GB", 1, false)},
			want: []ComputeOption{
				{Name: "A100 40GB", Size: 1, Available: false},
			},
		},
		{
			name:  "resource at a size the type catalog lacks: degraded, not guessed",
			types: testTypes(),
			res:   []verda.ComputeResource{computeRes("B200", 4, true)},
			want: []ComputeOption{
				{Name: "B200", Size: 4, Available: true},
			},
		},
		{
			name:  "type with no matching resource: dropped entirely",
			types: testTypes(),
			res:   []verda.ComputeResource{computeRes("RTX PRO 6000", 1, true)},
			want: []ComputeOption{
				rtx(1, true),
			},
		},
		{
			name:  "unavailable rows sort after available ones regardless of VRAM",
			types: testTypes(),
			res: []verda.ComputeResource{
				computeRes("B200", 1, false),
				computeRes("RTX PRO 6000", 1, true),
				computeRes("A100 40GB", 1, false),
			},
			want: []ComputeOption{
				rtx(1, true),
				{Name: "A100 40GB", Size: 1, Available: false}, // degraded: 0 VRAM first
				{Name: "B200", Size: 1, Available: false, Manufacturer: "NVIDIA", TotalVRAMGB: 180, VRAMPerGPUGB: 180, HourlyPrice: 6.72, HourlySpotPrice: 3.36, Enriched: true},
			},
		},
		{
			name:  "CPU flavor: keys on cores, vram zero, no division by zero, price still enriches",
			types: testTypes(),
			res:   []verda.ComputeResource{computeRes("CPU Node", 8, true)},
			want: []ComputeOption{
				{Name: "CPU Node", Size: 8, Available: true, Manufacturer: "AMD", HourlyPrice: 0.06, HourlySpotPrice: 0.03, Enriched: true},
			},
		},
		{
			name:  "empty inputs: empty non-nil slice",
			types: []verda.ContainerType{},
			res:   []verda.ComputeResource{},
			want:  []ComputeOption{},
		},
		{
			name:  "nil inputs: empty non-nil slice",
			types: nil,
			res:   nil,
			want:  []ComputeOption{},
		},
		{
			name:  "duplicate (name, size): first row wins",
			types: testTypes(),
			res: []verda.ComputeResource{
				computeRes("RTX PRO 6000", 1, true),
				computeRes("RTX PRO 6000", 1, false),
			},
			want: []ComputeOption{rtx(1, true)},
		},
		{
			name:  "totals at size 8: 768 read from the API row, per-GPU by division",
			types: testTypes(),
			res:   []verda.ComputeResource{computeRes("RTX PRO 6000", 8, true)},
			want:  []ComputeOption{rtx(8, true)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := joinComputeCatalog(tc.types, tc.res)
			if got == nil {
				t.Fatalf("got nil slice, want empty non-nil")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mismatch:\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

// Regression for the 2026-08-11 name-vocabulary mismatch: a name-based join
// silently enriches NOTHING and every label degrades. The join must key on
// model; this test fails if the enriched count is zero.
func TestJoinComputeCatalog_EnrichesOnModelKey(t *testing.T) {
	got := joinComputeCatalog(testTypes(), []verda.ComputeResource{
		computeRes("RTX PRO 6000", 1, true),
		computeRes("B200", 2, true),
		computeRes("CPU Node", 8, true),
	})
	enriched := 0
	for i := range got {
		if !got[i].Enriched {
			continue
		}
		enriched++
		if got[i].Name != "CPU Node" && got[i].TotalVRAMGB == 0 {
			t.Fatalf("enriched GPU row %q has zero VRAM", got[i].Name)
		}
	}
	if enriched == 0 {
		t.Fatal("zero rows enriched — join key is broken (name vs model vocabulary?)")
	}
	if enriched != 3 {
		t.Fatalf("enriched %d rows, want 3", enriched)
	}
}

func TestComputeOptionLabel(t *testing.T) {
	enrichedRTX2 := ComputeOption{
		Name: "RTX PRO 6000", Size: 2, Available: true, Enriched: true,
		TotalVRAMGB: 192, HourlyPrice: 4.16, HourlySpotPrice: 2.08,
	}
	cases := []struct {
		name string
		opt  ComputeOption
		spot bool
		want string
	}{
		{
			name: "degraded and available: legacy text plus gap note",
			opt:  ComputeOption{Name: "RTX 6000 Ada", Size: 1, Available: true},
			want: "RTX 6000 Ada  (size 1) · VRAM and price unavailable",
		},
		{
			name: "degraded and unavailable: marker after the gap note",
			opt:  ComputeOption{Name: "A100 40GB", Size: 1, Available: false},
			want: "A100 40GB  (size 1) · VRAM and price unavailable · unavailable",
		},
		{
			name: "enriched on-demand shows VRAM and on-demand price",
			opt:  enrichedRTX2,
			spot: false,
			want: "RTX PRO 6000 · 2× · 192 GB VRAM · $4.16/h",
		},
		{
			name: "enriched spot shows the spot price, not the on-demand one",
			opt:  enrichedRTX2,
			spot: true,
			want: "RTX PRO 6000 · 2× · 192 GB VRAM · $2.08/h",
		},
		{
			name: "spot with no spot price on the row omits the price segment",
			opt: ComputeOption{
				Name: "RTX PRO 6000", Size: 2, Available: true, Enriched: true,
				TotalVRAMGB: 192, HourlyPrice: 4.16,
			},
			spot: true,
			want: "RTX PRO 6000 · 2× · 192 GB VRAM",
		},
		{
			name: "enriched CPU flavor omits the VRAM segment",
			opt:  ComputeOption{Name: "CPU Node", Size: 8, Available: true, Enriched: true, HourlyPrice: 0.06, HourlySpotPrice: 0.03},
			want: "CPU Node · 8× · $0.06/h",
		},
		{
			name: "enriched CPU flavor on spot shows the spot price",
			opt:  ComputeOption{Name: "CPU Node", Size: 8, Available: true, Enriched: true, HourlyPrice: 0.06, HourlySpotPrice: 0.03},
			spot: true,
			want: "CPU Node · 8× · $0.03/h",
		},
		{
			name: "API rounding is respected verbatim (16.63, not 16.64)",
			opt:  ComputeOption{Name: "RTX PRO 6000", Size: 8, Available: true, Enriched: true, TotalVRAMGB: 768, HourlyPrice: 16.63},
			want: "RTX PRO 6000 · 8× · 768 GB VRAM · $16.63/h",
		},
		{
			name: "enriched unavailable carries the marker at the tail",
			opt:  ComputeOption{Name: "B200", Size: 8, Available: false, Enriched: true, TotalVRAMGB: 1440, HourlyPrice: 53.77},
			want: "B200 · 8× · 1440 GB VRAM · $53.77/h · unavailable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := computeOptionLabel(&tc.opt, tc.spot); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Real production captures, 2026-08-11 (verbatim copies of
// temp/agent/fixtures/*.json, inlined because temp/ is gitignored and tests
// must not depend on it).
const realTypesJSON = `[
  {
    "id": "b300b300-b300-4000-8001-b300b300b301",
    "model": "B300",
    "name": "B300 SXM6 268GB",
    "instance_type": "1B300.28V",
    "cpu": {
      "description": "28 CPU",
      "number_of_cores": 28
    },
    "gpu": {
      "description": "1x B300 SXM6 268GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "268GB GPU RAM",
      "size_in_gigabytes": 268
    },
    "memory": {
      "description": "250GB RAM",
      "size_in_gigabytes": 250
    },
    "serverless_price": 8.25,
    "serverless_spot_price": 4.13,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b300b300-b300-4000-8001-b300b300b302",
    "model": "B300",
    "name": "B300 SXM6 268GB",
    "instance_type": "2B300.56V",
    "cpu": {
      "description": "56 CPU",
      "number_of_cores": 56
    },
    "gpu": {
      "description": "2x B300 SXM6 268GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "536GB GPU RAM",
      "size_in_gigabytes": 536
    },
    "memory": {
      "description": "500GB RAM",
      "size_in_gigabytes": 500
    },
    "serverless_price": 16.5,
    "serverless_spot_price": 8.25,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b300b300-b300-4000-8001-b300b300b304",
    "model": "B300",
    "name": "B300 SXM6 268GB",
    "instance_type": "4B300.112V",
    "cpu": {
      "description": "112 CPU",
      "number_of_cores": 112
    },
    "gpu": {
      "description": "4x B300 SXM6 268GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "1072GB GPU RAM",
      "size_in_gigabytes": 1072
    },
    "memory": {
      "description": "1000GB RAM",
      "size_in_gigabytes": 1000
    },
    "serverless_price": 33,
    "serverless_spot_price": 16.5,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b300b300-b300-4000-8001-b300b300b308",
    "model": "B300",
    "name": "B300 SXM6 268GB",
    "instance_type": "8B300.224V",
    "cpu": {
      "description": "224 CPU",
      "number_of_cores": 224
    },
    "gpu": {
      "description": "8x B300 SXM6 268GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "2144GB GPU RAM",
      "size_in_gigabytes": 2144
    },
    "memory": {
      "description": "2000GB RAM",
      "size_in_gigabytes": 2000
    },
    "serverless_price": 66,
    "serverless_spot_price": 33,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b2000001-0000-447d-a68e-0b694079a4fb",
    "model": "B200",
    "name": "B200 SXM6 180GB",
    "instance_type": "1B200.28V",
    "cpu": {
      "description": "28 CPU",
      "number_of_cores": 28
    },
    "gpu": {
      "description": "1x B200 SXM6 180GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "180GB GPU RAM",
      "size_in_gigabytes": 180
    },
    "memory": {
      "description": "165GB RAM",
      "size_in_gigabytes": 165
    },
    "serverless_price": 6.72,
    "serverless_spot_price": 3.36,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b2000002-0000-447d-a68e-0b694079a4fb",
    "model": "B200",
    "name": "B200 SXM6 180GB",
    "instance_type": "2B200.56V",
    "cpu": {
      "description": "56 CPU",
      "number_of_cores": 56
    },
    "gpu": {
      "description": "2x B200 SXM6 180GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "360GB GPU RAM",
      "size_in_gigabytes": 360
    },
    "memory": {
      "description": "330GB RAM",
      "size_in_gigabytes": 330
    },
    "serverless_price": 13.44,
    "serverless_spot_price": 6.72,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b2000004-0000-447d-a68e-0b694079a4fb",
    "model": "B200",
    "name": "B200 SXM6 180GB",
    "instance_type": "4B200.112V",
    "cpu": {
      "description": "112 CPU",
      "number_of_cores": 112
    },
    "gpu": {
      "description": "4x B200 SXM6 180GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "720GB GPU RAM",
      "size_in_gigabytes": 720
    },
    "memory": {
      "description": "660GB RAM",
      "size_in_gigabytes": 660
    },
    "serverless_price": 26.88,
    "serverless_spot_price": 13.44,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b2000008-0000-447d-a68e-0b694079a4fb",
    "model": "B200",
    "name": "B200 SXM6 180GB",
    "instance_type": "8B200.224V",
    "cpu": {
      "description": "224 CPU",
      "number_of_cores": 224
    },
    "gpu": {
      "description": "8x B200 SXM6 180GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "1440GB GPU RAM",
      "size_in_gigabytes": 1440
    },
    "memory": {
      "description": "1320GB RAM",
      "size_in_gigabytes": 1320
    },
    "serverless_price": 53.77,
    "serverless_spot_price": 26.88,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b01dd00d-0000-4111-8111-111111111111",
    "model": "H200",
    "name": "H200 SXM5 141GB",
    "instance_type": "1H200.141S.21V",
    "cpu": {
      "description": "21 CPU",
      "number_of_cores": 21
    },
    "gpu": {
      "description": "1x H200 SXM5 141GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "141GB GPU RAM",
      "size_in_gigabytes": 141
    },
    "memory": {
      "description": "175GB RAM",
      "size_in_gigabytes": 175
    },
    "serverless_price": 4.4,
    "serverless_spot_price": 2.2,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b01dd00d-0001-4111-8111-111111111111",
    "model": "H200",
    "name": "H200 SXM5 141GB",
    "instance_type": "2H200.141S.42V",
    "cpu": {
      "description": "42 CPU",
      "number_of_cores": 42
    },
    "gpu": {
      "description": "2x H200 SXM5 141GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "282GB GPU RAM",
      "size_in_gigabytes": 282
    },
    "memory": {
      "description": "350GB RAM",
      "size_in_gigabytes": 350
    },
    "serverless_price": 8.8,
    "serverless_spot_price": 4.4,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b01dd00d-0002-4111-8111-111111111111",
    "model": "H200",
    "name": "H200 SXM5 141GB",
    "instance_type": "4H200.141S.84V",
    "cpu": {
      "description": "84 CPU",
      "number_of_cores": 84
    },
    "gpu": {
      "description": "4x H200 SXM5 141GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "564GB GPU RAM",
      "size_in_gigabytes": 564
    },
    "memory": {
      "description": "700GB RAM",
      "size_in_gigabytes": 700
    },
    "serverless_price": 17.6,
    "serverless_spot_price": 8.8,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "b01dd00d-0003-4111-8111-111111111111",
    "model": "H200",
    "name": "H200 SXM5 141GB",
    "instance_type": "8H200.141S.168V",
    "cpu": {
      "description": "168 CPU",
      "number_of_cores": 168
    },
    "gpu": {
      "description": "8x H200 SXM5 141GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "1128GB GPU RAM",
      "size_in_gigabytes": 1128
    },
    "memory": {
      "description": "1400GB RAM",
      "size_in_gigabytes": 1400
    },
    "serverless_price": 35.2,
    "serverless_spot_price": 17.6,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "c01dd00d-0000-4111-8111-111111111111",
    "model": "H100",
    "name": "H100 SXM5 80GB",
    "instance_type": "1H100.80S.21V",
    "cpu": {
      "description": "21 CPU",
      "number_of_cores": 21
    },
    "gpu": {
      "description": "1x H100 SXM5 80GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "80GB GPU RAM",
      "size_in_gigabytes": 80
    },
    "memory": {
      "description": "175GB RAM",
      "size_in_gigabytes": 175
    },
    "serverless_price": 3.58,
    "serverless_spot_price": 1.79,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "c01dd00d-0001-4111-8111-111111111111",
    "model": "H100",
    "name": "H100 SXM5 80GB",
    "instance_type": "2H100.80S.42V",
    "cpu": {
      "description": "42 CPU",
      "number_of_cores": 42
    },
    "gpu": {
      "description": "2x H100 SXM5 80GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "160GB GPU RAM",
      "size_in_gigabytes": 160
    },
    "memory": {
      "description": "350GB RAM",
      "size_in_gigabytes": 350
    },
    "serverless_price": 7.15,
    "serverless_spot_price": 3.58,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "c01dd00d-0002-4111-8111-111111111111",
    "model": "H100",
    "name": "H100 SXM5 80GB",
    "instance_type": "4H100.80S.84V",
    "cpu": {
      "description": "84 CPU",
      "number_of_cores": 84
    },
    "gpu": {
      "description": "4x H100 SXM5 80GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "320GB GPU RAM",
      "size_in_gigabytes": 320
    },
    "memory": {
      "description": "700GB RAM",
      "size_in_gigabytes": 700
    },
    "serverless_price": 14.3,
    "serverless_spot_price": 7.15,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "c01dd00d-0003-4111-8111-111111111111",
    "model": "H100",
    "name": "H100 SXM5 80GB",
    "instance_type": "8H100.80S.168V",
    "cpu": {
      "description": "168 CPU",
      "number_of_cores": 168
    },
    "gpu": {
      "description": "8x H100 SXM5 80GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "640GB GPU RAM",
      "size_in_gigabytes": 640
    },
    "memory": {
      "description": "1400GB RAM",
      "size_in_gigabytes": 1400
    },
    "serverless_price": 28.6,
    "serverless_spot_price": 14.3,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "60006000-6000-47af-8ff0-600060006001",
    "model": "RTX PRO 6000",
    "name": "RTX PRO 6000 96GB",
    "instance_type": "1RTXPRO6000.28V",
    "cpu": {
      "description": "28 CPU",
      "number_of_cores": 28
    },
    "gpu": {
      "description": "1x RTX PRO 6000 96GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "96GB GPU RAM",
      "size_in_gigabytes": 96
    },
    "memory": {
      "description": "85GB RAM",
      "size_in_gigabytes": 85
    },
    "serverless_price": 2.08,
    "serverless_spot_price": 1.04,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "60006000-6000-47af-8ff0-600060006002",
    "model": "RTX PRO 6000",
    "name": "RTX PRO 6000 96GB",
    "instance_type": "2RTXPRO6000.56V",
    "cpu": {
      "description": "56 CPU",
      "number_of_cores": 56
    },
    "gpu": {
      "description": "2x RTX PRO 6000 96GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "192GB GPU RAM",
      "size_in_gigabytes": 192
    },
    "memory": {
      "description": "170GB RAM",
      "size_in_gigabytes": 170
    },
    "serverless_price": 4.16,
    "serverless_spot_price": 2.08,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "60006000-6000-47af-8ff0-600060006003",
    "model": "RTX PRO 6000",
    "name": "RTX PRO 6000 96GB",
    "instance_type": "4RTXPRO6000.112V",
    "cpu": {
      "description": "112 CPU",
      "number_of_cores": 112
    },
    "gpu": {
      "description": "4x RTX PRO 6000 96GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "384GB GPU RAM",
      "size_in_gigabytes": 384
    },
    "memory": {
      "description": "340GB RAM",
      "size_in_gigabytes": 340
    },
    "serverless_price": 8.32,
    "serverless_spot_price": 4.16,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "60006000-6000-47af-8ff0-600060006004",
    "model": "RTX PRO 6000",
    "name": "RTX PRO 6000 96GB",
    "instance_type": "8RTXPRO6000.224V",
    "cpu": {
      "description": "224 CPU",
      "number_of_cores": 224
    },
    "gpu": {
      "description": "8x RTX PRO 6000 96GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "768GB GPU RAM",
      "size_in_gigabytes": 768
    },
    "memory": {
      "description": "680GB RAM",
      "size_in_gigabytes": 680
    },
    "serverless_price": 16.63,
    "serverless_spot_price": 8.32,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "40000000-a5d3-4972-ae4e-d429115d055b",
    "model": "L40S",
    "name": "L40S 48GB",
    "instance_type": "1L40S.20V.58G",
    "cpu": {
      "description": "20 CPU",
      "number_of_cores": 20
    },
    "gpu": {
      "description": "1x L40S 48GB",
      "number_of_gpus": 1
    },
    "gpu_memory": {
      "description": "48GB GPU RAM",
      "size_in_gigabytes": 48
    },
    "memory": {
      "description": "58GB RAM",
      "size_in_gigabytes": 58
    },
    "serverless_price": 1.51,
    "serverless_spot_price": 0.75,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "40000001-a5d3-4972-ae4e-d429115d055b",
    "model": "L40S",
    "name": "L40S 48GB",
    "instance_type": "2L40S.40V.116G",
    "cpu": {
      "description": "40 CPU",
      "number_of_cores": 40
    },
    "gpu": {
      "description": "2x L40S 48GB",
      "number_of_gpus": 2
    },
    "gpu_memory": {
      "description": "96GB GPU RAM",
      "size_in_gigabytes": 96
    },
    "memory": {
      "description": "116GB RAM",
      "size_in_gigabytes": 116
    },
    "serverless_price": 3.01,
    "serverless_spot_price": 1.51,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "40000002-a5d3-4972-ae4e-d429115d055b",
    "model": "L40S",
    "name": "L40S 48GB",
    "instance_type": "4L40S.80V.232G",
    "cpu": {
      "description": "80 CPU",
      "number_of_cores": 80
    },
    "gpu": {
      "description": "4x L40S 48GB",
      "number_of_gpus": 4
    },
    "gpu_memory": {
      "description": "192GB GPU RAM",
      "size_in_gigabytes": 192
    },
    "memory": {
      "description": "232GB RAM",
      "size_in_gigabytes": 232
    },
    "serverless_price": 6.03,
    "serverless_spot_price": 3.01,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "40000003-a5d3-4972-ae4e-d429115d055b",
    "model": "L40S",
    "name": "L40S 48GB",
    "instance_type": "8L40S.160V.464G",
    "cpu": {
      "description": "160 CPU",
      "number_of_cores": 160
    },
    "gpu": {
      "description": "8x L40S 48GB",
      "number_of_gpus": 8
    },
    "gpu_memory": {
      "description": "384GB GPU RAM",
      "size_in_gigabytes": 384
    },
    "memory": {
      "description": "464GB RAM",
      "size_in_gigabytes": 464
    },
    "serverless_price": 12.06,
    "serverless_spot_price": 6.03,
    "currency": "usd",
    "manufacturer": "NVIDIA"
  },
  {
    "id": "ccc00008-0000-4000-8000-007c90031a75",
    "model": "CPU Node",
    "name": "AMD EPYC",
    "instance_type": "CPU.8V.32GC",
    "cpu": {
      "description": "8 CPU",
      "number_of_cores": 8
    },
    "gpu": {
      "description": "",
      "number_of_gpus": 0
    },
    "gpu_memory": {
      "description": "",
      "size_in_gigabytes": 0
    },
    "memory": {
      "description": "32GB RAM",
      "size_in_gigabytes": 32
    },
    "serverless_price": 0.06,
    "serverless_spot_price": 0.03,
    "currency": "usd",
    "manufacturer": "AMD"
  },
  {
    "id": "ccc00016-0000-4000-8000-015c80063a50",
    "model": "CPU Node",
    "name": "AMD EPYC",
    "instance_type": "CPU.16V.64GC",
    "cpu": {
      "description": "16 CPU",
      "number_of_cores": 16
    },
    "gpu": {
      "description": "",
      "number_of_gpus": 0
    },
    "gpu_memory": {
      "description": "",
      "size_in_gigabytes": 0
    },
    "memory": {
      "description": "64GB RAM",
      "size_in_gigabytes": 64
    },
    "serverless_price": 0.12,
    "serverless_spot_price": 0.06,
    "currency": "usd",
    "manufacturer": "AMD"
  },
  {
    "id": "ccc00032-0000-4000-8000-031c50127a00",
    "model": "CPU Node",
    "name": "AMD EPYC",
    "instance_type": "CPU.32V.128GC",
    "cpu": {
      "description": "32 CPU",
      "number_of_cores": 32
    },
    "gpu": {
      "description": "",
      "number_of_gpus": 0
    },
    "gpu_memory": {
      "description": "",
      "size_in_gigabytes": 0
    },
    "memory": {
      "description": "128GB RAM",
      "size_in_gigabytes": 128
    },
    "serverless_price": 0.25,
    "serverless_spot_price": 0.12,
    "currency": "usd",
    "manufacturer": "AMD"
  }
]`

const realResourcesJSON = `[
  {
    "name": "A100 40GB",
    "size": 1,
    "is_available": false
  },
  {
    "name": "A100 40GB",
    "size": 2,
    "is_available": false
  },
  {
    "name": "A100 40GB",
    "size": 4,
    "is_available": false
  },
  {
    "name": "A100 40GB",
    "size": 8,
    "is_available": false
  },
  {
    "name": "A100 80GB",
    "size": 1,
    "is_available": false
  },
  {
    "name": "A100 80GB",
    "size": 2,
    "is_available": false
  },
  {
    "name": "A100 80GB",
    "size": 4,
    "is_available": false
  },
  {
    "name": "A100 80GB",
    "size": 8,
    "is_available": false
  },
  {
    "name": "B200",
    "size": 1,
    "is_available": true
  },
  {
    "name": "B200",
    "size": 2,
    "is_available": true
  },
  {
    "name": "B200",
    "size": 4,
    "is_available": false
  },
  {
    "name": "B200",
    "size": 8,
    "is_available": false
  },
  {
    "name": "B300",
    "size": 1,
    "is_available": true
  },
  {
    "name": "B300",
    "size": 2,
    "is_available": true
  },
  {
    "name": "B300",
    "size": 4,
    "is_available": true
  },
  {
    "name": "B300",
    "size": 8,
    "is_available": false
  },
  {
    "name": "H100",
    "size": 1,
    "is_available": true
  },
  {
    "name": "H100",
    "size": 2,
    "is_available": true
  },
  {
    "name": "H100",
    "size": 4,
    "is_available": false
  },
  {
    "name": "H100",
    "size": 8,
    "is_available": false
  },
  {
    "name": "H200",
    "size": 1,
    "is_available": true
  },
  {
    "name": "H200",
    "size": 2,
    "is_available": true
  },
  {
    "name": "H200",
    "size": 4,
    "is_available": true
  },
  {
    "name": "H200",
    "size": 8,
    "is_available": false
  },
  {
    "name": "L40S",
    "size": 1,
    "is_available": true
  },
  {
    "name": "L40S",
    "size": 2,
    "is_available": true
  },
  {
    "name": "L40S",
    "size": 4,
    "is_available": true
  },
  {
    "name": "L40S",
    "size": 8,
    "is_available": false
  },
  {
    "name": "RTX 6000 Ada",
    "size": 1,
    "is_available": true
  },
  {
    "name": "RTX 6000 Ada",
    "size": 2,
    "is_available": true
  },
  {
    "name": "RTX 6000 Ada",
    "size": 4,
    "is_available": true
  },
  {
    "name": "RTX 6000 Ada",
    "size": 8,
    "is_available": false
  },
  {
    "name": "RTX PRO 6000",
    "size": 1,
    "is_available": true
  },
  {
    "name": "RTX PRO 6000",
    "size": 2,
    "is_available": true
  },
  {
    "name": "RTX PRO 6000",
    "size": 4,
    "is_available": true
  },
  {
    "name": "RTX PRO 6000",
    "size": 8,
    "is_available": false
  },
  {
    "name": "CPU Node",
    "size": 8,
    "is_available": true
  },
  {
    "name": "CPU Node",
    "size": 16,
    "is_available": true
  },
  {
    "name": "CPU Node",
    "size": 32,
    "is_available": true
  }
]`

// The CORRECTION-2 pin: against the 2026-08-11 production capture, exactly 12
// of 39 resource rows have no matching type (A100 40GB ×4, A100 80GB ×4,
// RTX 6000 Ada ×4) and exactly 3 of those are available (Ada sizes 1/2/4).
// A future capture that changes these numbers must fail here loudly and be
// updated deliberately.
func TestJoinComputeCatalog_RealFixtureCounts(t *testing.T) {
	var types []verda.ContainerType
	if err := json.Unmarshal([]byte(realTypesJSON), &types); err != nil {
		t.Fatalf("types fixture does not decode: %v", err)
	}
	var res []verda.ComputeResource
	if err := json.Unmarshal([]byte(realResourcesJSON), &res); err != nil {
		t.Fatalf("resources fixture does not decode: %v", err)
	}

	got := joinComputeCatalog(types, res)
	if len(got) != 39 {
		t.Fatalf("got %d options, want 39 (one per resource row)", len(got))
	}
	unenriched, unenrichedAvailable := 0, 0
	enriched := 0
	for i := range got {
		if got[i].Enriched {
			enriched++
			continue
		}
		unenriched++
		if got[i].Available {
			unenrichedAvailable++
		}
	}
	if enriched != 27 {
		t.Fatalf("enriched = %d, want 27", enriched)
	}
	if unenriched != 12 {
		t.Fatalf("unenriched = %d, want 12 (A100 x8, RTX 6000 Ada x4)", unenriched)
	}
	if unenrichedAvailable != 3 {
		t.Fatalf("unenriched but available = %d, want 3 (RTX 6000 Ada sizes 1/2/4)", unenrichedAvailable)
	}
}

// The store key stepCompute reads. Step values land in Collected(), not in
// Get()'s data map (engine writes via SetCollected).
func TestSpotSelected(t *testing.T) {
	if spotSelected(wizard.NewStore()) {
		t.Fatal("missing compute-type key must mean on-demand (batchjob has no such step)")
	}
	spot := wizard.NewStore()
	spot.SetCollected("compute-type", computeTypeSpot)
	if !spotSelected(spot) {
		t.Fatal("spot selection not detected")
	}
	onDemand := wizard.NewStore()
	onDemand.SetCollected("compute-type", computeTypeOnDemand)
	if spotSelected(onDemand) {
		t.Fatal("explicit on-demand read as spot")
	}
}

// Drives stepCompute's Loader against a local server shaped by the real
// production captures, in both price directions. Covers the review's F1/F2:
// spot flips the price shown, unavailable enriched rows carry the marker,
// unenriched rows carry the gap note.
func TestStepComputeLoader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("GET /serverless-compute-resources", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, realResourcesJSON)
	})
	mux.HandleFunc("GET /container-types", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, realTypesJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithBaseURL(srv.URL),
		verda.WithClientID("test"),
		verda.WithClientSecret("test"),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	getClient := func() (*verda.Client, error) { return client, nil }

	var target string
	step := stepCompute(getClient, &apiCache{}, &target)

	// Back-navigation price refresh only happens if the engine knows the
	// dependency; pin the declaration (the reset logic itself is engine-tested).
	if !slices.Contains(step.DependsOn, "compute-type") {
		t.Fatalf(`step compute must declare DependsOn ["compute-type"], has %v`, step.DependsOn)
	}

	labelStartingWith := func(choices []wizard.Choice, prefix string) string {
		t.Helper()
		for i := range choices {
			if strings.HasPrefix(choices[i].Label, prefix) {
				return choices[i].Label
			}
		}
		t.Fatalf("no choice label starts with %q; have %d choices", prefix, len(choices))
		return ""
	}

	onDemand, err := step.Loader(context.Background(), nil, nil, wizard.NewStore())
	if err != nil {
		t.Fatalf("loader: %v", err)
	}
	if got := labelStartingWith(onDemand, "RTX PRO 6000 · 1×"); !strings.Contains(got, "$2.08/h") {
		t.Fatalf("on-demand RTX PRO 6000 1x label = %q, want $2.08/h", got)
	}
	if got := labelStartingWith(onDemand, "B200 · 8×"); !strings.HasSuffix(got, " · unavailable") {
		t.Fatalf("unavailable B200 8x label = %q, want the unavailable marker", got)
	}
	if got := labelStartingWith(onDemand, "RTX 6000 Ada  (size 1)"); !strings.Contains(got, "VRAM and price unavailable") {
		t.Fatalf("unenriched Ada label = %q, want the gap note", got)
	}

	spotStore := wizard.NewStore()
	spotStore.SetCollected("compute-type", computeTypeSpot)
	var target2 string
	spotChoices, err := stepCompute(getClient, &apiCache{}, &target2).Loader(context.Background(), nil, nil, spotStore)
	if err != nil {
		t.Fatalf("spot loader: %v", err)
	}
	if got := labelStartingWith(spotChoices, "RTX PRO 6000 · 1×"); !strings.Contains(got, "$1.04/h") {
		t.Fatalf("spot RTX PRO 6000 1x label = %q, want $1.04/h", got)
	}
	if got := labelStartingWith(spotChoices, "B300 · 4×"); !strings.Contains(got, "$16.50/h") {
		t.Fatalf("spot B300 4x label = %q, want $16.50/h (API value, not half of 33)", got)
	}
}
