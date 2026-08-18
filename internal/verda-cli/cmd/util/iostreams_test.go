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
	"io"
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
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

// Pins both halves of the wiring, each invisible to a per-command test:
// unwrapping a stream reintroduces ANSI on every piped command, and hiding the
// fd behind the wrapper costs bubbletea term.GetSize — which blanks every
// prompt, spinner and pager on a real terminal.
func TestNewStdIOStreamsWrapsBothWriters(t *testing.T) {
	t.Parallel()

	s := NewStdIOStreams()

	cases := []struct {
		name string
		w    io.Writer
		file *os.File
	}{
		{"Out", s.Out, os.Stdout},
		{"ErrOut", s.ErrOut, os.Stderr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tw, ok := tc.w.(*terminalWriter)
			if !ok {
				t.Fatalf("%s is %T, want *terminalWriter", tc.name, tc.w)
			}
			if tw.Writer == nil {
				t.Fatalf("%s has no colorprofile writer; ANSI would survive a pipe", tc.name)
			}
			if tw.Forward != tc.file {
				t.Errorf("%s forwards to %v, want %v", tc.name, tw.Forward, tc.file)
			}

			f, ok := tc.w.(term.File)
			if !ok {
				t.Fatalf("%s does not satisfy term.File; bubbletea renders into a 0x0 viewport", tc.name)
			}
			if f.Fd() != tc.file.Fd() {
				t.Errorf("%s Fd() = %d, want %d", tc.name, f.Fd(), tc.file.Fd())
			}
		})
	}

	if s.In != os.Stdin {
		t.Errorf("In = %v, want os.Stdin", s.In)
	}
}

// Close must not take the process's stdout with it.
func TestTerminalWriterCloseIsNoop(t *testing.T) {
	t.Parallel()

	if err := newTerminalWriter(os.Stdout).Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if _, err := fmt.Fprint(io.Discard, "still usable"); err != nil {
		t.Fatalf("stdout unusable after Close: %v", err)
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
