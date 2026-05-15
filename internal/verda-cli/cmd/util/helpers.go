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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/verda-cloud/verdagostack/pkg/tui"
)

// IsPromptCancel reports whether err represents a clean prompter exit
// (Ctrl+C surfaces as tui.ErrInterrupted, Esc as context.Canceled) rather
// than a real failure. Real I/O errors and context deadlines should propagate.
//
// Most call sites should prefer IsPromptInterrupt / IsPromptBack so the
// two cancel keys can be handled differently — Ctrl+C is a deliberate
// "I'm done with everything", Esc is a lightweight "back / cancel this
// scope". Conflating them produces UX where the "esc back" hint surfaces
// an unexpected confirmation dialog.
func IsPromptCancel(err error) bool {
	return IsPromptInterrupt(err) || IsPromptBack(err)
}

// IsPromptInterrupt reports whether err is specifically a Ctrl+C interrupt
// from the prompter (tui.ErrInterrupted). Use this to gate exit-confirmation
// prompts — Ctrl+C is a deliberate "I want out" signal.
func IsPromptInterrupt(err error) bool {
	return errors.Is(err, tui.ErrInterrupted)
}

// IsPromptBack reports whether err is specifically an Esc / soft-cancel
// from the prompter (context.Canceled). Use this to drive "back" or
// "return to previous scope" behavior — Esc should not surface an exit
// confirmation since the hint bar already advertises "esc back".
func IsPromptBack(err error) bool {
	return errors.Is(err, context.Canceled)
}

// CheckErr prints a user-friendly error to stderr and exits with code 1.
func CheckErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// UsageErrorf creates a formatted usage error that hints the user to run --help.
func UsageErrorf(cmd *cobra.Command, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nSee '%s --help' for help and examples", msg, cmd.CommandPath())
}

// DefaultSubCommandRun prints help when a parent command is invoked without a subcommand.
func DefaultSubCommandRun(out io.Writer) func(c *cobra.Command, args []string) {
	return func(c *cobra.Command, args []string) {
		c.SetOut(out)
		c.SetErr(out)
		RequireNoArguments(c, args)
		_ = c.Help()
	}
}

// DebugJSON writes a labeled JSON dump to w when debug is true.
func DebugJSON(w io.Writer, debug bool, label string, v any) {
	if !debug {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_, _ = fmt.Fprintf(w, "DEBUG: %s\n", label)
	_ = enc.Encode(v)
	_, _ = fmt.Fprintln(w)
}

// RequireNoArguments prints a usage error and exits if extra arguments are present.
func RequireNoArguments(c *cobra.Command, args []string) {
	if len(args) > 0 {
		CheckErr(UsageErrorf(c, "unknown command %q", strings.Join(args, " ")))
	}
}
