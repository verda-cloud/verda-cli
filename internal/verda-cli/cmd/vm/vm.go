package vm

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdVM creates the parent VM command.
func NewCmdVM(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vm",
		Aliases: []string{"instance", "instances"},
		Short:   "Manage virtual machines",
		Long: cmdutil.LongDesc(`
			Create and manage Verda virtual machine instances.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdCreate(f, ioStreams),
		NewCmdList(f, ioStreams),
		NewCmdDescribe(f, ioStreams),
		NewCmdAction(f, ioStreams),
		NewCmdAvailability(f, ioStreams),
	)
	return cmd
}
