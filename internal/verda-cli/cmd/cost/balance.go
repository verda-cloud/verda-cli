package cost

import (
	"context"
	"fmt"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
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
