package vm

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
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
