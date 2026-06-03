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

package objectstorage

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// progressBarWidth is the cell width of the in-line transfer bar.
	progressBarWidth = 24
	// progressMinInterval throttles redraws (download writes can fire thousands
	// of times per second from concurrent ranged GETs).
	progressMinInterval = 100 * time.Millisecond
)

// transferProgress renders a single in-place line (overwritten via \r) showing
// a bar, percentage, and live transfer rate for an upload or download. The tui
// progress component accepts only a percentage and can't carry a rate, so the
// transfer paths render their own line. The rate is measured over bytes moved
// this session (baseline-relative), so a resumed upload reports its true
// throughput. update is safe to call concurrently (download writers do).
type transferProgress struct {
	w     io.Writer
	verb  string // "Uploading" / "Downloading"
	name  string
	total int64
	start time.Time

	mu       sync.Mutex
	baseline int64
	baseSet  bool
	lastDraw time.Time
}

func newTransferProgress(w io.Writer, verb, name string, total int64, start time.Time) *transferProgress {
	return &transferProgress{w: w, verb: verb, name: name, total: total, start: start}
}

// update redraws the line for the cumulative bytes transferred so far.
func (p *transferProgress) update(done int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.baseSet {
		p.baseline, p.baseSet = done, true
	}
	now := time.Now()
	final := p.total > 0 && done >= p.total
	if !final && !p.lastDraw.IsZero() && now.Sub(p.lastDraw) < progressMinInterval {
		return
	}
	p.lastDraw = now

	rate := ""
	if moved := done - p.baseline; moved > 0 {
		if secs := now.Sub(p.start).Seconds(); secs > 0 {
			rate = humanBytes(int64(float64(moved)/secs)) + "/s"
		}
	}
	if p.total > 0 {
		pct := float64(done) / float64(p.total)
		filled := min(int(pct*progressBarWidth), progressBarWidth)
		bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)
		_, _ = fmt.Fprintf(p.w, "\r  %s %s  %s %3.0f%%  %-11s", p.verb, p.name, bar, pct*100, rate)
		return
	}
	// Unknown total (e.g. HeadObject unavailable): show bytes + rate, no bar.
	_, _ = fmt.Fprintf(p.w, "\r  %s %s  %s  %-11s", p.verb, p.name, humanBytes(done), rate)
}

// finish clears the progress line so the final result line prints cleanly.
func (p *transferProgress) finish() {
	_, _ = fmt.Fprint(p.w, "\r\033[K")
}

// countingWriterAt wraps an io.WriterAt and reports the running total of bytes
// written via onWrite. The transfer manager writes ranges concurrently, so the
// counter is atomic; onWrite must be safe for concurrent calls.
type countingWriterAt struct {
	w       io.WriterAt
	n       atomic.Int64
	onWrite func(total int64)
}

func (c *countingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	written, err := c.w.WriteAt(p, off)
	if written > 0 && c.onWrite != nil {
		c.onWrite(c.n.Add(int64(written)))
	}
	return written, err
}
