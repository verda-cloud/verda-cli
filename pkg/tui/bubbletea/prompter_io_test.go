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

package bubbletea

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/verda-cloud/verda-cli/pkg/tui"
)

// TestWithIO_SplitsPromptUIAndData is the factory-wiring contract at the
// backend level: interactive UI renders on ErrOut, data on Out.
func TestWithIO_SplitsPromptUIAndData(t *testing.T) {
	t.Parallel()

	var uiOut, dataOut bytes.Buffer
	p := New(WithIO(tui.IO{
		In:     bytes.NewBufferString("\r"), // Enter: pick the first choice
		ErrOut: &uiOut,
		Out:    &dataOut,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	idx, err := p.Select(ctx, "Pick one", []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if idx != 0 {
		t.Errorf("Select returned %d, want 0 (Enter picks the first choice)", idx)
	}
	if dataOut.Len() != 0 {
		t.Errorf("prompt UI leaked into the data stream: %q", dataOut.String())
	}
	if !strings.Contains(uiOut.String(), "\x1b[") {
		t.Errorf("prompt UI frames did not render on ErrOut: %q", uiOut.String())
	}

	if err := p.Table(ctx, []string{"NAME"}, [][]string{{"row-1"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(dataOut.String(), "NAME") {
		t.Errorf("table data missing from Out: %q", dataOut.String())
	}
}

// TestWithIO_NilStreamsKeepDefaults: partial IO only overrides the given
// streams.
func TestWithIO_NilStreamsKeepDefaults(t *testing.T) {
	t.Parallel()

	var dataOut bytes.Buffer
	p := New(WithIO(tui.IO{Out: &dataOut}))
	if p.in == nil || p.out == nil || p.errOut == nil {
		t.Fatal("nil streams left the prompter unwired")
	}

	if err := p.Table(context.Background(), []string{"A"}, [][]string{{"b"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(dataOut.String(), "A") {
		t.Errorf("table data missing from the configured Out: %q", dataOut.String())
	}
}

// TestNewFromIO_AdaptsTUIModifiers guards the registered Default/DefaultStatus
// builders, which silently dropped their ioOpts before.
func TestNewFromIO_AdaptsTUIModifiers(t *testing.T) {
	t.Parallel()

	var dataOut bytes.Buffer
	p := NewFromIO(func(io *tui.IO) { io.Out = &dataOut })
	if err := p.Table(context.Background(), []string{"A"}, [][]string{{"x"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(dataOut.String(), "A") {
		t.Errorf("NewFromIO dropped the Out modifier: %q", dataOut.String())
	}
}
