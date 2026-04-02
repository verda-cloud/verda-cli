package cost

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

type priceHistoryOptions struct {
	Months       int
	InstanceType string
}

func newCmdPriceHistory(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	opts := &priceHistoryOptions{Months: 1}

	cmd := &cobra.Command{
		Use:     "price-history",
		Aliases: []string{"history", "prices"},
		Short:   "Show instance type price history",
		Long: cmdutil.LongDesc(`
			Show historical pricing for instance types. Useful for
			spotting trends and optimizing when to launch spot instances.
		`),
		Example: cmdutil.Examples(`
			# Last month of pricing
			verda cost price-history

			# Last 3 months for a specific type
			verda cost price-history --type 1V100.6V --months 3

			# JSON output
			verda cost price-history --type 1V100.6V -o json
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPriceHistory(cmd, f, ioStreams, opts)
		},
	}

	flags := cmd.Flags()
	flags.IntVar(&opts.Months, "months", opts.Months, "Number of months of history")
	flags.StringVar(&opts.InstanceType, "type", "", "Filter by instance type")

	return cmd
}

func runPriceHistory(cmd *cobra.Command, f cmdutil.Factory, ioStreams cmdutil.IOStreams, opts *priceHistoryOptions) error {
	client, err := f.VerdaClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), f.Options().Timeout)
	defer cancel()

	var sp interface{ Stop(string) }
	if status := f.Status(); status != nil {
		sp, _ = status.Spinner(ctx, "Loading price history...")
	}
	history, err := client.InstanceTypes.GetPriceHistory(ctx, opts.Months, "USD")
	if sp != nil {
		sp.Stop("")
	}
	if err != nil {
		return err
	}

	cmdutil.DebugJSON(ioStreams.ErrOut, f.Debug(), "Price history:", history)

	// Filter by type if specified.
	if opts.InstanceType != "" {
		filtered := make(map[string]interface{})
		for k, v := range history {
			if k == opts.InstanceType {
				filtered[k] = v
			}
		}
		if len(filtered) == 0 {
			_, _ = fmt.Fprintf(ioStreams.Out, "No price history found for %s.\n", opts.InstanceType)
			return nil
		}
		if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), filtered); wrote {
			return err
		}
	} else {
		if wrote, err := cmdutil.WriteStructured(ioStreams.Out, f.OutputFormat(), history); wrote {
			return err
		}
	}

	bold := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	price := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	sep := dim.Render(strings.Repeat("─", 60))

	_, _ = fmt.Fprintf(ioStreams.Out, "\n  %s\n", bold.Render("Price History"))
	_, _ = fmt.Fprintf(ioStreams.Out, "  %s\n\n", sep)

	// Sort instance types for consistent output.
	types := make([]string, 0, len(history))
	for k := range history {
		if opts.InstanceType != "" && k != opts.InstanceType {
			continue
		}
		types = append(types, k)
	}
	sort.Strings(types)

	for _, typeName := range types {
		records := history[typeName]
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s\n", bold.Render(typeName))
		_, _ = fmt.Fprintf(ioStreams.Out, "  %-12s  %12s  %12s\n", "Date", "Fixed", "Dynamic")
		_, _ = fmt.Fprintf(ioStreams.Out, "  %s\n", dim.Render(strings.Repeat("─", 40)))

		for _, r := range records {
			_, _ = fmt.Fprintf(ioStreams.Out, "  %-12s  %s  %s\n",
				r.Date,
				price.Render(fmt.Sprintf("%12s", formatPrice(float64(r.FixedPricePerHour)))),
				price.Render(fmt.Sprintf("%12s", formatPrice(float64(r.DynamicPricePerHour)))))
		}
		_, _ = fmt.Fprintln(ioStreams.Out)
	}

	return nil
}
