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
	"io"
	"os"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

// IOStreams provides the standard names for iostreams, making it easy to
// test commands that read/write to stdin, stdout, and stderr.
type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// NewStdIOStreams returns an IOStreams wired to os.Stdin, os.Stdout, and os.Stderr.
//
// Both writers are wrapped in a colorprofile writer, which detects what the
// destination can display and downsamples or strips ANSI accordingly. lipgloss
// v2 always emits escapes from Style.Render — stripping is the writer's job, not
// the style's — so this is the single point where a piped or redirected stream
// stops receiving color. Per-command IsStdoutTerminal checks remain useful for
// choosing a *layout* (interactive picker vs plain list); this handles color.
//
// Tests build IOStreams directly, so they keep raw buffers and are unaffected.
func NewStdIOStreams() IOStreams {
	return IOStreams{
		In:     os.Stdin,
		Out:    colorprofile.NewWriter(os.Stdout, os.Environ()),
		ErrOut: colorprofile.NewWriter(os.Stderr, os.Environ()),
	}
}

// IsStdoutTerminal returns true if stdout is a terminal (not piped/redirected).
func IsStdoutTerminal() bool {
	return term.IsTerminal(os.Stdout.Fd())
}

// IsStderrTerminal returns true if stderr is a terminal. Progress/status output
// goes to stderr, so in-place (\r) rendering should be gated on this.
func IsStderrTerminal() bool {
	return term.IsTerminal(os.Stderr.Fd())
}
