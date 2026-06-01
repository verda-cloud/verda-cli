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

package cmd

import (
	"bytes"
	"testing"
)

func TestPrintBanner_NoOpForNonTTYWriter(t *testing.T) {
	var buf bytes.Buffer
	printBanner(&buf)
	if buf.Len() != 0 {
		t.Fatalf("banner leaked into non-TTY writer: %q", buf.String())
	}
}
