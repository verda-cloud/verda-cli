package startupscript

import (
	"github.com/spf13/cobra"

	cmdutil "github/verda-cloud/verda-cli/internal/verda-cli/cmd/util"
)

// NewCmdStartupScript creates the parent startup-script command.
func NewCmdStartupScript(f cmdutil.Factory, ioStreams cmdutil.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "startup-script",
		Aliases: []string{"script"},
		Short:   "Manage startup scripts",
		Long: cmdutil.LongDesc(`
			Create, list, and delete startup scripts that run when a VM boots.
		`),
		Run: cmdutil.DefaultSubCommandRun(ioStreams.Out),
	}

	cmd.AddCommand(
		NewCmdList(f, ioStreams),
		NewCmdAdd(f, ioStreams),
		NewCmdDelete(f, ioStreams),
	)
	return cmd
}
