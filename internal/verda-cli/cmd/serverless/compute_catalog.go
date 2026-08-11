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
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// ComputeOption is one selectable (flavor, size) pair with everything the
// picker needs. Assembled by joining the availability list with the type
// catalog; never constructed from one source alone.
type ComputeOption struct {
	Name         string // → CreateDeploymentRequest.Compute.Name
	Size         int    // → CreateDeploymentRequest.Compute.Size (GPU or vCPU count)
	Available    bool
	Manufacturer string
	TotalVRAMGB  int // VRAM total for this (flavor, size), read from the API row — never computed
	VRAMPerGPUGB int // TotalVRAMGB / gpus (division only); 0 for CPU flavors and unknowns
	// Per-row totals from the API, rendered as-is — never computed client-side:
	// 8× RTX PRO 6000 is 16.63, not 2.08×8 = 16.64 (the API rounds per row).
	HourlyPrice     float64
	HourlySpotPrice float64
	Enriched        bool // false = no matching ContainerType; label degrades
}

// joinComputeCatalog joins /serverless-compute-resources (the spine — what
// may actually be selected) with /container-types (enrichment only). Pure:
// no context, no client, no I/O.
//
// Join key verified against captured production payloads: the endpoints share
// NO name vocabulary — resources say `RTX PRO 6000`, types say it in `model`
// (`RTX PRO 6000`) while their `name` carries the display form
// (`RTX PRO 6000 96GB`). So key = (ContainerType.Model == ComputeResource.Name)
// AND the size axis: `gpu.number_of_gpus` for GPU flavors, `cpu.number_of_cores`
// for CPU flavors (their gpu count is 0). Availability varies per size, so both
// halves of the key matter. A resource row with no matching type (A100s,
// RTX 6000 Ada in the 2026-08-11 capture) degrades to Enriched: false.
func joinComputeCatalog(types []verda.ContainerType, res []verda.ComputeResource) []ComputeOption {
	byKey := make(map[catalogKey]verda.ContainerType, len(types))
	for i := range types {
		t := &types[i]
		units := t.GPU.NumberOfGPUs
		if units == 0 {
			units = t.CPU.NumberOfCores
		}
		key := catalogKey{name: t.Model, size: units}
		if _, dup := byKey[key]; !dup {
			byKey[key] = *t
		}
	}

	seen := make(map[catalogKey]struct{}, len(res))
	opts := make([]ComputeOption, 0, len(res))
	for i := range res {
		r := &res[i]
		key := catalogKey{name: r.Name, size: r.Size}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		opt := ComputeOption{Name: r.Name, Size: r.Size, Available: r.IsAvailable}
		if t, ok := byKey[key]; ok {
			opt.Enriched = true
			opt.Manufacturer = t.Manufacturer
			opt.TotalVRAMGB = t.GPUMemory.SizeInGigabytes
			if gpus := t.GPU.NumberOfGPUs; gpus > 0 {
				opt.VRAMPerGPUGB = opt.TotalVRAMGB / gpus
			}
			opt.HourlyPrice = float64(t.ServerlessPrice)
			opt.HourlySpotPrice = float64(t.ServerlessSpotPrice)
		}
		opts = append(opts, opt)
	}

	slices.SortStableFunc(opts, func(a, b ComputeOption) int {
		if a.Available != b.Available {
			if a.Available {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(a.TotalVRAMGB, b.TotalVRAMGB),
			strings.Compare(a.Name, b.Name),
			cmp.Compare(a.Size, b.Size),
		)
	})
	return opts
}

type catalogKey struct {
	name string
	size int
}

// computeOptionLabel renders the picker label. Degraded rows keep the legacy
// "name  (size N)" text plus a gap note: the wizard's select renders labels
// only (Choice.Description never reaches the output), so the label is the only
// channel where missing enrichment or unavailability is visible at the point
// of decision. spot selects the spot price; a missing spot price omits the
// segment rather than show the on-demand figure. Prices render at 2 decimals —
// sub-cent prices would show $0.00; accepted (cheapest spot today is $0.03).
func computeOptionLabel(o *ComputeOption, spot bool) string {
	var label string
	if !o.Enriched {
		label = o.Name + "  (size " + strconv.Itoa(o.Size) + ") · VRAM and price unavailable"
	} else {
		label = o.Name + " · " + strconv.Itoa(o.Size) + "×"
		if o.TotalVRAMGB > 0 {
			label += " · " + strconv.Itoa(o.TotalVRAMGB) + " GB VRAM"
		}
		price := o.HourlyPrice
		if spot {
			price = o.HourlySpotPrice
		}
		if price > 0 {
			label += " · $" + strconv.FormatFloat(price, 'f', 2, 64) + "/h"
		}
	}
	if !o.Available {
		label += " · unavailable"
	}
	return label
}
