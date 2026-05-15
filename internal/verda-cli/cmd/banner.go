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
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
)

//go:embed verda-logo.png
var verdaLogoPNG []byte

// printBanner draws the embedded logo via iTerm2 inline-image OSC when stdout
// is a TTY and the terminal is known to support it; otherwise it no-ops so
// unsupported terminals never print raw escape garbage.
func printBanner(out io.Writer) {
	f, ok := out.(*os.File)
	if !ok || !term.IsTerminal(f.Fd()) {
		return
	}
	if !supportsITermImageProtocol() {
		return
	}
	enc := base64.StdEncoding.EncodeToString(verdaLogoPNG)
	// Height in terminal rows; width follows aspect ratio.
	_, _ = fmt.Fprintf(f,
		"\x1b]1337;File=inline=1;height=6;preserveAspectRatio=1:%s\x07\n\n",
		enc)
}

// supportsITermImageProtocol is best-effort TERM_PROGRAM/LC_TERMINAL sniffing.
// Wrong guesses omit the banner or print a stray escape; data paths are unaffected.
func supportsITermImageProtocol() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm":
		return true
	}
	return os.Getenv("LC_TERMINAL") == "iTerm2"
}
