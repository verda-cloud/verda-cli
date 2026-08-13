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
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// styled is what every table and card in this CLI produces: lipgloss always
// emits escapes, whatever the destination turns out to be.
func styled() string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2")).Render("GPU Instances")
}

func TestStyleRenderAlwaysEmitsEscapes(t *testing.T) {
	t.Parallel()

	// The premise of the wiring: stripping cannot be the style's job, because a
	// Style has no idea where its output goes.
	if !strings.ContainsRune(styled(), '\033') {
		t.Fatal("lipgloss no longer emits escapes; the colorprofile wrapper may be redundant")
	}
}

// A piped or redirected stream must receive plain text: this is the defect that
// reached users as ANSI in `verda instance-types > file`.
func TestColorProfileWriterStripsForNonTerminal(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := colorprofile.NewWriter(&buf, os.Environ())

	if _, err := fmt.Fprint(w, styled()); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := buf.String()
	if strings.ContainsRune(got, '\033') {
		t.Errorf("escapes survived to a non-terminal writer: %q", got)
	}
	if !strings.Contains(got, "GPU Instances") {
		t.Errorf("text lost along with the color: %q", got)
	}
}

// The other direction, which a sandbox cannot prove with a real PTY: when the
// destination can display color, the wrapper must not strip it. Forcing the
// profile isolates the writer's behavior from terminal detection.
func TestColorProfileWriterKeepsColorWhenSupported(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.TrueColor}

	if _, err := fmt.Fprint(w, styled()); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := buf.String()
	if !strings.ContainsRune(got, '\033') {
		t.Errorf("color was stripped for a color-capable destination: %q", got)
	}
	if !strings.Contains(got, "GPU Instances") {
		t.Errorf("text lost: %q", got)
	}
}

// Pins the wiring itself: unwrapping either stream silently reintroduces ANSI on
// every piped command, which no per-command test would notice.
func TestNewStdIOStreamsWrapsBothWriters(t *testing.T) {
	t.Parallel()

	s := NewStdIOStreams()

	out, ok := s.Out.(*colorprofile.Writer)
	if !ok {
		t.Fatalf("Out is %T, want *colorprofile.Writer", s.Out)
	}
	if out.Forward != os.Stdout {
		t.Errorf("Out forwards to %v, want os.Stdout", out.Forward)
	}

	errOut, ok := s.ErrOut.(*colorprofile.Writer)
	if !ok {
		t.Fatalf("ErrOut is %T, want *colorprofile.Writer", s.ErrOut)
	}
	if errOut.Forward != os.Stderr {
		t.Errorf("ErrOut forwards to %v, want os.Stderr", errOut.Forward)
	}

	if s.In != os.Stdin {
		t.Errorf("In = %v, want os.Stdin", s.In)
	}
}

// Under test the profile is derived from the destination, so a buffer resolves to
// something that cannot show color. If this ever flips, the strip test above
// would pass for the wrong reason.
func TestNonTerminalProfileIsDetectedNotAssumed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := colorprofile.NewWriter(&buf, os.Environ())

	if w.Profile == colorprofile.TrueColor || w.Profile == colorprofile.ANSI256 {
		t.Errorf("a bytes.Buffer resolved to %v; detection is not seeing the destination", w.Profile)
	}
}
