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
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTransferProgress_BarPercentRate(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	p := newTransferProgress(buf, "Downloading", "model.bin", 100, time.Now().Add(-2*time.Second))
	p.update(0) // first call sets baseline
	buf.Reset()
	p.update(100) // reaching total forces a render despite the throttle

	out := buf.String()
	for _, want := range []string{"Downloading", "model.bin", "100%", "/s", "█", "\r"} {
		if !strings.Contains(out, want) {
			t.Errorf("progress line missing %q:\n%q", want, out)
		}
	}
}

func TestTransferProgress_UnknownTotalNoBar(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	p := newTransferProgress(buf, "Downloading", "blob.bin", 0, time.Now())
	p.update(2048)

	out := buf.String()
	if !strings.Contains(out, "blob.bin") {
		t.Errorf("missing name: %q", out)
	}
	if strings.Contains(out, "%") || strings.Contains(out, "█") {
		t.Errorf("unknown total should render no bar/percent: %q", out)
	}
}

// trackingWriterAt is a no-op io.WriterAt used to exercise countingWriterAt.
type trackingWriterAt struct{}

func (trackingWriterAt) WriteAt(p []byte, _ int64) (int, error) { return len(p), nil }

func TestCountingWriterAt_AccumulatesAndReports(t *testing.T) {
	t.Parallel()
	var totals []int64
	c := &countingWriterAt{w: trackingWriterAt{}, onWrite: func(total int64) { totals = append(totals, total) }}

	if n, err := c.WriteAt([]byte("hello"), 0); n != 5 || err != nil {
		t.Fatalf("WriteAt = (%d, %v)", n, err)
	}
	if n, err := c.WriteAt([]byte("ab"), 5); n != 2 || err != nil {
		t.Fatalf("WriteAt = (%d, %v)", n, err)
	}
	if len(totals) != 2 || totals[0] != 5 || totals[1] != 7 {
		t.Errorf("cumulative totals = %v, want [5 7]", totals)
	}
}
