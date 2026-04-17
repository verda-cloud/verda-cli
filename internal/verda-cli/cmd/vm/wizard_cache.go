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

package vm

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	"github.com/verda-cloud/verdagostack/pkg/tui/wizard"
)

// apiCache holds data fetched from the API, shared across wizard steps
// to avoid redundant calls within a single wizard session.
type apiCache struct {
	avail         []verda.LocationAvailability
	locations     map[string]verda.Location
	cachedSpot    bool // tracks which isSpot value was cached
	loaded        bool
	instanceTypes map[string]verda.InstanceTypeInfo // keyed by instance type name
	volumeTypes   map[string]verda.VolumeType       // keyed by volume type name
}

// fetchAvailability loads availability and location data, caching the result.
// Cache is invalidated if isSpot changes (user switched billing type).
func (c *apiCache) fetchAvailability(ctx context.Context, getClient clientFunc, isSpot bool) ([]verda.LocationAvailability, map[string]verda.Location, error) {
	if c.loaded && c.cachedSpot == isSpot {
		return c.avail, c.locations, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, nil, err
	}
	avail, err := client.InstanceAvailability.GetAllAvailabilities(ctx, isSpot, "")
	if err != nil {
		return nil, nil, fmt.Errorf("fetching availability: %w", err)
	}
	locations, err := client.Locations.Get(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching locations: %w", err)
	}
	c.locations = make(map[string]verda.Location, len(locations))
	for _, loc := range locations {
		c.locations[loc.Code] = loc
	}
	c.avail = avail
	c.cachedSpot = isSpot
	c.loaded = true
	return c.avail, c.locations, nil
}

// fetchLocations loads location data without availability, caching the result.
func (c *apiCache) fetchLocations(ctx context.Context, getClient clientFunc) (map[string]verda.Location, error) {
	if c.locations != nil {
		return c.locations, nil
	}
	client, err := getClient()
	if err != nil {
		return nil, err
	}
	locations, err := client.Locations.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching locations: %w", err)
	}
	c.locations = make(map[string]verda.Location, len(locations))
	for _, loc := range locations {
		c.locations[loc.Code] = loc
	}
	return c.locations, nil
}

// ensurePricingCache populates the apiCache with instance type and volume
// type pricing data if it's missing. This happens when wizard steps were
// skipped because a template pre-filled the values.
func ensurePricingCache(ctx context.Context, getClient clientFunc, cache *apiCache) {
	client, err := getClient()
	if err != nil {
		return
	}

	// Fetch instance types if missing.
	if cache.instanceTypes == nil {
		if types, err := client.InstanceTypes.Get(ctx, "usd"); err == nil {
			cache.instanceTypes = make(map[string]verda.InstanceTypeInfo, len(types))
			for i := range types {
				cache.instanceTypes[types[i].InstanceType] = types[i]
			}
		}
	}

	// Fetch volume types if missing.
	if cache.volumeTypes == nil {
		if vTypes, err := client.VolumeTypes.GetAllVolumeTypes(ctx); err == nil {
			cache.volumeTypes = make(map[string]verda.VolumeType, len(vTypes))
			for _, vt := range vTypes {
				cache.volumeTypes[vt.Type] = vt
			}
		}
	}
}

// hoursInMonth is 365*24/12 = 730, matching the web frontend.
const hoursInMonth = 730

// volumeHourlyPrice calculates hourly price: monthlyPerGB * size / 730, rounded up to 4 decimals.
func volumeHourlyPrice(monthlyPerGB float64, sizeGB int) float64 {
	return math.Ceil(monthlyPerGB*float64(sizeGB)/hoursInMonth*10000) / 10000
}

// --- Location loaders ---

// loadAllLocations returns all locations with a skip option (for template mode).
func loadAllLocations(ctx context.Context, cache *apiCache, getClient clientFunc) ([]wizard.Choice, error) {
	choices := []wizard.Choice{{Label: "None (decide at deploy time)", Value: ""}}
	locMap, err := cache.fetchLocations(ctx, getClient)
	if err != nil {
		return nil, err
	}
	for _, loc := range locMap {
		choices = append(choices, wizard.Choice{
			Label: fmt.Sprintf("%s (%s)", loc.Code, loc.Name),
			Value: loc.Code,
		})
	}
	return choices, nil
}

