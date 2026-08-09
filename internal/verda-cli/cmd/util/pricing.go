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

package util

import (
	"math"
	"sort"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
)

// HoursInMonth converts an hourly rate to a monthly estimate: 365*24/12,
// matching the web frontend's hoursInMonth.
const HoursInMonth = 730

// VolumeHourlyPrice converts volume pricing (monthlyPerGB per GiB) to the
// hourly rate for a sizeGB volume: monthlyPerGB*sizeGB spread over the month,
// rounded up to 4 decimals. The ceiling is applied AFTER multiplying by size —
// per-GB rounding would overstate the rate (match the web frontend exactly).
func VolumeHourlyPrice(monthlyPerGB float64, sizeGB int) float64 {
	return math.Ceil(monthlyPerGB*float64(sizeGB)/HoursInMonth*10000) / 10000
}

// VolumeMonthlyPrice returns the monthly price of a sizeGB volume.
func VolumeMonthlyPrice(monthlyPerGB float64, sizeGB int) float64 {
	return monthlyPerGB * float64(sizeGB)
}

// ValidVolumeTypeNames returns the sorted type names of a volume-type catalog,
// for error messages that list the accepted values.
func ValidVolumeTypeNames(vtMap map[string]verda.VolumeType) []string {
	names := make([]string, 0, len(vtMap))
	for name := range vtMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
