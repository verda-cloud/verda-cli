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

package s3

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// progressBarWidth is the cell width of the in-line upload bar.
const progressBarWidth = 24

// uploadProgress renders a single in-place line (overwritten via \r) showing a
// bar, percentage, and live transfer rate. The tui progress component only
// accepts a percentage and can't carry a rate, so the upload path renders its
// own line. The rate is measured over bytes moved this session, so a resume
// reports its true throughput (the already-on-server baseline is excluded).
type uploadProgress struct {
	w        io.Writer
	name     string
	fileSize int64
	partSize int64
	started  time.Time
	baseline int32
	baseSet  bool
}

func newUploadProgress(w io.Writer, name string, fileSize, partSize int64, started time.Time) *uploadProgress {
	return &uploadProgress{w: w, name: name, fileSize: fileSize, partSize: partSize, started: started}
}

// update redraws the line for the given completed/total part counts.
func (p *uploadProgress) update(done, total int32) {
	if !p.baseSet {
		p.baseline, p.baseSet = done, true
	}
	pct := 0.0
	if total > 0 {
		pct = float64(done) / float64(total)
	}
	filled := min(int(pct*progressBarWidth), progressBarWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)

	rate := ""
	if sent := int64(done-p.baseline) * p.partSize; sent > 0 {
		if secs := time.Since(p.started).Seconds(); secs > 0 {
			rate = humanBytes(int64(float64(min(sent, p.fileSize))/secs)) + "/s"
		}
	}
	_, _ = fmt.Fprintf(p.w, "\r  Uploading %s  %s %3.0f%%  %-11s", p.name, bar, pct*100, rate)
}

// finish clears the progress line so the final result line prints cleanly.
func (p *uploadProgress) finish() {
	_, _ = fmt.Fprint(p.w, "\r\033[K")
}