// loadAvailableLocations returns locations where instType is available (for deploy mode).
func loadAvailableLocations(ctx context.Context, cache *apiCache, getClient clientFunc, isSpot bool, instType string) ([]wizard.Choice, error) {
	avail, locMap, err := cache.fetchAvailability(ctx, getClient, isSpot)
	if err != nil {
		return nil, err
	}
	var choices []wizard.Choice
	for _, la := range avail {
		if slices.Contains(la.Availabilities, instType) {
			loc := locMap[la.LocationCode]
			choices = append(choices, wizard.Choice{
				Label: fmt.Sprintf("%s (%s)", loc.Code, loc.Name),
				Value: loc.Code,
			})
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("instance type %s is not available in any location right now — try again later or choose a different instance type", instType)
	}
	return choices, nil
}

// --- Instance type choices ---

// buildInstanceTypeChoices filters and formats instance types into wizard choices.
func buildInstanceTypeChoices(types []verda.InstanceTypeInfo, kind string, isSpot bool, availLocs map[string][]string, cache *apiCache) []wizard.Choice {
	choices := make([]wizard.Choice, 0, len(types))
	for i := range types {
		t := &types[i]
		if !matchesKind(t.InstanceType, kind) {
			continue
		}
		if availLocs != nil && len(availLocs[t.InstanceType]) == 0 {
			continue
		}
		totalPrice := float64(t.PricePerHour)
		if isSpot {
			totalPrice = float64(t.SpotPrice)
		}
		units := instanceUnits(t)
		var priceStr string
		if units > 1 {
			unitLabel := unitLabelGPU
			if t.GPU.NumberOfGPUs == 0 {
				unitLabel = unitLabelVCPU
			}
			perUnit := totalPrice / float64(units)
			priceStr = fmt.Sprintf("$%.3f/%s/hr  $%.3f/hr", perUnit, unitLabel, totalPrice)
		} else {
			priceStr = fmt.Sprintf("$%.3f/hr", totalPrice)
		}
		label := fmt.Sprintf("%s — %s, %s  %s",
			t.InstanceType, formatGPU(t), formatMemory(t), priceStr)
		var desc string
		if availLocs != nil {
			locs := availLocs[t.InstanceType]
			locNames := make([]string, len(locs))
			for j, code := range locs {
				if loc, ok := cache.locations[code]; ok {
					locNames[j] = loc.Code
				} else {
					locNames[j] = code
				}
			}
			desc = fmt.Sprintf("[%s]", strings.Join(locNames, ", "))
		}
		choices = append(choices, wizard.Choice{
			Label:       label,
			Value:       t.InstanceType,
			Description: desc,
		})
	}
	return choices
}

// --- Helpers ---

// instanceUnits returns the number of billable units (GPUs or vCPUs).
func instanceUnits(t *verda.InstanceTypeInfo) int {
	if t.GPU.NumberOfGPUs > 0 {
		return t.GPU.NumberOfGPUs
	}
	return t.CPU.NumberOfCores
}

func matchesKind(instanceType, kind string) bool {
	isCPU := strings.HasPrefix(strings.ToUpper(instanceType), "CPU.")
	switch strings.ToLower(kind) {
	case "cpu":
		return isCPU
	case kindGPU:
		return !isCPU
	default:
		return true
	}
}

func formatGPU(t *verda.InstanceTypeInfo) string {
	if t.GPU.NumberOfGPUs > 0 {
		return fmt.Sprintf("%dx %s", t.GPU.NumberOfGPUs, t.GPU.Description)
	}
	return fmt.Sprintf("%d cores", t.CPU.NumberOfCores)
}

func formatMemory(t *verda.InstanceTypeInfo) string {
	if t.GPUMemory.SizeInGigabytes > 0 {
		return fmt.Sprintf("%dGB VRAM, %dGB RAM", t.GPUMemory.SizeInGigabytes, t.Memory.SizeInGigabytes)
	}
	return fmt.Sprintf("%dGB RAM", t.Memory.SizeInGigabytes)
}
