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

import "time"

// progressWindow is the sliding window over which throughput is averaged.
// 5s balances responsiveness (reacts to stalls quickly) against noise from
// bursty TCP writes during ggcr layer uploads.
const progressWindow = 5 * time.Second

// progressSampleCap bounds the samples slice so a long-running transfer does
// not grow memory unboundedly. At ~10 Hz update rates from ggcr, 100 samples
// covers the entire 5s window with headroom; older samples are trimmed.
const progressSampleCap = 100

// meterSample is one tick of (timestamp, cumulative bytes complete).
type meterSample struct {
	t        time.Time
	complete int64
}

// Meter tracks byte-transfer progress over time and computes
// windowed (EWMA-ish) throughput + ETA. Zero value is usable.
// Concurrency: a Meter is NOT safe for concurrent Update calls;
// callers serialize.
type Meter struct {
	first    time.Time
	last     time.Time
	complete int64
	total    int64
	samples  []meterSample
	now      func() time.Time // injectable for tests; defaults to time.Now
	started  bool
}

// Snapshot is a point-in-time view of the meter, safe to pass around.
type Snapshot struct {
	Complete      int64
	Total         int64
	Fraction      float64       // 0..1; 0 when Total==0
	ThroughputBps float64       // bytes/sec over the sliding window
	ETA           time.Duration // until Complete == Total at current throughput; 0 if unknown/done
	Elapsed       time.Duration // since first Update
}

// Update records a new tick. Safe for Complete == 0 (e.g. cross-repo
// mount where the layer completes with no byte transfer); the meter
// records zero throughput for that instant.
//
// If complete regresses (e.g. an upload retry restarts a layer), the
// meter accepts the new value as-is. Throughput derived from that
// transition may be negative for one sample; subsequent forward
// progress recovers a sensible rate.
func (m *Meter) Update(complete, total int64) {
	if m.now == nil {
		m.now = time.Now
	}
	now := m.now()

	if !m.started {
		m.first = now
		m.started = true
	}
	m.last = now
	m.complete = complete
	m.total = total

	// Trim samples outside the sliding window, then enforce cap.
	cutoff := now.Add(-progressWindow)
	i := 0
	for i < len(m.samples) && m.samples[i].t.Before(cutoff) {
		i++
	}
	if i > 0 {
		m.samples = m.samples[i:]
	}
	if len(m.samples) >= progressSampleCap {
		// Drop oldest to make room, keeping cap-1 elements plus the new one.
		drop := len(m.samples) - (progressSampleCap - 1)
		m.samples = m.samples[drop:]
	}
	m.samples = append(m.samples, meterSample{t: now, complete: complete})
}

// Snapshot returns the current state.
func (m *Meter) Snapshot() Snapshot {
	if !m.started {
		return Snapshot{}
	}

	s := Snapshot{
		Complete: m.complete,
		Total:    m.total,
		Elapsed:  m.last.Sub(m.first),
	}

	if m.total > 0 {
		f := float64(m.complete) / float64(m.total)
		if f < 0 {
			f = 0
		}
		if f > 1 {
			f = 1
		}
		s.Fraction = f
	}

	// Throughput: prefer sliding window (>= 2 samples), else overall average.
	s.ThroughputBps = m.windowThroughput()

	// ETA: only meaningful with a known positive total and forward progress.
	if m.total > 0 && m.complete < m.total && s.ThroughputBps > 0 {
		remaining := float64(m.total - m.complete)
		secs := remaining / s.ThroughputBps
		s.ETA = time.Duration(secs * float64(time.Second))
	}

	return s
}

// windowThroughput computes bytes/sec. Uses the oldest sample still in
// the 5s window; falls back to the first-ever sample if the window has
// fewer than 2 points (warmup) or the span is non-positive.
func (m *Meter) windowThroughput() float64 {
	if len(m.samples) < 2 {
		// Fall back to overall average using first Update timestamp.
		span := m.last.Sub(m.first).Seconds()
		if span <= 0 {
			return 0
		}
		// m.complete - (initial complete). Initial complete is the first
		// sample's complete value; with < 2 samples we only have one, so
		// the delta is 0 unless m.complete differs. Use samples[0] when
		// available.
		if len(m.samples) == 1 {
			delta := float64(m.complete - m.samples[0].complete)
			if delta == 0 {
				return 0
			}
			return delta / span
		}
		return 0
	}

	oldest := m.samples[0]
	newest := m.samples[len(m.samples)-1]
	span := newest.t.Sub(oldest.t).Seconds()
	if span <= 0 {
		return 0
	}
	delta := float64(newest.complete - oldest.complete)
	return delta / span
}
