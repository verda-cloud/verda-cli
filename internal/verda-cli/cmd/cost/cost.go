package cost

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdCost creates the parent cost command.
func NewCmdCost(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Cost estimation, pricing, and billing",
		Long: cmdutil.LongDesc(`
			Estimate costs, view price history, and check account balance.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		newCmdEstimate(f, ioStreams),
		newCmdBalance(f, ioStreams),
	)

	return cmd
}
