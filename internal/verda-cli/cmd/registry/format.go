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

package registry

import (
	"fmt"
	"strconv"
	"time"
)

// formatBytes renders a byte count in binary-unit form (1 KiB = 1024 B),
// matching the `ls -h` convention used by most container tooling.
//
// Shared between tags.go (per-tag size column) and push_view.go (bubbletea
// progress bar). Previously lived in tags.go as `formatTagBytes`; promoted
// to a package-level helper when the bubbletea push view was added.
//
// Rules:
//   - n < 0        -> "--" (unknown / not looked up)
//   - n < 1024     -> "<n> B" (no decimals)
//   - n < 10*unit  -> two decimals (e.g. "4.25 GiB")
//   - n < 100*unit -> one decimal  (e.g. "42.3 MiB")
//   - otherwise    -> zero decimals (e.g. "512 MiB")
func formatBytes(n int64) string {
	if n < 0 {
		return "--"
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value := float64(n) / unit
	idx := 0
	for value >= unit && idx < len(units)-1 {
		value /= unit
		idx++
	}
	switch {
	case value < 10:
		return fmt.Sprintf("%.2f %s", value, units[idx])
	case value < 100:
		return fmt.Sprintf("%.1f %s", value, units[idx])
	default:
		return fmt.Sprintf("%.0f %s", value, units[idx])
	}
}

// formatBytesPerSec renders a throughput (bytes/sec) using the same
// binary-unit scale as formatBytes, suffixing "/s".
//
// Returns "-- B/s" for negative (sentinel) rates and "0 B/s" for zero.
func formatBytesPerSec(bps float64) string {
	if bps < 0 {
		return "-- B/s"
	}
	if bps < 1 {
		return "0 B/s"
	}
	// Reuse the int64 formatter for consistency; cast after clamping.
	return formatBytes(int64(bps)) + "/s"
}

// formatMMSS renders a duration as MM:SS for durations under an hour and
// HH:MM:SS once it reaches one hour. Negative durations clamp to "00:00".
// Pure; no clock access.
func formatMMSS(d time.Duration) string {
	if d < 0 {
		return "00:00"
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
