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

package cost

import (
	"context"
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cmdutil "github.com/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

func newCmdBalance(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "balance",
		Short: "Show account balance",
		Long: cmdutil.LongDesc(`
			Display the current account balance and currency.
		`),
		Example: cmdutil.Examples(`
			verda cost balance
			verda cost balance -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBalance(cmd, f, ioStreams)
		},
	}
}

func runBalance(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading balance...")
	}
	balance, err := client.Balance.Get(ctx)
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Balance:", balance)

	if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), balance); wrote {
		return err
	}

	bold := lipgloss.NewStyle().Bold(true)
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)

	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s  %s\n\n",
		bold.Render("Balance:"),
		price.Render(fmt.Sprintf("$%.2f %s", balance.Amount, balance.Currency)))

	return nil
}
