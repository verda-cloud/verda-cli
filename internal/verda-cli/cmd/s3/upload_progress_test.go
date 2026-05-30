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
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestUploadProgress_RendersBarPercentRate(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	// started 2s ago so the rate divisor is non-zero and deterministic-ish.
	up := newUploadProgress(buf, "model.bin", 100*minPartSize, minPartSize, time.Now().Add(-2*time.Second))

	up.update(1, 4) // first call sets the baseline; no rate yet
	buf.Reset()
	up.update(3, 4) // 2 new parts moved over ~2s -> a rate appears

	out := buf.String()
	for _, want := range []string{"model.bin", "75%", "/s", "█", "\r"} {
		if !strings.Contains(out, want) {
			t.Errorf("progress line missing %q:\n%q", want, out)
		}
	}
}

func TestUploadProgress_FinishClearsLine(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	newUploadProgress(buf, "x", 10, 5, time.Now()).finish()
	if !strings.Contains(buf.String(), "\r") {
		t.Errorf("finish should rewrite the line, got %q", buf.String())
	}
}
