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
	"math"
	"testing"
	"time"
)

// fakeClock returns a now() func that yields a scripted sequence of times.
// Each call consumes one entry; if exhausted, the last time is reused.
func fakeClock(times ...time.Time) func() time.Time {
	i := 0
	return func() time.Time {
		if i >= len(times) {
			return times[len(times)-1]
		}
		t := times[i]
		i++
		return t
	}
}

// approxEq reports whether a and b are within epsilon.
func approxEq(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

func TestMeter_NoUpdatesYieldsZeroSnapshot(t *testing.T) {
	t.Parallel()
	var m Meter
	got := m.Snapshot()
	want := Snapshot{}
	if got != want {
		t.Fatalf("expected zero snapshot, got %+v", got)
	}
}

func TestMeter_SingleUpdateTotalZero(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	m := Meter{now: fakeClock(base)}
	m.Update(0, 0)
	got := m.Snapshot()
	if got.Fraction != 0 {
		t.Errorf("Fraction: want 0, got %v", got.Fraction)
	}
	if got.ETA != 0 {
		t.Errorf("ETA: want 0, got %v", got.ETA)
	}
	if got.ThroughputBps != 0 {
		t.Errorf("ThroughputBps: want 0, got %v", got.ThroughputBps)
	}
	if got.Complete != 0 || got.Total != 0 {
		t.Errorf("Complete/Total: want 0/0, got %d/%d", got.Complete, got.Total)
	}
}

func TestMeter_ProgressingWithKnownTotal(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	// Three Updates plus one Snapshot call: give Snapshot the last time too.
	m := Meter{now: fakeClock(
		base,
		base.Add(1*time.Second),
		base.Add(2*time.Second),
	)}
	m.Update(1000, 10000)
	m.Update(2000, 10000)
	m.Update(3000, 10000)

	got := m.Snapshot()
	if !approxEq(got.Fraction, 0.3, 1e-9) {
		t.Errorf("Fraction: want 0.3, got %v", got.Fraction)
	}
	if !approxEq(got.ThroughputBps, 1000, 1e-6) {
		t.Errorf("ThroughputBps: want ~1000, got %v", got.ThroughputBps)
	}
	if got.ETA != 7*time.Second {
		t.Errorf("ETA: want 7s, got %v", got.ETA)
	}
	if got.Elapsed != 2*time.Second {
		t.Errorf("Elapsed: want 2s, got %v", got.Elapsed)
	}
}

func TestMeter_CompletedReturnsZeroETA(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	m := Meter{now: fakeClock(
		base,
		base.Add(1*time.Second),
		base.Add(2*time.Second),
	)}
	m.Update(5000, 10000)
	m.Update(8000, 10000)
	m.Update(10000, 10000)

	got := m.Snapshot()
	if !approxEq(got.Fraction, 1.0, 1e-9) {
		t.Errorf("Fraction: want 1, got %v", got.Fraction)
	}
	if got.ETA != 0 {
		t.Errorf("ETA: want 0 on completion, got %v", got.ETA)
	}
}

func TestMeter_SlidingWindowDropsOldSamples(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	// First two samples are "slow" at t=0s, t=1s. Then we jump 10s forward
	// (well past the 5s window) and log fast progress at t=11s, t=12s.
	// Only the fast samples should remain in the window.
	m := Meter{now: fakeClock(
		base,                    // Update 1: (100, 0) slow, unknown total
		base.Add(1*time.Second), // Update 2: (200, 0) slow
		// Jump forward 10s. All previous samples fall outside window.
		base.Add(11*time.Second), // Update 3: (10_200, 0) — +10_000 bytes at fast speed from last slow sample
		base.Add(12*time.Second), // Update 4: (20_200, 0)
	)}
	m.Update(100, 0)
	m.Update(200, 0)
	m.Update(10_200, 0)
	m.Update(20_200, 0)

	got := m.Snapshot()
	// Samples within window: Update 3 and Update 4 (both within 5s of t=12s).
	// delta = 20_200 - 10_200 = 10_000 over 1s => 10_000 B/s.
	if !approxEq(got.ThroughputBps, 10_000, 1e-6) {
		t.Errorf("ThroughputBps: want ~10000 (window-local), got %v", got.ThroughputBps)
	}
}

func TestMeter_UnknownTotalKeepsFractionZero(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	m := Meter{now: fakeClock(
		base,
		base.Add(1*time.Second),
		base.Add(2*time.Second),
	)}
	m.Update(1000, 0)
	m.Update(2000, 0)
	m.Update(3000, 0)

	got := m.Snapshot()
	if got.Fraction != 0 {
		t.Errorf("Fraction: want 0 when Total=0, got %v", got.Fraction)
	}
	if got.ETA != 0 {
		t.Errorf("ETA: want 0 when Total=0, got %v", got.ETA)
	}
	if got.ThroughputBps <= 0 {
		t.Errorf("ThroughputBps: want > 0 with forward progress, got %v", got.ThroughputBps)
	}
}

func TestMeter_ElapsedTracksFirstUpdate(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	m := Meter{now: fakeClock(
		base,
		base.Add(5*time.Second),
	)}
	m.Update(100, 1000)
	m.Update(500, 1000) // second Update sets m.last; Elapsed = 5s.

	got := m.Snapshot()
	if got.Elapsed != 5*time.Second {
		t.Errorf("Elapsed: want 5s, got %v", got.Elapsed)
	}
}

func TestMeter_UpdateMonotonicity(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	m := Meter{now: fakeClock(
		base,
		base.Add(1*time.Second),
		base.Add(2*time.Second),
	)}
	// Regression: upload retry drops complete from 5000 back to 2000.
	m.Update(5000, 10000)
	m.Update(2000, 10000) // backwards; must not panic.
	// Sanity: snapshot is produced without panicking and Complete reflects
	// the most recent (lower) value.
	mid := m.Snapshot()
	if mid.Complete != 2000 {
		t.Errorf("Complete: want 2000 after regression, got %d", mid.Complete)
	}

	// Forward progress recovers.
	m.Update(4000, 10000)
	got := m.Snapshot()
	if got.Complete != 4000 {
		t.Errorf("Complete: want 4000 after recovery, got %d", got.Complete)
	}
	if !approxEq(got.Fraction, 0.4, 1e-9) {
		t.Errorf("Fraction: want 0.4, got %v", got.Fraction)
	}
	// We deliberately do not assert ThroughputBps sign here: the window may
	// still contain the regression. The important contract is no-panic and
	// sensible Fraction/Complete.
}

func TestMeter_SampleCapEnforced(t *testing.T) {
	t.Parallel()
	base := time.Unix(1_700_000_000, 0)
	// Generate 200 timestamps spaced 1ms apart — all well inside the 5s
	// window, so only the cap (not the window trim) bounds the slice.
	times := make([]time.Time, 200)
	for i := range times {
		times[i] = base.Add(time.Duration(i) * time.Millisecond)
	}
	m := Meter{now: fakeClock(times...)}
	for i := 0; i < 200; i++ {
		m.Update(int64(i*100), 1_000_000)
	}
	if got := len(m.samples); got > progressSampleCap {
		t.Errorf("samples length: want <= %d, got %d", progressSampleCap, got)
	}
}
