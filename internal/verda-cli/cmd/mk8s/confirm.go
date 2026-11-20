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

package mk8s

import (
	"context"
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/verda-cloud/verda-cli/pkg/tui"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// confirmationRequired: the cause is not always agent mode, so the text names every case.
func confirmationRequired(action string) *cmdutil.AgentError {
	ae := cmdutil.NewConfirmationRequiredError(action)
	ae.Message = fmt.Sprintf("destructive action %q requires --yes: there is no prompt in scripts, without a terminal, in agent mode or with -o json|yaml", action)
	return ae
}

// checkConfirmed never prompts: --yes is needed unless a person at a terminal can confirm.
func checkConfirmed(f cmdutil.Factory, yes bool, action string) error {
	if !yes && !interactiveTTY(f) {
		return confirmationRequired(action)
	}
	return nil
}

// confirmWarning prints a red warning with detail lines on stderr, then asks.
func confirmWarning(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, heading string, detail []string, prompt string) (bool, error) {
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	_, _ = fmt.Fprintf(ioStreams.ErrOut, "\n  %s %s\n", warn.Render("⚠"), warn.Render(heading))
	for _, line := range detail {
		_, _ = fmt.Fprintf(ioStreams.ErrOut, "  %s\n", line)
	}
	_, _ = fmt.Fprintln(ioStreams.ErrOut)

	return askConfirm(ctx, ioStreams, prompter, prompt)
}

// askConfirm asks yes/no. A no, Esc or Ctrl+C all give false, nil after printing "Canceled.".
func askConfirm(ctx context.Context, ioStreams cmdutil.IOStreams, prompter tui.Prompter, prompt string) (bool, error) {
	ok, err := prompter.Confirm(ctx, prompt)
	if err != nil {
		if !cmdutil.IsPromptCancel(err) {
			return false, err
		}
		ok = false
	}
	if !ok {
		printCanceled(ioStreams)
	}
	return ok, nil
}

func printCanceled(ioStreams cmdutil.IOStreams) {
	_, _ = fmt.Fprintln(ioStreams.ErrOut, canceledMsg)
}
